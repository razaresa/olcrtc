// Package runtime holds shared runtime limits used by client and server.
package runtime

import "github.com/openlibrecommunity/olcrtc/internal/crypto"

const (
	// SmuxFrameOverhead is the fixed smux frame header size. MaxFrameSize
	// caps only the smux payload, while muxconn encrypts and sends the whole
	// smux frame as one transport message.
	SmuxFrameOverhead = 8
	// SmuxWireOverhead is the non-payload overhead added around each smux
	// frame before it reaches the transport payload limit.
	SmuxWireOverhead = crypto.WireOverhead + SmuxFrameOverhead
	// MinSmuxWirePayload is the smallest encrypted transport payload cap
	// that can still carry a non-empty smux frame.
	MinSmuxWirePayload = SmuxWireOverhead + 1
)
