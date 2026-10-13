package render

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

const justifyText = "The backup procedure describes how the copies of the data are made, where they are kept, " +
	"who looks after them and how often a restore is tried out, so that a failure costs no more than a day of work."

const justifyDoc = "# A heading that is long enough to show that a heading keeps its place on the left side\n\n" +
	justifyText + "\n\n- " + justifyText + "\n\n> " + justifyText + "\n\n> [!NOTE]\n> " + justifyText +
	"\n\n| a | b |\n|---|---|\n| " + justifyText + " | " + justifyText + " |\n"

func justifiedTheme() theme.Set {
	set := theme.Default()
	set.PDF.Text.Align, set.DOCX.Text.Align = theme.AlignJustify, theme.AlignJustify
	return set
}

var tracedText = regexp.MustCompile(`text  x=([0-9.]+) y=([0-9.]+) \S+ "([^"]*)"`)

// tracedLine is the text drawn at one height of a page.
type tracedLine struct {
	y     float64
	left  float64 // where its first piece starts
	end   float64 // where its last piece starts
	words int
	text  string
}

// tracedLines groups the text of a trace into lines, in the order drawn.
func tracedLines(trace string) []tracedLine {
	var lines []tracedLine
	for _, m := range tracedText.FindAllStringSubmatch(trace, -1) {
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		if n := len(lines); n == 0 || math.Abs(lines[n-1].y-y) > 3 {
			lines = append(lines, tracedLine{y: y, left: x})
		}
		l := &lines[len(lines)-1]
		l.end, l.words, l.text = x, l.words+1, l.text+" "+m[3]
	}
	return lines
}

func pdfTraceWith(t *testing.T, md string, set theme.Set) string {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r.trace.String()
}

// In a justified paragraph every line but the last is as wide as the column.
func TestJustifiedParagraphFillsItsLines(t *testing.T) {
	set := justifiedTheme()
	r := NewPdfRenderer()
	r.opts = Options{Theme: &set}
	if err := r.RenderToFile(&ast.Document{}, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}

	const width = 300.0
	runs := []ast.InlineRun{{Text: justifyText}, {Text: " in bold,", Bold: true}, {Text: " " + justifyText}}
	lines := r.layout(runs, r.proseStyle(), width, width)
	ragged := r.layout(runs, r.bodyStyle(), width, width)
	if len(lines) < 4 || len(lines) != len(ragged) {
		t.Fatalf("the paragraph has %d lines, too few to tell", len(lines))
	}
	for i, line := range lines {
		if i < len(lines)-1 && math.Abs(line.width()-width) > 0.01 {
			t.Errorf("line %d is %.2f wide, not %.0f", i+1, line.width(), width)
		}
		if i == len(lines)-1 && math.Abs(line.width()-ragged[i].width()) > 0.01 {
			t.Errorf("the last line is %.2f wide, not %.2f as it is flush left", line.width(), ragged[i].width())
		}
		if line.frags[0].x != 0 {
			t.Errorf("line %d starts at %.2f", i+1, line.frags[0].x)
		}
	}
}

// Left alone, a theme sets text as it did before there was a setting: the
// words of a run on a line are drawn as one piece.
func TestTextIsFlushLeftUnlessTheThemeSaysOtherwise(t *testing.T) {
	lines := tracedLines(pdfTraceWith(t, justifyText+"\n", theme.Default()))
	for _, line := range lines {
		if line.words != 1 {
			t.Errorf("a line of plain text was drawn in %d pieces:%s", line.words, line.text)
		}
	}
}

// A line with too few words to fill it is left ragged.
func TestLineThatWouldBeTornApartStaysRagged(t *testing.T) {
	lines := []textLine{
		{frags: []fragment{{text: "two", x: 0, width: 20}, {text: "words", x: 24, width: 30, spaced: true}}},
		{frags: []fragment{{text: "end", x: 0, width: 20}}},
	}
	justify(lines, 200, 200, 4)
	if x := lines[0].frags[1].x; x != 24 {
		t.Errorf("the second word was moved to %.1f", x)
	}

	justify(lines, 60, 60, 4)
	if x := lines[0].frags[1].x; x != 30 {
		t.Errorf("the second word is at %.1f, not at 30 where it ends at the margin", x)
	}
}

// An underline, a highlight or a link runs through the widened spaces.
func TestJustifiedRunHasNoHolesAtItsSpaces(t *testing.T) {
	frags := []fragment{
		{text: "one", x: 0, width: 20},
		{text: "two", x: 24, width: 20, spaced: true, joined: true},
		{text: "next", x: 48, width: 20, spaced: true},
	}
	spread(frags, 6)
	if frags[0].width != 30 {
		t.Errorf("the first word is %.1f wide; it should reach the second at 30", frags[0].width)
	}
	if frags[1].width != 20 {
		t.Errorf("the second word is %.1f wide; the run ends with it", frags[1].width)
	}
	if frags[2].x != 60 {
		t.Errorf("the third word is at %.1f, not 60", frags[2].x)
	}
}

// Paragraphs, list items, quotes and callouts are justified; headings and
// table cells are not.
func TestWhatIsJustified(t *testing.T) {
	set := justifiedTheme()

	t.Run("PDF", func(t *testing.T) {
		// Flush left, the words of a line are drawn as one piece; justified,
		// each is placed on its own.
		tests := []struct {
			name, md string
			pieces   int // on a full line, at most
		}{
			{"heading", "# " + justifyText + "\n", 2}, // the parser hands over its last word as a run of its own
			{"table", "| a | b |\n|---|---|\n| " + justifyText + " | " + justifyText + " |\n", 2},
			{"paragraph", justifyText + "\n", 0},
			{"list item", "- " + justifyText + "\n", 0},
			{"quote", "> " + justifyText + "\n", 0},
			{"callout", "> [!NOTE]\n> " + justifyText + "\n", 0},
		}
		for _, tt := range tests {
			most := 0
			for _, line := range tracedLines(pdfTraceWith(t, tt.md, set)) {
				most = max(most, line.words)
			}
			if tt.pieces > 0 && most > tt.pieces {
				t.Errorf("%s: a line is drawn in %d pieces; it is not to be justified", tt.name, most)
			}
			if tt.pieces == 0 && most < 8 {
				t.Errorf("%s: no line is drawn in more than %d pieces; it is not justified", tt.name, most)
			}
		}
	})

	t.Run("DOCX", func(t *testing.T) {
		doc, err := parse.ParseFlavor([]byte(justifyDoc), flavor.Default(), "")
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := RenderDocx(doc, out, Options{Theme: &set}); err != nil {
			t.Fatal(err)
		}
		justified := 0
		for _, line := range strings.Split(docxOutline(t, out), "\n") {
			if strings.Contains(line, "jc=both") {
				justified++
			}
		}
		if justified != 4 {
			t.Errorf("%d paragraphs are justified, not the four of running text:\n%s", justified, docxOutline(t, out))
		}
	})
}
