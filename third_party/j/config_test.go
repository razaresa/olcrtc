package j

import "testing"

func TestParseJitsiConfigDomainsUsesCustomMUC(t *testing.T) {
	js := `
var config = {
    hosts: {
        domain: 'meet1.arbitr.ru',
        muc: 'muc.meet1.arbitr.ru'
    },
    // focus: 'ignored.example.com',
    focusUserJid: 'focus@auth.meet1.arbitr.ru',
}
`

	domains := parseJitsiConfigDomains("meet1.arbitr.ru", []byte(js))

	if domains.MUCDomain != "muc.meet1.arbitr.ru" {
		t.Fatalf("MUCDomain = %q, want %q", domains.MUCDomain, "muc.meet1.arbitr.ru")
	}
	if domains.FocusDomain != "focus.meet1.arbitr.ru" {
		t.Fatalf("FocusDomain = %q, want %q", domains.FocusDomain, "focus.meet1.arbitr.ru")
	}
}

func TestParseJitsiConfigDomainsFallsBackToDefaults(t *testing.T) {
	domains := parseJitsiConfigDomains("meet.cryptopro.ru", []byte(`var config = {};`))

	if domains.MUCDomain != "conference.meet.cryptopro.ru" {
		t.Fatalf("MUCDomain = %q, want default conference domain", domains.MUCDomain)
	}
	if domains.FocusDomain != "focus.meet.cryptopro.ru" {
		t.Fatalf("FocusDomain = %q, want default focus domain", domains.FocusDomain)
	}
}

func TestParseJitsiConfigDomainsEvaluatesSimpleStringExpression(t *testing.T) {
	js := `
var subdomain = '';
var config = {
    hosts: {
        muc: 'conference.' + subdomain + 'meet.igps.ru',
    },
}
`

	domains := parseJitsiConfigDomains("meet.igps.ru", []byte(js))

	if domains.MUCDomain != "conference.meet.igps.ru" {
		t.Fatalf("MUCDomain = %q, want %q", domains.MUCDomain, "conference.meet.igps.ru")
	}
}
