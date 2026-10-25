package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// labelsDoc has the five kinds of callout, a callout with a title of its own
// and a table of contents, in the GitLab flavor, which has all three. Its
// callout titles are the author's own words only where they are written.
const labelsDoc = `# Title

[[_TOC_]]

## Part

> [!NOTE]
> a note

> [!TIP]
> a tip

> [!IMPORTANT]
> an important thing

> [!WARNING]
> a warning

> [!CAUTION]
> a caution

> [!NOTE] My own title
> with words
`

var (
	englishLabels   = []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION", "Table of Contents"}
	hungarianLabels = []string{"MEGJEGYZÉS", "TIPP", "FONTOS", "FIGYELEM", "VIGYÁZAT", "Tartalomjegyzék"}
)

// labelsTheme is the default theme with the words of a theme file.
func labelsTheme() *theme.Set {
	set := theme.Default()
	for _, th := range []*theme.Theme{&set.PDF, &set.DOCX} {
		th.Alert.Note.Label = "Megjegyzés"
		th.Alert.Warning.Label = "Danger"
		th.TOC.Title = "Contents"
	}
	return &set
}

func labelsParsed(t *testing.T) *ast.Document {
	t.Helper()
	gitlab, _ := flavor.ByID("gitlab")
	doc, err := parse.ParseFlavor([]byte(labelsDoc), gitlab, "")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// pdfDrawn is every text the PDF draws, as the trace has them in quotes.
func pdfDrawn(t *testing.T, opts Options) string {
	t.Helper()
	return pdfTraceOf(t, labelsParsed(t), opts)
}

func docxText(t *testing.T, opts Options) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(labelsParsed(t), out, opts); err != nil {
		t.Fatal(err)
	}
	return docxPart(t, out, "word/document.xml")
}

func TestPDFCalloutAndContentsLabels(t *testing.T) {
	quoted := func(s string) string { return `"` + s + `"` }
	check := func(name string, trace string, want, not []string) {
		for _, w := range want {
			if !strings.Contains(trace, quoted(w)) {
				t.Errorf("%s: missing %s in:\n%s", name, w, trace)
			}
		}
		for _, n := range not {
			if strings.Contains(trace, quoted(n)) {
				t.Errorf("%s: has %s", name, n)
			}
		}
	}
	own := "My own title"

	check("English", pdfDrawn(t, Options{}), append(englishLabels, own), hungarianLabels)
	check("another language", pdfDrawn(t, Options{Lang: "de"}), englishLabels, hungarianLabels)
	check("Hungarian", pdfDrawn(t, Options{Lang: "hu"}), append(hungarianLabels, own), englishLabels)
	check("Hungarian region", pdfDrawn(t, Options{Lang: "hu-HU"}), hungarianLabels, englishLabels)
	// The own title is not a label: nothing else is drawn in its place
	if trace := pdfDrawn(t, Options{Lang: "hu"}); strings.Count(trace, quoted("MEGJEGYZÉS")) != 1 {
		t.Errorf("the callout with a title of its own is labeled too:\n%s", trace)
	}

	// A theme's words win over the language, for the kinds it names
	for _, lang := range []string{"", "hu"} {
		trace := pdfDrawn(t, Options{Lang: lang, Theme: labelsTheme()})
		check("theme "+lang, trace, []string{"Megjegyzés", "Danger", "Contents", own}, []string{"NOTE", "WARNING", "MEGJEGYZÉS", "FIGYELEM", "Table of Contents", "Tartalomjegyzék"})
	}
	trace := pdfDrawn(t, Options{Lang: "hu", Theme: labelsTheme()})
	check("theme, the kinds it leaves", trace, []string{"TIPP", "FONTOS", "VIGYÁZAT"}, nil)
}

func TestDOCXCalloutAndContentsLabels(t *testing.T) {
	check := func(name string, doc string, want, not []string) {
		for _, w := range want {
			if !strings.Contains(doc, ">"+w+"<") {
				t.Errorf("%s: missing %s", name, w)
			}
		}
		for _, n := range not {
			if strings.Contains(doc, ">"+n+"<") {
				t.Errorf("%s: has %s", name, n)
			}
		}
	}
	own := "My own title"

	check("English", docxText(t, Options{}), append(englishLabels, own), hungarianLabels)
	check("another language", docxText(t, Options{Lang: "de"}), englishLabels, hungarianLabels)
	check("Hungarian", docxText(t, Options{Lang: "hu"}), append(hungarianLabels, own), englishLabels)
	check("Hungarian region", docxText(t, Options{Lang: "hu-HU"}), hungarianLabels, englishLabels)
	if doc := docxText(t, Options{Lang: "hu"}); strings.Count(doc, ">MEGJEGYZÉS<") != 1 {
		t.Errorf("the callout with a title of its own is labeled too")
	}

	for _, lang := range []string{"", "hu"} {
		doc := docxText(t, Options{Lang: lang, Theme: labelsTheme()})
		check("theme "+lang, doc, []string{"Megjegyzés", "Danger", "Contents", own}, []string{"NOTE", "WARNING", "MEGJEGYZÉS", "FIGYELEM", "Table of Contents", "Tartalomjegyzék"})
	}
	check("theme, the kinds it leaves", docxText(t, Options{Lang: "hu", Theme: labelsTheme()}), []string{"TIPP", "FONTOS", "VIGYÁZAT"}, nil)
}
