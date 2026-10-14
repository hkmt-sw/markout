package render

import (
	"strings"
	"unicode"
)

// softHyphen marks a place where a word may be broken; it is written as
// &shy; in Markdown.
const softHyphen = '\u00AD'

// maxBrokenLines is how many lines in a row may end in a part of a word. A
// column of hyphens down the margin is harder on the eye than a loose line.
const maxBrokenLines = 3

// wordBreak is a place where a word may be broken at the end of a line.
type wordBreak struct {
	at     int  // how many characters stay on the line
	hyphen bool // a hyphen is added after them
}

// unhyphenatedWarning says why the words of a PDF are not hyphenated though
// the theme asks for it.
func unhyphenatedWarning(language string) string {
	if language == "" {
		return "words are not hyphenated in the PDF: the document does not say what language it is in. Name it with lang in the front matter or with --lang."
	}
	return "words are not hyphenated in the PDF: there are no hyphenation patterns for " + language + ", only for hu, en-US and en-GB."
}

// softHyphens takes the soft hyphens out of text. It returns the text
// without them and, for the words that had any, where they were: by the
// number of the word, how many characters come before each.
func softHyphens(text string) (string, map[int][]int) {
	if !strings.ContainsRune(text, softHyphen) {
		return text, nil
	}
	var clean strings.Builder
	places := map[int][]int{}
	word, letters, inWord := -1, 0, false
	for _, c := range text {
		switch {
		case c == softHyphen:
			if inWord && letters > 0 {
				places[word] = append(places[word], letters)
			}
			continue
		case unicode.IsSpace(c) && !isNoBreakSpace(c):
			inWord = false
		case !inWord:
			word, letters, inWord = word+1, 1, true
		default:
			letters++
		}
		clean.WriteRune(c)
	}
	return clean.String(), places
}

// breaksOf is where a word may be broken. soft is where its soft hyphens
// were: a word that has them is broken there and nowhere else.
func (b *lineBreaker) breaksOf(word string, soft []int) []wordBreak {
	if !b.soft {
		return nil
	}
	if len(soft) > 0 {
		breaks := make([]wordBreak, len(soft))
		for i, at := range soft {
			breaks[i] = wordBreak{at: at, hyphen: true}
		}
		return breaks
	}
	if b.hyphen == nil {
		return nil
	}

	// Words joined by a no-break space are hyphenated one by one
	letters := []rune(word)
	var breaks []wordBreak
	start := 0
	for i := 0; i <= len(letters); i++ {
		if i < len(letters) && letters[i] != ' ' {
			continue
		}
		breaks = append(breaks, b.breaksIn(letters, start, i)...)
		start = i + 1
	}
	return breaks
}

// breaksIn is where letters[from:to], one word, may be broken. What is
// hyphenated is the word without the punctuation around it.
func (b *lineBreaker) breaksIn(letters []rune, from, to int) []wordBreak {
	for from < to && !inWord(letters[from]) {
		from++
	}
	for to > from && !inWord(letters[to-1]) {
		to--
	}
	core := letters[from:to]
	if at := compoundBreaks(core); at != nil {
		return shifted(at, from, false)
	}
	return shifted(b.hyphen.Breaks(string(core)), from, true)
}

// inWord reports whether a character is part of a word and not punctuation
// around it. A digit is: a word with one in it is not hyphenated.
func inWord(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

func shifted(at []int, by int, hyphen bool) []wordBreak {
	breaks := make([]wordBreak, len(at))
	for i, n := range at {
		breaks[i] = wordBreak{at: by + n, hyphen: hyphen}
	}
	return breaks
}

// compoundBreaks is where a word written with hyphens ("well-known",
// "ügyfél-nyilvántartás") may be broken: after a hyphen that has two letters
// or more on both sides. It is nil for a word without hyphens, and empty for
// one that has them but is not to be broken, such as "e-mail" or "A-12".
func compoundBreaks(word []rune) []int {
	if !strings.ContainsRune(string(word), '-') {
		return nil
	}
	breaks := []int{}
	for i, c := range word {
		if c == '-' && lettersAt(word, i-2, i) && lettersAt(word, i+1, i+3) {
			breaks = append(breaks, i+1)
		}
	}
	return breaks
}

// lettersAt reports whether word[from:to] exists and is all letters.
func lettersAt(word []rune, from, to int) bool {
	if from < 0 || to > len(word) {
		return false
	}
	for _, c := range word[from:to] {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}
