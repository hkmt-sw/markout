package render

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

func runningTheme() theme.Set {
	set := theme.Default()
	for _, th := range []*theme.Theme{&set.PDF, &set.DOCX} {
		th.Header.Left, th.Header.Right, th.Header.Rule = "{title}", "{date}", true
		th.Footer.Left, th.Footer.Center = "{author}", "Page {page} of {pages}"
	}
	return set
}

// longDoc fills three PDF pages.
func longDoc(frontMatter string) string {
	return frontMatter + "# The Heading\n\n" + strings.Repeat("A paragraph of body text.\n\n", 90)
}

func TestPDFHeaderAndFooterOnEveryPage(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(longDoc("---\ntitle: The Title\nauthor: An Author\ndate: 2026-10-06\n---\n\n")), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	set := runningTheme()
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	trace := r.trace.String()

	pages := strings.Count(trace, `"The Title" #4A4A68`)
	if pages < 3 {
		t.Fatalf("the header appears on %d pages, expected at least 3:\n%s", pages, trace)
	}
	for page := 1; page <= pages; page++ {
		prefix := "p" + itoa(page) + " "
		for _, want := range []string{
			prefix + `text  x=56.7 y=23.8 Arial/9 "The Title" #4A4A68`, // left, in the middle of the top margin
			`Arial/9 "2026-10-06" #4A4A68`,
			prefix + `line  x=56.7 y=37.8 to x=538.6 y=37.8 #B4B4B4/0.5`, // the rule under the header
			`Arial/9 "An Author" #4A4A68`,
			`"Page ` + itoa(page) + ` of ` + itoa(pages) + `" #4A4A68`,
		} {
			if !strings.Contains(trace, want) {
				t.Errorf("page %d: missing %s", page, want)
			}
		}
	}
	if strings.Contains(trace, "Page "+itoa(pages+1)) {
		t.Error("a footer was drawn for a page that does not exist")
	}

	// The date is right-aligned: it ends at the right margin.
	for _, line := range strings.Split(trace, "\n") {
		// 538.6 is the right margin; the text is 46.0 points wide
		if strings.Contains(line, `"2026-10-06" #4A4A68`) && !strings.Contains(line, "x=492.6 ") {
			t.Errorf("the date is not right-aligned: %s", line)
		}
	}
}

func TestRunningTextFallbacks(t *testing.T) {
	// No front matter: the title is the first heading, author and date are
	// empty and leave no stray text.
	doc, err := parse.ParseFlavor([]byte(longDoc("")), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	info := infoOf(doc)
	if want := (docInfo{title: "The Heading"}); info != want {
		t.Fatalf("info = %+v, want %+v", info, want)
	}
	set := runningTheme()
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	if trace := r.trace.String(); !strings.Contains(trace, `Arial/9 "The Heading"`) || strings.Contains(trace, `Arial/9 ""`) {
		t.Errorf("fallback title missing or empty text drawn:\n%s", trace)
	}

	// Without header and footer text nothing is added at all.
	plain := NewPdfRenderer()
	plain.trace = &strings.Builder{}
	if err := plain.RenderToFile(doc, filepath.Join(t.TempDir(), "plain.pdf")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.trace.String(), "Arial/9") {
		t.Error("the default theme drew a header or footer")
	}
}

func TestDOCXHeaderAndFooter(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("---\ntitle: The Title\nauthor: An Author\n---\n\n# H\n\ntext\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	set := runningTheme()
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{Theme: &set}); err != nil {
		t.Fatal(err)
	}

	body := docxPart(t, out, "word/document.xml")
	if !strings.Contains(body, "headerReference") || !strings.Contains(body, "footerReference") {
		t.Fatal("the document does not reference a header and a footer")
	}
	// Both sit inside their margins, not on the page edge.
	if strings.Contains(body, `w:header="0"`) || strings.Contains(body, `w:footer="0"`) {
		t.Error("the header or footer distance from the page edge is zero")
	}

	header := docxPart(t, out, "word/header1.xml")
	if !strings.Contains(header, ">The Title<") || !strings.Contains(header, `w:val="right"`) || !strings.Contains(header, "w:bottom") {
		t.Errorf("header: %s", header)
	}
	footer := docxPart(t, out, "word/footer1.xml")
	for _, want := range []string{">An Author<", ">Page <", ">PAGE<", "> of <", ">NUMPAGES<", `w:val="center"`} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer lacks %s", want)
		}
	}
	// A field marked "dirty" makes Word ask whether to update fields when the
	// document is opened.
	for _, part := range []string{body, header, footer} {
		if strings.Contains(part, "w:dirty") {
			t.Error("a field is marked as needing an update")
		}
	}
	if strings.Contains(footer, "{page}") || strings.Contains(footer, "{author}") {
		t.Error("a placeholder was left in the footer")
	}

	// The default theme adds neither part.
	plain := filepath.Join(t.TempDir(), "plain.docx")
	if err := RenderDocx(doc, plain, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(docxPart(t, plain, "word/document.xml"), "headerReference") {
		t.Error("the default theme added a header")
	}
}

func TestPageParts(t *testing.T) {
	for in, want := range map[string][]string{
		"Page {page} of {pages}": {"Page ", "{page}", " of ", "{pages}"},
		"{page}":                 {"{page}"},
		"no numbers":             {"no numbers"},
		"{pages}{page}":          {"{pages}", "{page}"},
		"":                       nil,
	} {
		if got := pageParts(in); !reflect.DeepEqual(got, want) {
			t.Errorf("pageParts(%q) = %q, want %q", in, got, want)
		}
	}
}
