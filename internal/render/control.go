package render

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// controlField is one row of the document control block.
type controlField struct {
	label, value string
}

// controlKey is a front matter key the control block knows, with its label
// in English and in Hungarian.
type controlKey struct {
	key    string
	en, hu string
	theme  func(theme.ControlLabels) string
}

// controlKeys are the keys the control block knows, in the order it shows
// them.
var controlKeys = []controlKey{
	{"title", "Title", "Cím", func(l theme.ControlLabels) string { return l.Title }},
	{"version", "Version", "Verzió", func(l theme.ControlLabels) string { return l.Version }},
	{"status", "Status", "Állapot", func(l theme.ControlLabels) string { return l.Status }},
	{"author", "Author", "Szerző", func(l theme.ControlLabels) string { return l.Author }},
	{"owner", "Owner", "Felelős", func(l theme.ControlLabels) string { return l.Owner }},
	{"approved-by", "Approved by", "Jóváhagyta", func(l theme.ControlLabels) string { return l.ApprovedBy }},
	{"date", "Date", "Dátum", func(l theme.ControlLabels) string { return l.Date }},
	{"review-date", "Review date", "Felülvizsgálat", func(l theme.ControlLabels) string { return l.ReviewDate }},
	{"classification", "Classification", "Minősítés", func(l theme.ControlLabels) string { return l.Classification }},
}

// steeringKeys are front matter keys that steer the conversion. They are
// never rows of the control block, whatever their value.
var steeringKeys = map[string]bool{"cover": true, "watermark": true, "history": true, "pdfa": true}

// normKey is a front matter key as keys are compared: in lower case, with -
// for _ (approved_by, Approved-By).
func normKey(key string) string {
	return strings.ReplaceAll(strings.ToLower(key), "_", "-")
}

// findKey returns the key of the front matter that norm names, as written.
// Of several that differ only in case, the first in alphabetical order wins.
func findKey(fm ast.FrontMatter, norm string) (string, bool) {
	found := ""
	for key := range fm.Raw {
		if normKey(key) == norm && (found == "" || key < found) {
			found = key
		}
	}
	return found, found != ""
}

// frontValue is the value of the front matter key that norm names, as the
// control block shows it; empty if there is none.
func frontValue(fm ast.FrontMatter, norm string) string {
	if key, ok := findKey(fm, norm); ok {
		return controlValue(fm, key)
	}
	return ""
}

// controlFields lists the rows of the control block made from the front
// matter: the keys it knows first, in their order and under their labels,
// then the other keys under their own names, as written. A known key is
// matched whatever its case and with _ for - (approved_by, Approved-By).
// Other keys show only text; lists, tables, numbers and flags are left out.
// The keys that steer the conversion, and those named in omit (as normKey
// writes them), are not shown.
func controlFields(fm ast.FrontMatter, language string, labels theme.ControlLabels, omit ...string) []controlField {
	skip := func(norm string) bool { return steeringKeys[norm] || slices.Contains(omit, norm) }

	known := map[string]string{} // normalized key -> key as written
	for key := range fm.Raw {
		norm := normKey(key)
		for _, k := range controlKeys {
			if k.key == norm {
				if prev, seen := known[norm]; !seen || key < prev {
					known[norm] = key
				}
			}
		}
	}

	hungarian := language == "hu" || strings.HasPrefix(language, "hu-")
	var fields []controlField
	for _, k := range controlKeys {
		key, ok := known[k.key]
		if !ok || skip(k.key) {
			continue
		}
		value := controlValue(fm, key)
		if value == "" {
			continue
		}
		label := k.theme(labels)
		if label == "" {
			label = k.en
			if hungarian {
				label = k.hu
			}
		}
		fields = append(fields, controlField{label, value})
	}

	for _, key := range slices.Sorted(maps.Keys(fm.Raw)) {
		norm := normKey(key)
		if _, isKnown := known[norm]; isKnown || skip(norm) {
			continue
		}
		// Only text values are shown; lists and maps have no single line
		if s, ok := fm.Raw[key].(string); ok && s != "" {
			fields = append(fields, controlField{key, s})
		}
	}
	return fields
}

// controlValue is the value of a known key as text: numbers as written in
// YAML, dates without a time of day as a day.
func controlValue(fm ast.FrontMatter, key string) string {
	if written, ok := fm.Written[key]; ok {
		return written
	}
	switch v := fm.Raw[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case time.Time:
		if h, m, s := v.Clock(); h == 0 && m == 0 && s == 0 {
			return v.Format("2006-01-02")
		}
		return v.Format("2006-01-02 15:04")
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// frontBool reads a front matter value as a yes-or-no answer: true, false,
// yes or no, in any case and with spaces around, as a YAML boolean or as a
// string (yes is a string in YAML). ok is false for anything else.
func frontBool(v any) (value, ok bool) {
	switch v := v.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes":
			return true, true
		case "false", "no":
			return false, true
		}
	}
	return false, false
}

// frontBoolWarning is the warning for a value that frontBool does not accept
// as the answer to the front matter key named.
func frontBoolWarning(key string, v any) string {
	text, isString := v.(string)
	if !isString {
		text = fmt.Sprint(v)
	}
	return fmt.Sprintf("%s in the front matter must be true, false, yes or no, not %q; it was ignored.", key, text)
}

// coverFor says whether a document gets a cover page. The front matter's
// cover key decides when it is true, false, yes or no; without it, or with
// anything else (which is ignored, and named in the warning), the theme does.
func coverFor(fm ast.FrontMatter, themed bool) (cover bool, warning string) {
	key, ok := findKey(fm, "cover")
	if !ok {
		return themed, ""
	}
	if asked, ok := frontBool(fm.Raw[key]); ok {
		return asked, ""
	}
	return themed, frontBoolWarning("cover", fm.Raw[key])
}

// coverOf says whether a document gets a cover page: it is decided by the
// front matter, when that is the first element of the document, and the
// warning says what is wrong with its cover key.
func coverOf(doc *ast.Document, themed bool) (cover bool, warning string) {
	if fm, ok := frontMatterOf(doc); ok {
		return coverFor(fm, themed)
	}
	return false, ""
}
