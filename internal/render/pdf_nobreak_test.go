package render

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

var noBreakSpaces = map[string]string{
	"no-break space":        "\u00a0",
	"narrow no-break space": "\u202f",
	"figure space":          "\u2007",
}

// nameBroken lays out a name behind n short words, for every n up to twenty,
// and says for how many of them a line ended after the first word of the
// name or began with the second.
func nameBroken(r *PdfRenderer, style textStyle, space string, width float64) (broken int) {
	for n := 1; n <= 20; n++ {
		text := strings.Repeat("ab ", n) + "Kovács" + space + "Anna végén"
		lines := lineTexts(r.layout([]ast.InlineRun{{Text: text}}, style, width, width))
		for i, line := range lines {
			if (strings.HasSuffix(line, "Kovács") && i < len(lines)-1) || (strings.HasPrefix(line, "Anna") && i > 0) {
				broken++
				break
			}
		}
	}
	return broken
}

// A no-break space keeps the words around it on one line; an ordinary space
// does not.
func TestNoBreakSpaceKeepsWordsTogether(t *testing.T) {
	r := laidOut(t, theme.Default(), "")
	if nameBroken(r, r.bodyStyle(), " ", 120) == 0 {
		t.Fatal("an ordinary space never breaks the name: the test shows nothing")
	}
	for what, space := range noBreakSpaces {
		if n := nameBroken(r, r.bodyStyle(), space, 120); n != 0 {
			t.Errorf("%s: the line is broken inside the name %d times", what, n)
		}
	}
}

// Justified lines are stretched at the ordinary spaces only: the name stays
// one piece, drawn with the width of a space.
func TestNoBreakSpaceInJustifiedText(t *testing.T) {
	r := laidOut(t, justifiedTheme(), "")
	style := r.proseStyle()
	if !style.justify {
		t.Fatal("the theme does not justify")
	}
	for what, space := range noBreakSpaces {
		if n := nameBroken(r, style, space, 120); n != 0 {
			t.Errorf("%s: the line is broken inside the name %d times", what, n)
		}
	}

	const width = 200.0
	text := "Az ügyfél Kovács\u00a0Anna volt, akinek a megbízást a cég nevében a vezérigazgató adta meg tegnap délután."
	lines := r.layout([]ast.InlineRun{{Text: text}}, style, width, width)
	name, flush := 0, 0
	for i, line := range lines {
		for _, f := range line.frags {
			if f.text == "Kovács Anna" {
				name++
			}
		}
		if i < len(lines)-1 && line.width() > width-0.01 {
			flush++
		}
	}
	if name != 1 {
		t.Errorf("the name is in %d pieces, not one", name)
	}
	if flush == 0 {
		t.Errorf("no line is stretched to the width of the column:\n%s", strings.Join(lineTexts(lines), "\n"))
	}
}

// A long word next to a no-break space is still hyphenated, and the line is
// not broken at the space.
func TestHyphenationNextToNoBreakSpace(t *testing.T) {
	r := laidOut(t, hyphenatedTheme(theme.HyphenateAlways), "hu")
	b := lineBreaker{hyphen: r.hyphenator(), soft: true}
	one := len(b.breaksOf("rendszerüzemeltetés", nil))
	if one == 0 {
		t.Fatal("the word has no breaks: the test shows nothing")
	}
	// A no-break space comes into the word as an ordinary space
	if got := len(b.breaksOf("információbiztonság rendszerüzemeltetés", nil)); got <= one {
		t.Errorf("the first word has no breaks of its own: %d in all, %d in the second", got, one)
	}

	// Both words fit a line together, but not what is left of the first line
	text := "Az Ön új vasútállomás\u00a0rendszerüzemeltetés végén"
	lines := lineTexts(r.layout([]ast.InlineRun{{Text: text}}, r.proseStyle(), 180, 180))
	joined, hyphens := "", 0
	for i, line := range lines {
		if strings.HasSuffix(line, "-") {
			hyphens++
			joined += strings.TrimSuffix(line, "-")
		} else {
			joined += line
			if i < len(lines)-1 {
				joined += " "
			}
		}
		if strings.HasSuffix(line, "vasútállomás") || strings.HasPrefix(line, "rendszerüzemeltetés") {
			t.Errorf("the line is broken at the no-break space:\n%s", strings.Join(lines, "\n"))
		}
	}
	if hyphens == 0 {
		t.Errorf("no word is hyphenated:\n%s", strings.Join(lines, "\n"))
	}
	if want := strings.ReplaceAll(text, "\u00a0", " "); joined != want {
		t.Errorf("the lines read %q, not %q", joined, want)
	}
}

const nameTable = "| Name | Note |\n|---|---|\n| Kovács&nbsp;Anna | %s |\n| Nagy Béla | the second row |\n"

// A column is wide enough for words joined by a no-break space, while a name
// written with an ordinary space is broken in the same column.
func TestNoBreakSpaceInNarrowTableColumn(t *testing.T) {
	note := strings.Repeat("a note that is long enough to take most of the table ", 4)
	trace := pdfTrace(t, fmt.Sprintf(nameTable, note))
	if !strings.Contains(trace, `"Kovács Anna"`) {
		t.Errorf("the name is not on one line:\n%s", trace)
	}
	if !strings.Contains(trace, `"Nagy"`) || !strings.Contains(trace, `"Béla"`) {
		t.Errorf("a name with an ordinary space is not broken in the narrow column:\n%s", trace)
	}
}

// The space is drawn as a space: nothing is missing, nothing is warned of,
// and the font is asked for no glyph of its own.
func TestNoBreakSpaceIsDrawnAsSpace(t *testing.T) {
	md := "Kovács&nbsp;Anna, Nagy\u202fBéla and Kiss\u2007Pál.\n"
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPdfRenderer()
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	trace := r.trace.String()
	if !strings.Contains(trace, `"Kovács Anna, Nagy Béla and"`) || !strings.Contains(trace, `"Kiss Pál."`) {
		t.Errorf("the spaces are not drawn as spaces:\n%s", trace)
	}
	if len(r.missing) > 0 || len(r.warnings()) > 0 {
		t.Errorf("missing %q, warnings %q", string(r.missing), r.warnings())
	}
	for _, c := range "\u00a0\u202f\u2007" {
		if got := r.usable("a" + string(c) + "b"); got != "a"+string(c)+"b" {
			t.Errorf("usable drops U+%04X: %q", c, got)
		}
	}
}

// The words of a table cell joined by a no-break space count as one word.
func TestColumnSizingKeepsNoBreakWords(t *testing.T) {
	cell := ast.TableCell{Runs: []ast.InlineRun{{Text: "Kovács\u00a0Anna and Nagy\u202fBéla"}}}
	if got := cellLongestWord(cell); got != len([]rune("Kovács Anna")) {
		t.Errorf("the longest word is %d characters, not %d", got, len([]rune("Kovács Anna")))
	}
}
