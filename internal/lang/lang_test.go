package lang

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"hu": "hu", "HU": "hu", " hu-hu ": "hu-HU", "en_GB": "en-GB", "EN-us": "en-US",
		"sr-latn-rs": "sr-Latn-RS", "es-419": "es-419", "de-CH-1996": "de-CH-1996", "fil": "fil",
	} {
		if got, ok := Normalize(in); !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "h", "magyar", "Hungarian", "hu-", "hu--HU", "hu HU", "hü", "en-toolongsubtag", "12"} {
		if got, ok := Normalize(in); ok {
			t.Errorf("Normalize(%q) = %q; want it refused", in, got)
		}
	}
}

func TestRegional(t *testing.T) {
	for in, want := range map[string]string{"hu": "hu-HU", "en": "en-US", "en-GB": "en-GB", "fil": "fil", "sr-Latn-RS": "sr-Latn-RS"} {
		if got := Regional(in); got != want {
			t.Errorf("Regional(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestOfferedAreNormalized(t *testing.T) {
	for _, l := range Offered {
		if got, ok := Normalize(l.Tag); !ok || got != l.Tag {
			t.Errorf("%q is offered but is not in its usual form (%q, %v)", l.Tag, got, ok)
		}
	}
}
