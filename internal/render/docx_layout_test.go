package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

const layoutDoc = "# Title\n\nA paragraph.\n\n- one\n  - nested\n- two\n\nAfter the list.\n\n> A quote.\n\n---\n\n| a | b |\n|---|---|\n| 1 | **2** |\n\n```\ncode\n```\n\n> [!NOTE]\n> A note.\n\nTerm\n: Definition\n\nText[^1].\n\n[^1]: A note with **bold**.\n"

func docxOutlineWith(t *testing.T, set *theme.Set) string {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(layoutDoc), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{Theme: set}); err != nil {
		t.Fatal(err)
	}
	return docxOutline(t, out)
}

// A theme's spacing, indents and rules are written into DOCX. The numbers
// are twips (twentieths of a point).
func TestDOCXFollowsTheThemesSpacing(t *testing.T) {
	set, err := theme.Loader{}.Load("modern")
	if err != nil {
		t.Fatal(err)
	}
	th := &set.DOCX
	th.Text.LineHeight, th.Text.ParagraphSpacing = 17, 10
	th.Heading[0].LineHeight, th.Heading[0].SpaceBefore, th.Heading[0].SpaceAfter = 36, 8, 16
	th.List.Indent, th.List.SpaceAfter = 20, 8
	th.Quote.Indent, th.Quote.SpaceAfter, th.Quote.BarWidth = 20, 4, 2
	th.Rule.Space, th.Rule.Width = 12, 1
	th.Table.CellPadding, th.Table.LineHeight = 6, 14
	th.Code.Padding, th.Code.BlockLineHeight = 10, 13
	th.Alert.Padding = 8
	th.Footnote.LineHeight = 14

	outline := docxOutlineWith(t, &set)
	wants := map[string]string{
		"heading":            `outline=0 spacing=160/320 line=720/atLeast`, // in the Heading 1 style
		"paragraph":          `P{spacing=0/200 line=340/atLeast}`,
		"list item":          `P{level=0 list=1 spacing=0/0 line=340/atLeast}`,
		"nested list item":   `P{level=1 list=1 spacing=0/0 line=340/atLeast}`,
		"last list item":     `P{level=0 list=1 spacing=0/160 line=340/atLeast}`,
		"quote":              `P{border-left=single/16/4F46E5 indent=400 spacing=0/80 line=340/atLeast}`,
		"rule":               `P{border-bottom=single/8/E5E7EB spacing=240/240 line=20/exact}`,
		"table cell":         `P{indent=120/120 spacing=120/120 line=280/atLeast}`,
		"code block":         `P{indent=200/200 spacing=200/200 line=260/atLeast}`,
		"callout title":      `P{indent=160/160 spacing=160/0 line=340/atLeast}`,
		"callout last line":  `P{indent=160/160 spacing=0/160 line=340/atLeast}`,
		"definition":         `P{indent=400 spacing=0/80 line=340/atLeast}`,
		"footnote":           `P{spacing=0/80 line=280/atLeast}`,
		"space after tables": `line=160/exact}`,
	}
	for what, want := range wants {
		if !strings.Contains(outline, want) {
			t.Errorf("%s: missing %s", what, want)
		}
	}
	if t.Failed() {
		t.Logf("outline:\n%s", outline)
	}

	// Indents and borders, not leading spaces and box-drawing characters.
	for _, drawn := range []string{"│", "────", `"    • `, `R "    "`} {
		if strings.Contains(outline, drawn) {
			t.Errorf("the styled layout still draws %q", drawn)
		}
	}
	// The bullet belongs to the list, not to the text of the item.
	if !strings.Contains(outline, `LEVEL 0 bullet "•"`) || !strings.Contains(outline, `R "one"`) {
		t.Errorf("list bullets are not as expected:\n%s", outline)
	}
}

// The default theme is laid out with spacing like every other: nothing is
// left to the word processor's defaults, and nothing is drawn with characters.
func TestDefaultDOCXIsLaidOut(t *testing.T) {
	outline := docxOutlineWith(t, nil)
	for what, want := range map[string]string{
		"heading":    `outline=0 spacing=400/240 line=560/atLeast`, // in the Heading 1 style
		"paragraph":  `P{spacing=0/160 line=320/atLeast}`,
		"list item":  `P{level=0 list=1 spacing=0/0 line=320/atLeast}`,
		"quote":      `P{border-left=single/24/3B82F6 indent=400 spacing=0/80 line=320/atLeast}`,
		"rule":       `P{border-bottom=single/8/B4B4B4 spacing=240/240 line=20/exact}`,
		"table cell": `P{indent=80/80 spacing=80/80 line=260/atLeast}`,
	} {
		if !strings.Contains(outline, want) {
			t.Errorf("%s: missing %s", what, want)
		}
	}
	for _, drawn := range []string{"│", "────", `"    •`} {
		if strings.Contains(outline, drawn) {
			t.Errorf("the default layout still draws %q", drawn)
		}
	}
	if t.Failed() {
		t.Logf("outline:\n%s", outline)
	}
}

// Inline formatting is kept in DOCX headings, table cells, definitions and
// footnotes, in every theme.
func TestDOCXFormattingOutsideParagraphs(t *testing.T) {
	md := "## Head `code` *it*\n\n| a |\n|---|\n| **bold** and [link](https://example.com) |\n\nTerm\n: Def **bold**\n\nx[^1]\n\n[^1]: note *it*\n"
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	outline := docxOutline(t, out)
	for _, want := range []string{
		`R{color=CF222E font=Consolas} "code"`, // code in a heading keeps the size and weight of the heading style
		`R{i} "it"`,
		`R{b} "bold"`,
		`link=https://example.com`,
		`R{color=4A4A68 font=Georgia i sz=18} "it"`,
	} {
		if !strings.Contains(outline, want) {
			t.Errorf("missing %s in:\n%s", want, outline)
		}
	}
}
