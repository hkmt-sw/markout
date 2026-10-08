package render

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

func layoutWithTOC(t *testing.T, md string) (*PdfRenderer, string) {
	t.Helper()
	gitlab, _ := flavor.ByID("gitlab")
	doc, err := parse.ParseFlavor([]byte(md), gitlab, "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPdfRenderer()
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r, r.trace.String()
}

// The entries of a table of contents carry the page their heading is on.
func TestContentsHavePageNumbers(t *testing.T) {
	md := "# Title\n\n[[_TOC_]]\n\n## First\n\n" + numbered(45, "Paragraph %d.\n\n") +
		"## Second\n\n" + numbered(45, "More %d.\n\n") + "### Third-part\n\nEnd.\n"
	r, trace := layoutWithTOC(t, md)

	// What the table says is where the headings are
	if !reflect.DeepEqual(r.tocPages, r.headingPages) {
		t.Errorf("the table of contents has %v, the headings are on %v", r.tocPages, r.headingPages)
	}
	pages := r.headingPages
	if len(pages) != 4 || pages["title"] != 1 || pages["first"] != 1 || pages["second"] <= 1 || pages["third-part"] <= pages["second"] {
		t.Fatalf("headings are on %v, want the four on pages going up from 1", pages)
	}

	// On the page: the title on the left, the number at the right edge of
	// the text, on the same line, with dots between
	texts := drawnWith(t, trace)
	right := r.marginLeft + r.contentWidth
	for title, page := range map[string]string{"Second": strconv.Itoa(pages["second"]), "Third-part": strconv.Itoa(pages["third-part"])} {
		entry := find(t, texts, title)
		found := false
		for _, d := range texts {
			if d.text == page && d.y == entry.y && d.x > entry.x && d.x > right-20 {
				found = true
			}
		}
		if !found {
			t.Errorf("no page number %s at the end of the line of %q", page, title)
		}
	}
	if !strings.Contains(trace, `" . . .`) {
		t.Error("no dots lead from the titles to the numbers")
	}
	// The number is a link to the heading, like the title
	if n := len(traceLines(trace, "goto")); n != 8 {
		t.Errorf("%d links in the table of contents, want 2 for each of 4 entries", n)
	}
	// The first layout leaves nothing in the trace
	if n := strings.Count(trace, `"End."`); n != 1 {
		t.Errorf("the end of the document is drawn %d times in the trace", n)
	}
	// ... nor does it double the warnings
	_, warnings := pdfWarningsAs(t, "[[_TOC_]]\n\n# T\n\n你好\n")
	if len(warnings) != 1 {
		t.Errorf("warnings: %q", warnings)
	}
}

// pdfWarningsAs is pdfWarnings for the GitLab flavor, which has a marker
// for a table of contents.
func pdfWarningsAs(t *testing.T, md string) (string, []string) {
	t.Helper()
	gitlab, _ := flavor.ByID("gitlab")
	doc, err := parse.ParseFlavor([]byte(md), gitlab, "")
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	r := NewPdfRenderer()
	r.opts.Warn = func(m string) { warnings = append(warnings, m) }
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r.trace.String(), warnings
}

// A table of contents long enough to push headings onto later pages still
// gives the pages they end up on.
func TestLongContentsStayCorrect(t *testing.T) {
	var md strings.Builder
	md.WriteString("[[_TOC_]]\n\n")
	for i := 1; i <= 70; i++ {
		md.WriteString("## Section " + strconv.Itoa(i) + " with a title long enough to come near the end of its line in the table\n\nText.\n\n")
	}
	r, _ := layoutWithTOC(t, md.String())
	if !reflect.DeepEqual(r.tocPages, r.headingPages) {
		t.Error("the page numbers in the table are not where the headings are")
	}
	if len(r.headingPages) != 70 || r.page < 4 {
		t.Errorf("%d headings on %d pages", len(r.headingPages), r.page)
	}
}

// Without a table of contents the document is laid out once.
func TestNoContentsNoSecondLayout(t *testing.T) {
	r, _ := layoutWithTOC(t, "# Title\n\nText.\n")
	if r.tocPages != nil {
		t.Error("a document without a table of contents was laid out for one")
	}
}
