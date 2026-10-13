package hyphen

import (
	"strings"
	"testing"
)

// show writes a word with its breaks.
func show(h *Hyphenator, word string) string {
	letters := []rune(word)
	var b strings.Builder
	at := h.Breaks(word)
	for i, c := range letters {
		if len(at) > 0 && at[0] == i {
			b.WriteByte('-')
			at = at[1:]
		}
		b.WriteRune(c)
	}
	return b.String()
}

func TestBreaks(t *testing.T) {
	for tag, words := range map[string]string{
		"hu": "in-for-má-ció-biz-ton-sá-gi sza-bály-zat men-té-si el-já-rás-rend rend-szer-gaz-da meg-ol-dás fe-le-lős-ség " +
			"koc-ká-zat-elem-zés vissza-ál-lí-tás vas-út rend-őr egész-ség rend-szer-üze-mel-te-tő Fel-hasz-ná-ló " +
			// a break here would change the spelling, so there is none
			"asszony hosszú könnyű meggyel " +
			// too short, or not only letters
			"alma kör a COVID-19 2026-ban e-mail",
		"en-US": "hy-phen-ation in-for-ma-tion se-cu-rity or-ga-ni-za-tion re-spon-si-bil-ity man-age-ment au-then-ti-ca-tion " +
			"In-fra-struc-ture " +
			// exceptions to the patterns
			"ta-ble as-so-ciate project ref-or-ma-tion " +
			"word the it's",
		"en-GB": "hy-phen-a-tion in-form-a-tion se-cur-ity or-gan-isa-tion re-spons-ib-il-ity man-age-ment " +
			"uni-ver-sity how-ever through-out",
	} {
		h := For(tag)
		if h == nil {
			t.Fatalf("no hyphenator for %s", tag)
		}
		for _, want := range strings.Fields(words) {
			word := want
			if strings.Count(want, "-") > 0 && !strings.ContainsAny(want, "0123456789") && want != "e-mail" {
				word = strings.ReplaceAll(want, "-", "")
			}
			if got := show(h, word); got != want {
				t.Errorf("%s: %s is broken as %s, want %s", tag, word, got, want)
			}
		}
	}
}

// Every break leaves at least the letters the language asks for.
func TestBreaksLeaveEnoughLetters(t *testing.T) {
	for tag, right := range map[string]int{"hu": 2, "en-US": 3, "en-GB": 3} {
		h := For(tag)
		for _, word := range strings.Fields("abban academia elefánt ideiglenes available idea ability equality studio radio arena") {
			n := len([]rune(word))
			for _, p := range h.Breaks(word) {
				if p < 2 || n-p < right {
					t.Errorf("%s: %s is broken after %d of %d letters", tag, word, p, n)
				}
			}
		}
	}
}

func TestFor(t *testing.T) {
	if For("hu") != For("hu-HU") {
		t.Error("hu-HU is not hyphenated as hu")
	}
	if For("en") != For("en-US") || For("en-AU") != For("en-US") {
		t.Error("en and en-AU are not hyphenated as en-US")
	}
	if For("en-GB") == For("en-US") {
		t.Error("en-GB is hyphenated as en-US")
	}
	for _, tag := range []string{"", "de", "fil", "hun"} {
		if For(tag) != nil {
			t.Errorf("%q has a hyphenator", tag)
		}
	}
}

func TestSplitPattern(t *testing.T) {
	letters, votes := splitPattern(".ach4")
	if letters != ".ach" || len(votes) != 5 || votes[4] != 4 || votes[0] != 0 {
		t.Errorf(".ach4 is read as %q %v", letters, votes)
	}
	letters, votes = splitPattern("1á2r")
	if letters != "ár" || len(votes) != 3 || votes[0] != 1 || votes[1] != 2 || votes[2] != 0 {
		t.Errorf("1á2r is read as %q %v", letters, votes)
	}
}

func BenchmarkLoadHungarian(b *testing.B) {
	for range b.N {
		h := &Hyphenator{set: sets["hu"]}
		h.load()
	}
}

func BenchmarkBreaks(b *testing.B) {
	h := For("hu")
	h.Breaks("előkészítés")
	for range b.N {
		h.Breaks("információbiztonsági")
	}
}
