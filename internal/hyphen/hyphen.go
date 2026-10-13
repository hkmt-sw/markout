// Package hyphen says where words may be broken at the end of a line, in
// the languages it has patterns for. It uses the patterns of TeX and
// Liang's algorithm: every pattern that is found in a word votes on the
// places between its letters, and the highest vote at a place decides.
package hyphen

import (
	"embed"
	"strings"
	"sync"
	"unicode"
)

//go:embed patterns/*.txt
var files embed.FS

// minLetters is the length of the shortest word that is hyphenated.
const minLetters = 5

// set is the patterns of one language.
type set struct {
	file        string // name of the files, as in hyph-NAME.pat.txt
	exceptions  bool   // there is a hyph-NAME.hyp.txt too
	left, right int    // the fewest letters before and after a hyphen
}

// sets are the languages there are patterns for, by language tag. A
// language without a region is the first of its regions listed here.
var sets = map[string]set{
	"hu":    {file: "hu", left: 2, right: 2},
	"en":    {file: "en-us", exceptions: true, left: 2, right: 3},
	"en-US": {file: "en-us", exceptions: true, left: 2, right: 3},
	"en-GB": {file: "en-gb", exceptions: true, left: 2, right: 3},
}

// Hyphenator hyphenates the words of one language.
type Hyphenator struct {
	set
	once     sync.Once
	patterns map[string][]uint8 // the letters of a pattern -> its votes, one more than letters
	longest  int                // letters in the longest pattern
	except   map[string][]int   // a word -> where it is broken
}

var (
	mu          sync.Mutex
	hyphenators = map[string]*Hyphenator{}
)

// For returns the hyphenator of a language, given as a normalized language
// tag ("hu", "en-GB"), or nil if there are no patterns for it. A language
// of a region that has none of its own is hyphenated as the language:
// "hu-HU" as "hu", "en-AU" as "en".
func For(tag string) *Hyphenator {
	s, ok := sets[tag]
	if !ok {
		language, _, _ := strings.Cut(tag, "-")
		if s, ok = sets[language]; !ok {
			return nil
		}
	}
	mu.Lock()
	defer mu.Unlock()
	h := hyphenators[s.file]
	if h == nil {
		h = &Hyphenator{set: s}
		hyphenators[s.file] = h
	}
	return h
}

// load reads the patterns. They are only read when a word is first
// hyphenated: the Hungarian set is half a megabyte.
func (h *Hyphenator) load() {
	h.patterns = map[string][]uint8{}
	data, _ := files.ReadFile("patterns/hyph-" + h.file + ".pat.txt")
	for _, pattern := range strings.Fields(string(data)) {
		letters, votes := splitPattern(pattern)
		h.patterns[letters] = votes
		if n := len([]rune(letters)); n > h.longest {
			h.longest = n
		}
	}
	if !h.exceptions {
		return
	}
	h.except = map[string][]int{}
	data, _ = files.ReadFile("patterns/hyph-" + h.file + ".hyp.txt")
	for _, word := range strings.Fields(string(data)) {
		var at []int
		letters := 0
		for _, c := range word {
			if c == '-' {
				at = append(at, letters)
			} else {
				letters++
			}
		}
		h.except[strings.ReplaceAll(word, "-", "")] = at
	}
}

// splitPattern takes a pattern such as ".ach4" apart into its letters and
// the votes between them: no digit is a vote of 0.
func splitPattern(pattern string) (string, []uint8) {
	var letters strings.Builder
	var votes []uint8
	vote := uint8(0)
	for _, c := range pattern {
		if c >= '0' && c <= '9' {
			vote = uint8(c - '0')
			continue
		}
		votes = append(votes, vote)
		vote = 0
		letters.WriteRune(c)
	}
	return letters.String(), append(votes, vote)
}

// Breaks returns the places where a word may be broken, as numbers of
// letters before the hyphen, in order. A word that is short, or has
// anything but letters in it, is not broken.
func (h *Hyphenator) Breaks(word string) []int {
	letters := []rune(strings.ToLower(word))
	if len(letters) < minLetters {
		return nil
	}
	for _, c := range letters {
		if !unicode.IsLetter(c) {
			return nil
		}
	}
	h.once.Do(h.load)
	if at, ok := h.except[string(letters)]; ok {
		return h.allowed(at, len(letters))
	}

	// An odd vote allows a break; votes[p+1] is for the place after p letters
	votes := h.votes(letters)
	var at []int
	for p := 1; p < len(letters); p++ {
		if votes[p+1]%2 == 1 {
			at = append(at, p)
		}
	}
	return h.allowed(at, len(letters))
}

// votes is the highest vote of the patterns on every place of a word: before
// each letter of it between two dots, which stand for its ends in the
// patterns.
func (h *Hyphenator) votes(letters []rune) []uint8 {
	w := make([]rune, 0, len(letters)+2)
	w = append(append(append(w, '.'), letters...), '.')
	votes := make([]uint8, len(w)+1)
	for i := range w {
		for j := i + 1; j <= len(w) && j-i <= h.longest; j++ {
			for k, vote := range h.patterns[string(w[i:j])] {
				votes[i+k] = max(votes[i+k], vote)
			}
		}
	}
	return votes
}

// allowed keeps the breaks that leave enough letters on both sides.
func (h *Hyphenator) allowed(at []int, letters int) []int {
	var out []int
	for _, p := range at {
		if p >= h.left && letters-p >= h.right {
			out = append(out, p)
		}
	}
	return out
}
