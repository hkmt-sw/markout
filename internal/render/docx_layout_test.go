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
		"heading":            `P{spacing=160/320 line=720/atLeast}`,
		"paragraph":          `P{spacing=0/200 line=340/atLeast}`,
		"list item":          `P{indent=400 spacing=0/0 line=340/atLeast}`,
		"nested list item":   `P{indent=800 spacing=0/0 line=340/atLeast}`,
		"last list item":     `P{indent=400 spacing=0/160 line=340/atLeast}`,
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

	// Indents and borders replace the characters of the plain layout.
	for _, drawn := range []string{"│", "────", `"    • `, `R "    "`} {
		if strings.Contains(outline, drawn) {
			t.Errorf("the styled layout still draws %q", drawn)
		}
	}
	// List text starts right after its bullet.
	if !strings.Contains(outline, `"• "`) || !strings.Contains(outline, `"one"`) {
		t.Errorf("list bullets are not as expected:\n%s", outline)
	}
}

// The default theme's DOCX is laid out as it always was: by the word
// processor, with no spacing or indents set.
func TestDefaultDOCXKeepsThePlainLayout(t *testing.T) {
	outline := docxOutlineWith(t, nil)
	// Cells have borders in every layout; paragraphs only in the styled one
	for _, added := range []string{"spacing=", "indent=", "line=", "P{border-"} {
		if strings.Contains(outline, added) {
			t.Errorf("the default layout now has %s:\n%s", added, outline)
		}
	}
	for _, kept := range []string{"│ ", "────", `"    •  "`} {
		if !strings.Contains(outline, kept) {
			t.Errorf("the default layout lost %q", kept)
		}
	}

	// A theme file, even an empty one extending the default, gets spacing.
	set, err := theme.Loader{}.Load("classic")
	if err != nil {
		t.Fatal(err)
	}
	if set.DOCX.PlainLayout || !theme.Default().DOCX.PlainLayout || theme.Default().PDF.PlainLayout {
		t.Error("the plain layout belongs to the DOCX form of the built-in default only")
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
		`R{b color=CF222E font=Consolas sz=44} "code"`, // code in a heading keeps the heading's size and weight
		`R{b font=Georgia i sz=44} "it"`,
		`R{b color=1A1A2E font=Georgia} "bold"`,
		`link=https://example.com`,
		`R{color=4A4A68 font=Georgia i sz=18} "it"`,
	} {
		if !strings.Contains(outline, want) {
			t.Errorf("missing %s in:\n%s", want, outline)
		}
	}
}
