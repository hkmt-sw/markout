package render

import (
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

// controlFields lists the rows of the control block made from the front
// matter: the keys it knows first, in their order and under their labels,
// then the other keys under their own names, as written. A known key is
// matched whatever its case and with _ for - (approved_by, Approved-By).
// Other keys show only text; lists, tables, numbers and flags are left out.
func controlFields(fm ast.FrontMatter, language string, labels theme.ControlLabels) []controlField {
	known := map[string]string{} // normalized key -> key as written
	for key := range fm.Raw {
		norm := strings.ReplaceAll(strings.ToLower(key), "_", "-")
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
		if !ok {
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

	for _, key := range sortedKeys(fm.Raw) {
		if _, isKnown := known[strings.ReplaceAll(strings.ToLower(key), "_", "-")]; isKnown {
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
