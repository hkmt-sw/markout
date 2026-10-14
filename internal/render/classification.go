package render

import (
	"fmt"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// A document's classification and watermark come from its front matter. What
// they say, and how a classification looks, is decided here for both formats;
// each format draws them its own way.

// classBand is the band a document's classification puts on its pages.
type classBand struct {
	drawn             bool // false: no band, whatever the other fields say
	text              string
	color, background theme.Color
}

// classificationFor works out the band the front matter's classification key
// asks for. The band says the classification as the document writes it. The
// level the theme knows it by gives the colors; a classification no level
// knows gets the neutral colors, and the warning says so. There is no band
// when the document has no classification or the theme turns bands off.
func classificationFor(fm ast.FrontMatter, cls theme.Classification) (band classBand, warning string) {
	value := frontValue(fm, "classification")
	if value == "" || !cls.Band {
		return classBand{}, ""
	}
	band = classBand{drawn: true, text: value, color: cls.Color, background: cls.Background}
	level, known := cls.Find(value)
	if known {
		band.color, band.background = level.Color, level.Background
		return band, ""
	}
	return band, fmt.Sprintf("the classification %q in the front matter is not one the theme knows (%s); it has a band in neutral colors.",
		value, strings.Join(cls.LevelNames(), ", "))
}

// classificationOf works out the band of a document: it is decided by the
// front matter, when that is the first element of the document, and the
// warning says what is wrong with its classification.
func classificationOf(doc *ast.Document, cls theme.Classification) (band classBand, warning string) {
	if fm, ok := frontMatterOf(doc); ok {
		return classificationFor(fm, cls)
	}
	return classBand{}, ""
}

// watermarkFor returns the text the front matter's watermark key asks to be
// drawn across the pages, as one line. Anything but text is ignored, and the
// warning says so.
func watermarkFor(fm ast.FrontMatter) (text, warning string) {
	key, ok := findKey(fm, "watermark")
	if !ok {
		return "", ""
	}
	s, isText := fm.Raw[key].(string)
	if !isText {
		return "", fmt.Sprintf("watermark in the front matter must be text, not %v; it was ignored.", fm.Raw[key])
	}
	return strings.Join(strings.Fields(s), " "), ""
}

// watermarkOf returns the watermark of a document, from the front matter when
// that is the first element of the document.
func watermarkOf(doc *ast.Document) (text, warning string) {
	if fm, ok := frontMatterOf(doc); ok {
		return watermarkFor(fm)
	}
	return "", ""
}

// frontMatterOf returns the front matter of a document, if it begins with one.
func frontMatterOf(doc *ast.Document) (ast.FrontMatter, bool) {
	if len(doc.Elements) > 0 {
		fm, ok := doc.Elements[0].(ast.FrontMatter)
		return fm, ok
	}
	return ast.FrontMatter{}, false
}
