package render

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// pdfTrace converts Markdown and returns everything drawn on the pages.
func pdfTrace(t *testing.T, md string) string {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPdfRenderer()
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r.trace.String()
}

// Inline formatting is drawn wherever text appears, not only in paragraphs.
func TestInlineFormattingInEveryBlock(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want []string // fragments of trace lines that must appear
	}{
		{"heading", "## Title with `code` and *italic*\n",
			[]string{`Arial-Bold/20 "Title with"`, `Courier/19 "code"`, `Arial-BoldItalic/20 "italic"`}},
		{"list item", "- item with **bold** and `code`\n",
			[]string{`Arial/11 "item with"`, `Arial-Bold/11 "bold"`, `Courier/10 "code"`}},
		{"nested list item", "- outer\n  - inner *italic*\n",
			[]string{`Arial-Italic/11 "italic"`}},
		{"table cell", "| a | b |\n|---|---|\n| **bold** x | `code` |\n",
			[]string{`Arial-Bold/11 "a"`, `Arial-Bold/11 "bold"`, `Arial/11 "x"`, `Courier/10 "code"`}},
		{"blockquote", "> quoted **bold** text\n",
			[]string{`Arial-Italic/11 "quoted"`, `Arial-BoldItalic/11 "bold"`}},
		{"callout", "> [!NOTE]\n> body **bold** `code`\n",
			[]string{`Arial-Bold/11 "bold"`, `Courier/10 "code"`}},
		{"definition list", "Term `code`\n: Definition **bold**\n",
			[]string{`Arial-Bold/11 "Term"`, `Courier/10 "code"`, `Arial-Bold/11 "bold"`}},
		{"footnote", "Text[^1].\n\n[^1]: Note with **bold**.\n",
			[]string{`Arial/9 "Note with"`, `Arial-Bold/9 "bold"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trace := pdfTrace(t, tt.md)
			for _, want := range tt.want {
				if !strings.Contains(trace, want) {
					t.Errorf("missing %s in:\n%s", want, trace)
				}
			}
		})
	}
}

// Underline, strikethrough and highlight are drawn in lists and tables too.
func TestDecorationsOutsideParagraphs(t *testing.T) {
	plain := pdfTrace(t, "- plain item\n\n| a |\n|---|\n| cell |\n")
	styled := pdfTrace(t, "- ~~struck~~ item\n\n| a |\n|---|\n| ==marked== and <u>under</u> |\n")

	if got, base := strings.Count(styled, " line "), strings.Count(plain, " line "); got != base+2 {
		t.Errorf("%d lines drawn, want %d (one strike, one underline)", got, base+2)
	}
	if got, base := strings.Count(styled, " rect "), strings.Count(plain, " rect "); got != base+1 {
		t.Errorf("%d rectangles drawn, want %d (one highlight)", got, base+1)
	}
}

// Long formatted text wraps inside its container instead of running off it.
func TestFormattedTextWraps(t *testing.T) {
	long := strings.Repeat("word **bold** ", 30)
	trace := pdfTrace(t, "- "+long+"\n\n> [!TIP]\n> "+long+"\n")
	for _, line := range strings.Split(trace, "\n") {
		var page int
		var x, y float64
		if n, _ := fmtSscanf(line, &page, &x, &y); n == 3 && x > 595.28-56.69 {
			t.Fatalf("text drawn beyond the right margin: %s", line)
		}
	}
	if n := strings.Count(trace, `"bold"`) + strings.Count(trace, `"bold `); n < 60 {
		t.Errorf("only %d of 60 bold words were drawn", n)
	}
}

// fmtSscanf reads the page and position from a text line of the trace.
func fmtSscanf(line string, page *int, x, y *float64) (int, error) {
	if !strings.Contains(line, " text ") {
		return 0, nil
	}
	return fmt.Sscanf(line, "p%d text  x=%f y=%f", page, x, y)
}
