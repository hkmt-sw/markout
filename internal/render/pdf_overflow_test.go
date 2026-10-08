package render

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// drawnText is one piece of text in a PDF trace.
type drawnText struct {
	page int
	y    float64
	text string
}

var traceText = regexp.MustCompile(`^p(\d+) text  x=[\d.]+ y=([\d.]+) \S+ ("(?:[^"\\]|\\.)*")`)

// drawnTexts lists the text of a PDF trace in the order it was drawn.
func drawnTexts(t *testing.T, trace string) []drawnText {
	t.Helper()
	var out []drawnText
	for _, line := range strings.Split(trace, "\n") {
		m := traceText.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		page, _ := strconv.Atoi(m[1])
		y, _ := strconv.ParseFloat(m[2], 64)
		text, err := strconv.Unquote(m[3])
		if err != nil {
			t.Fatalf("trace line %q: %v", line, err)
		}
		out = append(out, drawnText{page, y, text})
	}
	return out
}

// checkOnPage fails if any text was drawn below the bottom margin.
func checkOnPage(t *testing.T, texts []drawnText) {
	t.Helper()
	page := theme.Default().PDF.Page
	bottom := page.Height - page.MarginBottom
	for _, d := range texts {
		if d.y > bottom {
			t.Errorf("page %d: %q is drawn at y=%.1f, below the bottom margin (%.1f)", d.page, d.text, d.y, bottom)
		}
	}
}

// pdfWarnings converts Markdown and returns the trace and the warnings.
func pdfWarnings(t *testing.T, md string) (string, []string) {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
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

func numbered(n int, format string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, format, i)
	}
	return b.String()
}

// A code block taller than a page continues on the next pages instead of
// running off the bottom of one.
func TestLongCodeBlockRunsOverPages(t *testing.T) {
	texts := drawnTexts(t, pdfTrace(t, "# Title\n\nIntro.\n\n```\n"+numbered(120, "code line %d.\n")+"```\n\nAfter.\n"))
	checkOnPage(t, texts)

	pages := map[int]bool{}
	next := 1
	for _, d := range texts {
		if d.text == fmt.Sprintf("code line %d.", next) {
			pages[d.page] = true
			next++
		}
	}
	if next != 121 {
		t.Fatalf("code line %d is missing or out of order", next)
	}
	if !pages[1] {
		t.Error("the block should start on the first page, under the text")
	}
	if len(pages) < 2 {
		t.Errorf("the block is on %d page(s), want it split over several", len(pages))
	}
}

// A block that fits on a page is not split: it moves to the next page whole.
func TestShortCodeBlockStaysTogether(t *testing.T) {
	md := numbered(30, "Paragraph %d.\n\n") + "```\n" + numbered(30, "code line %d.\n") + "```\n"
	texts := drawnTexts(t, pdfTrace(t, md))
	checkOnPage(t, texts)

	pages := map[int]bool{}
	for _, d := range texts {
		if strings.HasPrefix(d.text, "code line ") {
			pages[d.page] = true
		}
	}
	if len(pages) != 1 {
		t.Errorf("the block is drawn on %d pages, want 1", len(pages))
	}
}

// Callouts, diagrams, quotes and front matter longer than a page stay on the
// pages too.
func TestLongBoxesRunOverPages(t *testing.T) {
	tests := []struct {
		name, md, last string
	}{
		{"callout", "> [!NOTE]\n" + numbered(90, "> Callout-%d.\n>\n"), "Callout-90."},
		{"diagram", "```mermaid\ngraph TD\n" + numbered(120, "  A%d --> B\n") + "```\n", "  A120 --> B"},
		{"quote", "> " + strings.TrimSpace(numbered(900, "word%d ")) + "\n", "word900"},
		{"front matter", "---\ntitle: T\ndescription: " + strings.TrimSpace(numbered(900, "word%d ")) + "\n---\n\nText.\n", "word900"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			texts := drawnTexts(t, pdfTrace(t, tt.md))
			checkOnPage(t, texts)
			found := false
			for _, d := range texts {
				if strings.HasSuffix(d.text, tt.last) {
					found = d.page > 1
				}
			}
			if !found {
				t.Errorf("%q should be drawn, on a later page", tt.last)
			}
		})
	}
}

// Text with nowhere to break (a long line of code, a URL) is broken to the
// width of the page rather than drawn past its edge.
func TestWideTextIsBroken(t *testing.T) {
	long := strings.Repeat("z", 300)
	tests := []struct {
		name, md string
		maxLen   int // characters that fit on a line, generously
	}{
		{"code block", "```\n" + long + "\n```\n", 100},
		{"paragraph", "See https://example.com/" + long + " for more.\n", 120},
		{"table cell", "| a | b |\n|---|---|\n| " + long + " | 1 |\n", 120},
		{"math block", "$$\n" + long + "\n$$\n", 120},
		{"image placeholder", "![alt](missing/" + long + ".png)\n", 140},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := 0
			for _, d := range drawnTexts(t, pdfTrace(t, tt.md)) {
				n := strings.Count(d.text, "z")
				if n > tt.maxLen {
					t.Errorf("a piece of %d characters is drawn on one line", n)
				}
				if n > 1 {
					total += n
				}
			}
			if total != len(long) {
				t.Errorf("%d of the %d characters are drawn", total, len(long))
			}
		})
	}
}

func TestTabsInCodeAreExpanded(t *testing.T) {
	trace := pdfTrace(t, "```\n\tindented\na\tb\n```\n")
	for _, want := range []string{`"    indented"`, `"a   b"`} {
		if !strings.Contains(trace, want) {
			t.Errorf("missing %s in:\n%s", want, trace)
		}
	}
}

// Characters the fonts lack are left out and named in a warning; characters
// they have are drawn, symbols included.
func TestMissingCharactersAreReported(t *testing.T) {
	trace, warnings := pdfWarnings(t, "Trademark™ 你好 party 🎉 done.\n\n```\ncode 好 界\n```\n")
	if !strings.Contains(trace, "Trademark™") {
		t.Errorf("™ is in the font and should be drawn:\n%s", trace)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %q", len(warnings), warnings)
	}
	for _, c := range []string{"你", "好", "界", "🎉", "4 characters"} {
		if !strings.Contains(warnings[0], c) {
			t.Errorf("warning does not mention %s: %s", c, warnings[0])
		}
	}
	if strings.Contains(warnings[0], "™") {
		t.Errorf("warning names a character that was drawn: %s", warnings[0])
	}

	if _, warnings := pdfWarnings(t, "Plain text, árvíztűrő tükörfúrógép.\n"); len(warnings) != 0 {
		t.Errorf("unexpected warnings: %q", warnings)
	}
}

func TestRightToLeftTextIsReported(t *testing.T) {
	_, warnings := pdfWarnings(t, "Hebrew: שלום\n")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "right-to-left") {
		t.Errorf("want one right-to-left warning, got %q", warnings)
	}
}
