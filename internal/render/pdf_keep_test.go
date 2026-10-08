package render

import (
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/theme"
)

// pageOf returns the page a piece of text is drawn on.
func pageOf(t *testing.T, trace, text string) int {
	t.Helper()
	for _, d := range drawnTexts(t, trace) {
		if d.text == text {
			return d.page
		}
	}
	t.Fatalf("%q is not drawn", text)
	return 0
}

// filler is paragraphs that fill a page down to about n lines from its end.
func filler(t *testing.T, linesLeft int) string {
	t.Helper()
	th := theme.Default().PDF
	room := th.Page.Height - th.Page.MarginTop - th.Page.MarginBottom
	each := th.Text.LineHeight + th.Text.ParagraphSpacing
	n := int((room-float64(linesLeft)*th.Text.LineHeight)/each) - 1
	return numbered(n, "Filler-%d.\n\n")
}

// A heading is never the last thing on a page: it goes to the next page
// with what follows it.
func TestHeadingStaysWithWhatFollows(t *testing.T) {
	tests := []struct{ name, after, first string }{
		{"paragraph", "BODYTEXT\n", "BODYTEXT"},
		{"code block", "```\n" + numbered(12, "code %d\n") + "```\n", "code 1"},
		{"table", "| a | b |\n|---|---|\n| CELL | 2 |\n| c | d |\n| e | f |\n", "CELL"},
		{"chart", "```mermaid\ngraph TD\n    CHARTA --> CHARTB --> CHARTC\n```\n", "CHARTA"},
		{"another heading", "### Second\n\nBODYTEXT\n", "BODYTEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := pdfTrace(t, filler(t, 5)+"## Section\n\n"+tt.after)
			heading, first := pageOf(t, trace, "Section"), pageOf(t, trace, tt.first)
			if heading != first {
				t.Errorf("the heading is on page %d, what follows it on page %d", heading, first)
			}
			// With five lines left a tall block cannot follow the heading
			// there; a paragraph or the start of a table can
			tall := tt.name == "code block" || tt.name == "chart"
			if start := pageOf(t, trace, "Filler-1."); heading == start && tall {
				t.Errorf("the heading stayed on page %d, where its block cannot fit", heading)
			}
		})
	}

	// With room for both, nothing moves
	trace := pdfTrace(t, "Intro.\n\n## Section\n\nBODYTEXT\n")
	if pageOf(t, trace, "Section") != 1 || pageOf(t, trace, "BODYTEXT") != 1 {
		t.Error("a heading with room after it was moved")
	}
	// A heading at the very end is still drawn
	if !strings.Contains(pdfTrace(t, "Text.\n\n## Last heading\n"), "Last heading") {
		t.Error("a heading with nothing after it is not drawn")
	}
}

// A table's header row goes with its first row, and a term with the first
// line of its definition.
func TestHeaderRowsAndTermsStayWithWhatFollows(t *testing.T) {
	trace := pdfTrace(t, filler(t, 2)+"| HEAD | b |\n|---|---|\n| FIRSTROW | 2 |\n")
	if h, r := pageOf(t, trace, "HEAD"), pageOf(t, trace, "FIRSTROW"); h != r {
		t.Errorf("the header row is on page %d, the first row on page %d", h, r)
	}
	trace = pdfTrace(t, filler(t, 1)+"TERM\n: DEFINITION\n")
	if a, b := pageOf(t, trace, "TERM"), pageOf(t, trace, "DEFINITION"); a != b {
		t.Errorf("the term is on page %d, its definition on page %d", a, b)
	}
}
