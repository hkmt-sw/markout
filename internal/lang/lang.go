// Package lang knows the languages a document can say it is written in.
package lang

import "strings"

// Language is a language markout offers by name.
type Language struct {
	Tag  string // as written in a document: "hu", "en-GB"
	Name string
}

// Offered are the languages the settings list. A document may name any
// other language by its tag.
var Offered = []Language{
	{"hu", "Hungarian"},
	{"en-US", "English (United States)"},
	{"en-GB", "English (United Kingdom)"},
}

// Normalize returns a language tag in its usual form: the language in lower
// case, a script with a capital, a region in upper case ("hu", "en-GB",
// "sr-Latn-RS"). An underscore is read as a hyphen. It reports false for
// text that is not a language tag.
func Normalize(s string) (string, bool) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"), "-")
	if n := len(parts[0]); n < 2 || n > 3 || !isAlpha(parts[0]) {
		return "", false
	}
	parts[0] = strings.ToLower(parts[0])
	for i, part := range parts[1:] {
		if len(part) < 1 || len(part) > 8 || !isAlphanumeric(part) {
			return "", false
		}
		switch {
		case len(part) == 2 && isAlpha(part):
			part = strings.ToUpper(part) // a region
		case len(part) == 4 && isAlpha(part):
			part = strings.ToUpper(part[:1]) + strings.ToLower(part[1:]) // a script
		default:
			part = strings.ToLower(part)
		}
		parts[i+1] = part
	}
	return strings.Join(parts, "-"), true
}

// regions are where a language written without a region is taken to be
// from.
var regions = map[string]string{
	"cs": "CZ", "da": "DK", "de": "DE", "el": "GR", "en": "US", "es": "ES", "fi": "FI", "fr": "FR",
	"hr": "HR", "hu": "HU", "it": "IT", "nb": "NO", "nl": "NL", "pl": "PL", "pt": "PT", "ro": "RO",
	"sk": "SK", "sl": "SI", "sv": "SE", "uk": "UA",
}

// Regional returns a normalized tag with a region, which is how word
// processors name a language: "hu" is "hu-HU", "en" is "en-US". A tag that
// has more than a language, or whose region is not known, is returned as it
// is.
func Regional(tag string) string {
	if region, ok := regions[tag]; ok {
		return tag + "-" + region
	}
	return tag
}

func isAlpha(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func isAlphanumeric(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
