package render

import (
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// The words markout puts on a page itself, apart from the control block and
// the version history, follow the language of the document: Hungarian for hu
// and hu-*, English otherwise. A theme can give its own word instead.

// isHungarian is whether a language tag names Hungarian.
func isHungarian(language string) bool {
	return language == "hu" || strings.HasPrefix(language, "hu-")
}

// alertLabelsHU are the titles of the callouts in Hungarian; in English they
// are the names of the types (ast.AlertType).
var alertLabelsHU = map[ast.AlertType]string{
	ast.AlertNote:      "MEGJEGYZÉS",
	ast.AlertTip:       "TIPP",
	ast.AlertImportant: "FONTOS",
	ast.AlertCaution:   "VIGYÁZAT",
	ast.AlertWarning:   "FIGYELEM",
}

// alertTitle is the title line of a callout: the author's own title as
// written, otherwise the label the theme gives its kind, otherwise the name of
// the kind in the language of the document. colors are those of its kind.
func alertTitle(alert ast.Alert, language string, colors theme.AlertColors) string {
	if !alert.DefaultTitle {
		return alert.Title
	}
	if colors.Label != "" {
		return colors.Label
	}
	if isHungarian(language) {
		if label, ok := alertLabelsHU[alert.Type]; ok {
			return label
		}
	}
	return alert.Title
}

// tocTitle is the title of the table of contents.
func tocTitle(language string, toc theme.TOC) string {
	switch {
	case toc.Title != "":
		return toc.Title
	case isHungarian(language):
		return "Tartalomjegyzék"
	}
	return "Table of Contents"
}
