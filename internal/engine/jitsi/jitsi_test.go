package jitsi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/pion/webrtc/v4"
)

const (
	testHost      = "meet.example.com"
	testRoom      = "myroom"
	rawFieldKey   = "raw"
	classEndpoint = "EndpointMessage"
)

func TestNormaliseHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{testHost, testHost},
		{"https://" + testHost, testHost},
		{"https://" + testHost + "/", testHost},
		{"https://" + testHost + "/path", testHost},
		{"//" + testHost, testHost},
		{"  https://" + testHost + "  ", testHost},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normaliseHost(tc.raw); got != tc.want {
				t.Fatalf("normaliseHost(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDecodeRaw(t *testing.T) {
	const payload = "hello world"
	encoded := encodeForTest(t, []byte(payload))

	got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{rawFieldKey: encoded}))
	if string(got) != payload {
		t.Fatalf("decodeRaw = %q, want %q", got, payload)
	}

	if got := decodeRaw(makeBridgeMessage("OtherClass", map[string]any{rawFieldKey: encoded})); got != nil {
		t.Fatalf("decodeRaw(other class) = %q, want nil", got)
	}
	if got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{})); got != nil {
		t.Fatalf("decodeRaw(no raw) = %q, want nil", got)
	}
	if got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{rawFieldKey: "not-base64!!!"})); got != nil {
		t.Fatalf("decodeRaw(bad base64) = %q, want nil", got)
	}
}

func TestNewRequiresHost(t *testing.T) {
	_, err := New(context.Background(), engine.Config{
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if !errors.Is(err, ErrHostRequired) {
		t.Fatalf("err = %v, want ErrHostRequired", err)
	}
}

func TestNewRequiresRoom(t *testing.T) {
	_, err := New(context.Background(), engine.Config{
		URL: testHost,
	})
	if !errors.Is(err, ErrRoomRequired) {
		t.Fatalf("err = %v, want ErrRoomRequired", err)
	}
}

func TestNewSucceeds(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   "https://" + testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
		Name:  "olcrtc-test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()
	caps := sess.Capabilities()
	if !caps.ByteStream || !caps.VideoTrack {
		t.Fatalf("Capabilities = %+v, want ByteStream && VideoTrack", caps)
	}
}

func TestSendBeforeConnect(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:    testHost,
		Extra:  map[string]string{credentialKeyRoom: testRoom},
		OnData: func([]byte) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Send([]byte("data")); !errors.Is(err, ErrBridgeNotReady) {
		t.Fatalf("Send err = %v, want ErrBridgeNotReady", err)
	}
}

func TestSendAfterClose(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Send([]byte("data")); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Send err = %v, want ErrSessionClosed", err)
	}
}

func TestByteStreamNegotiatesPeerConnection(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:    testHost,
		Extra:  map[string]string{credentialKeyRoom: testRoom},
		OnData: func([]byte) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	if !js.shouldNegotiatePC() {
		t.Fatal("datachannel byte-stream sessions must negotiate PeerConnection")
	}
}

func TestBridgeSendErrorEndsSessionOnce(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	reasons := make(chan string, 2)
	js.SetEndedCallback(func(reason string) { reasons <- reason })

	js.handleBridgeSendError(errors.New("write failed"))
	js.handleBridgeSendError(errors.New("write failed again"))

	select {
	case got := <-reasons:
		if got != "jitsi bridge send failed" {
			t.Fatalf("ended reason = %q, want jitsi bridge send failed", got)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge send error did not end session")
	}
	select {
	case got := <-reasons:
		t.Fatalf("second ended callback = %q, want none", got)
	default:
	}
}

func TestBridgeKeepaliveMessageIsIgnoredByDataPlane(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	delivered := false
	js.onData = func([]byte) { delivered = true }

	msg := makeBridgeMessageFrom("peerA", bridgeKeepaliveFields())
	if !js.deliverBridgeMessage(msg, true) {
		t.Fatal("keepalive message stopped recv loop")
	}
	if delivered {
		t.Fatal("keepalive message reached data plane")
	}
}

func TestBridgeKeepaliveFieldsDoNotCarryRawPayload(t *testing.T) {
	fields := bridgeKeepaliveFields()
	if fields["type"] != bridgeKeepaliveType {
		t.Fatalf("type = %v, want %s", fields["type"], bridgeKeepaliveType)
	}
	if _, ok := fields[rawFieldKey]; ok {
		t.Fatal("keepalive must not include raw payload")
	}
}

func TestProblemPeerConnectionStatesEndSession(t *testing.T) {
	problemStates := []webrtc.PeerConnectionState{
		webrtc.PeerConnectionStateDisconnected,
		webrtc.PeerConnectionStateFailed,
		webrtc.PeerConnectionStateClosed,
	}
	for _, state := range problemStates {
		t.Run(state.String(), func(t *testing.T) {
			sess, err := New(context.Background(), engine.Config{
				URL:   testHost,
				Extra: map[string]string{credentialKeyRoom: testRoom},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = sess.Close() }()

			js, ok := sess.(*Session)
			if !ok {
				t.Fatal("sess is not *Session")
			}
			reasons := make(chan string, 1)
			js.SetEndedCallback(func(reason string) { reasons <- reason })

			js.handlePeerConnectionState(state)

			select {
			case got := <-reasons:
				want := "jitsi peer connection " + state.String()
				if got != want {
					t.Fatalf("ended reason = %q, want %q", got, want)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s did not end session", state)
			}
		})
	}
}

func TestSanitiseNick(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"alice", "alice"},
		{"Alice Smith", "Alice-Smith"},
		{"Конрад Олег", "Konrad-Oleg"},
		{"olcrtc-bot42", "olcrtc-bot42"},
		{"  bob  ", "bob"},
		{"$$$ %%%", ""},
		{"verylongnicknamethatexceedslimit", "verylongnicknamet"[:16]},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := sanitiseNick(tc.raw); got != tc.want {
				t.Fatalf("sanitiseNick(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDeliverBridgeMessageMagicAndPeerLatch(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	var received [][]byte
	js.onData = func(b []byte) {
		received = append(received, append([]byte(nil), b...))
	}

	good := makeBridgeFrame(t, []byte("alpha"))
	bad := encodeForTest(t, []byte("alpha")) // no magic prefix

	// First valid frame from peerA latches the peer and is delivered.
	if !js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: good}), true) {
		t.Fatal("deliverBridgeMessage returned false on valid frame")
	}
	// Frame without magic is dropped.
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: bad}), true)
	// Frame from a different sender after latch is dropped even with magic.
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{rawFieldKey: good}), true)
	// Another frame from latched peer still flows.
	beta := makeBridgeFrame(t, []byte("beta"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: beta}), true)

	if len(received) != 2 {
		t.Fatalf("received frames = %d, want 2 (%q)", len(received), received)
	}
	if string(received[0]) != "alpha" || string(received[1]) != "beta" {
		t.Fatalf("received = %q, want [alpha beta]", received)
	}
}

func TestBridgeSendTargetTracksPeerLatchAndReset(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	js.onData = func([]byte) {}

	if got := js.bridgeSendTarget(); got != "" {
		t.Fatalf("bridgeSendTarget before latch = %q, want broadcast", got)
	}

	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{
		rawFieldKey: makeBridgeFrame(t, []byte("hello")),
	}), true)
	if got := js.bridgeSendTarget(); got != "peerA" {
		t.Fatalf("bridgeSendTarget after latch = %q, want peerA", got)
	}

	js.ResetPeerLatch()
	if got := js.bridgeSendTarget(); got != "" {
		t.Fatalf("bridgeSendTarget after reset = %q, want broadcast", got)
	}

	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{
		rawFieldKey: makeBridgeFrame(t, []byte("hello")),
	}), true)
	if got := js.bridgeSendTarget(); got != "peerB" {
		t.Fatalf("bridgeSendTarget after relatch = %q, want peerB", got)
	}
}

func TestPeerSwitchRequestsReconnect(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	var received [][]byte
	js.onData = func(b []byte) {
		received = append(received, append([]byte(nil), b...))
	}

	reconnects := make(chan struct{}, 2)
	js.SetShouldReconnect(func() bool { return true })
	js.SetReconnectCallback(func(*webrtc.DataChannel) {
		js.ResetPeerLatch()
		reconnects <- struct{}{}
	})

	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{
		rawFieldKey: makeBridgeFrame(t, []byte("hello")),
	}), true)
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{
		rawFieldKey: makeBridgeFrame(t, []byte("new-peer")),
	}), true)

	select {
	case <-reconnects:
	case <-time.After(time.Second):
		t.Fatal("peer switch did not request reconnect")
	}
	if got := js.bridgeSendTarget(); got != "peerB" {
		t.Fatalf("bridgeSendTarget after peer switch = %q, want peerB", got)
	}
	if len(received) != 2 || string(received[1]) != "new-peer" {
		t.Fatalf("received after peer switch = %q, want second frame delivered", received)
	}

	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{
		rawFieldKey: makeBridgeFrame(t, []byte("duplicate")),
	}), true)
	select {
	case <-reconnects:
		t.Fatal("duplicate peer switch requested reconnect twice")
	default:
	}

	js.ResetPeerLatch()
	if got := js.bridgeSendTarget(); got != "" {
		t.Fatalf("bridgeSendTarget after reset = %q, want broadcast", got)
	}
}

func TestEngineRegistration(t *testing.T) {
	if _, err := engine.New(context.Background(), "jitsi", engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	}); err != nil {
		t.Fatalf("engine.New(jitsi) = %v, want nil", err)
	}
}
