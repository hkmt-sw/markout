package render

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// The corpus is real documents from public projects (fixtures/corpus, with
// where each is from in SOURCES.md). Every one is converted to both formats.
// The conversion must succeed, draw nothing off the page and write a
// consistent DOCX, and a summary of it is compared with a recording: what
// the document holds, how many pages it makes, what the conversion warns
// about. The summary changes when the reading of real documents changes,
// which the sample documents written for markout do not show.

// corpusFlavors maps the directories of the corpus to the flavor their
// documents are read as.
var corpusFlavors = map[string]string{"github": "github", "mkdocs": "mkdocs", "myst": "myst", "docusaurus": "docusaurus"}

func TestGoldenCorpus(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixturesDir, "corpus", "*", "*.md*"))
	sort.Strings(files)
	if len(files) < 10 {
		t.Fatalf("found only %d documents under %s/corpus", len(files), fixturesDir)
	}
	for _, path := range files {
		dir := filepath.Base(filepath.Dir(path))
		name := dir + "-" + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			fl, ok := flavor.ByID(corpusFlavors[dir])
			if !ok {
				t.Fatalf("no flavor for the directory %s", dir)
			}
			summary, _ := convertAndSummarize(t, path, fl)
			checkGolden(t, filepath.Join("corpus", name+".txt"), summary)
		})
	}
}

// The examples (the examples directory of the repository) are documents
// written for markout, of the kinds people convert: a policy, a runbook, a
// report, minutes. They are converted like the corpus, and one thing more is
// asked of them: that the conversion has nothing to warn about. An example
// that needs a warning is a poor example, or shows something to fix.
func TestGoldenExamples(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*", "*.md*"))
	sort.Strings(files)
	if len(files) < 8 {
		t.Fatalf("found only %d examples", len(files))
	}
	for _, path := range files {
		dir := filepath.Base(filepath.Dir(path))
		name := dir + "-" + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			// A directory named after a flavor holds documents in it
			fl, ok := flavor.ByID(dir)
			if !ok {
				fl = flavor.Default()
			}
			summary, warnings := convertAndSummarize(t, path, fl)
			for _, w := range warnings {
				t.Errorf("the conversion warns: %s", w)
			}
			checkGolden(t, filepath.Join("examples", name+".txt"), summary)
		})
	}
}

// convertAndSummarize converts a document to both formats, checks what must
// hold of any conversion, and returns a summary of the result with every
// warning the reading and the conversions gave.
func convertAndSummarize(t *testing.T, path string, fl flavor.Flavor) (string, []string) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.ParseFlavor(source, fl, filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	var summary strings.Builder
	var all []string
	fmt.Fprintf(&summary, "source: %d lines\n", strings.Count(string(source), "\n"))
	summary.WriteString("holds: " + elementCounts(doc.Elements) + "\n")
	for _, w := range doc.Warnings {
		summary.WriteString("reading warns: " + w + "\n")
	}
	all = append(all, doc.Warnings...)

	// PDF
	dir := t.TempDir()
	var warnings []string
	pdf := NewPdfRenderer()
	pdf.opts = Options{BaseDir: filepath.Dir(path), Warn: func(m string) { warnings = append(warnings, m) }}
	pdf.trace = &strings.Builder{}
	if err := pdf.RenderToFile(doc, filepath.Join(dir, "out.pdf")); err != nil {
		t.Fatalf("PDF: %v", err)
	}
	trace := pdf.trace.String()
	checkOnThePage(t, trace)
	if pdf.tocPages != nil && !reflect.DeepEqual(pdf.tocPages, pdf.headingPages) {
		t.Errorf("the table of contents gives pages %v, the headings are on %v", pdf.tocPages, pdf.headingPages)
	}
	if written, err := os.ReadFile(filepath.Join(dir, "out.pdf")); err != nil {
		t.Fatal(err)
	} else {
		checkPDFObjects(t, written)
	}
	fmt.Fprintf(&summary, "pdf: %d pages, %d bookmarks, %d links within the document\n",
		pdf.page, len(traceLines(trace, "bookmark")), len(traceLines(trace, "goto")))
	for _, w := range warnings {
		summary.WriteString("pdf warns: " + w + "\n")
	}
	all = append(all, warnings...)

	// DOCX
	out := filepath.Join(dir, "out.docx")
	warnings = nil
	err = RenderDocx(doc, out, Options{BaseDir: filepath.Dir(path), Warn: func(m string) { warnings = append(warnings, m) }})
	if err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	document := docxPart(t, out, "word/document.xml")
	checkDocxConsistent(t, out)
	count := func(s string) int { return strings.Count(document, s) }
	fmt.Fprintf(&summary, "docx: %d paragraphs, %d headings, %d list items, %d tables, %d equations, %d images\n",
		count("<w:p>"), count(`<w:pStyle w:val="Heading`), count("<w:numPr>"), count("<w:tbl>"), count("<m:oMath>"), count("<w:drawing>"))
	for _, w := range warnings {
		summary.WriteString("docx warns: " + w + "\n")
	}
	all = append(all, warnings...)
	return summary.String(), all
}

// elementCounts says how many elements of each kind a document has, the
// ones inside lists, quotes and callouts included.
func elementCounts(elems []ast.Element) string {
	counts := map[string]int{}
	var walk func(elems []ast.Element)
	var walkItems func(items []ast.ListItem)
	walk = func(elems []ast.Element) {
		for _, elem := range elems {
			counts[strings.TrimPrefix(fmt.Sprintf("%T", elem), "ast.")]++
			switch e := elem.(type) {
			case ast.List:
				walkItems(e.Items)
			case ast.Blockquote:
				walk(e.Elements)
			case ast.Alert:
				walk(e.Elements)
			}
		}
	}
	walkItems = func(items []ast.ListItem) {
		for _, item := range items {
			counts["ListItem"]++
			walk(item.Blocks)
			walkItems(item.Children)
		}
	}
	walk(elems)

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s %d", name, counts[name])
	}
	return strings.Join(parts, ", ")
}

var tracePlace = regexp.MustCompile(`^p\d+ (?:text|rect|line|curve|image) +x=(-?[\d.]+) y=(-?[\d.]+)`)

// checkOnThePage fails if anything in a PDF trace starts outside the page's
// margins: left of the text, below it, or past its right edge.
func checkOnThePage(t *testing.T, trace string) {
	t.Helper()
	page := theme.Default().PDF.Page
	left, right := page.MarginLeft-1, page.Width-page.MarginRight+1
	top, bottom := page.MarginTop-1, page.Height-page.MarginBottom+1
	reported := 0
	for _, line := range strings.Split(trace, "\n") {
		m := tracePlace.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var x, y float64
		fmt.Sscanf(m[1], "%g", &x)
		fmt.Sscanf(m[2], "%g", &y)
		if x < left || x > right || y < top || y > bottom {
			if reported++; reported <= 5 {
				t.Errorf("drawn outside the margins: %s", line)
			}
		}
	}
	if reported > 5 {
		t.Errorf("... and %d more", reported-5)
	}
}

// checkDocxConsistent fails if the parts of a DOCX written by hand are not
// well-formed, or the document refers to something that is not there.
func checkDocxConsistent(t *testing.T, path string) {
	t.Helper()
	document := docxPart(t, path, "word/document.xml")
	for part, content := range map[string]string{"document.xml": document, "styles.xml": docxPart(t, path, "word/styles.xml")} {
		if err := xml.Unmarshal([]byte(content), new(struct{})); err != nil {
			t.Errorf("%s is not well-formed: %v", part, err)
		}
	}
	all := func(pattern, in string) []string {
		var out []string
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(in, -1) {
			out = append(out, m[1])
		}
		return out
	}
	names := map[string]bool{}
	for _, n := range all(`<w:bookmarkStart[^>]*w:name="([^"]+)"`, document) {
		if names[n] {
			t.Errorf("bookmark %s is there twice", n)
		}
		names[n] = true
	}
	dead := 0
	for _, anchor := range all(`w:anchor="([^"]+)"`, document) {
		if !names[anchor] {
			dead++
		}
	}
	if dead > 0 {
		// A link to a heading the document does not have is the document's
		// own; it is counted so that a change in how links resolve shows
		t.Logf("%d links go to anchors the document does not have", dead)
	}
	if lists := all(`<w:numId w:val="(\d+)"`, document); len(lists) > 0 {
		defined := map[string]bool{}
		for _, id := range all(`<w:num w:numId="(\d+)"`, docxPart(t, path, "word/numbering.xml")) {
			defined[id] = true
		}
		for _, id := range lists {
			if !defined[id] {
				t.Errorf("list %s is used but not defined", id)
			}
		}
	}
	if begin, end := strings.Count(document, `w:fldCharType="begin"`), strings.Count(document, `w:fldCharType="end"`); begin != end {
		t.Errorf("%d fields begin and %d end", begin, end)
	}
	for _, mark := range []string{"markout-math", "markout-toc"} {
		if strings.Contains(document, mark) {
			t.Errorf("a %s mark was left in the document", mark)
		}
	}
}
