package parse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

func runsText(runs []ast.InlineRun) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// blockNames lists the kinds of a list item's blocks, with a little of their
// content.
func blockNames(blocks []ast.Element) []string {
	var out []string
	for _, b := range blocks {
		switch e := b.(type) {
		case ast.Paragraph:
			out = append(out, "para:"+runsText(e.Runs))
		case ast.CodeBlock:
			out = append(out, "code:"+strings.TrimSpace(e.Code))
		case ast.Table:
			out = append(out, fmt.Sprintf("table:%d", len(e.Rows)))
		case ast.Image:
			out = append(out, "image:"+e.URL)
		default:
			out = append(out, strings.TrimPrefix(fmt.Sprintf("%T", b), "ast."))
		}
	}
	return out
}

// What a list item holds besides its first paragraph is kept, in order.
func TestListItemBlocks(t *testing.T) {
	md := `1. First step:

   ` + "```" + `
   run this
   ` + "```" + `

   Second paragraph.

   | a |
   |---|
   | 1 |

   > quoted

   > [!NOTE]
   > noted

   ---

   - nested
2. Text ![alt](pic.png) after.
3. ` + "```" + `
   code first
   ` + "```" + `

   then text
`
	doc, err := ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := doc.Elements[0].(ast.List)
	if !ok || len(list.Items) != 3 || len(doc.Elements) != 1 {
		t.Fatalf("want one list of 3 items, got %d elements: %#v", len(doc.Elements), doc.Elements[0])
	}

	tests := []struct {
		text   string
		blocks []string
	}{
		{"First step:", []string{"code:run this", "para:Second paragraph.", "table:1", "Blockquote", "Alert", "HorizontalRule"}},
		{"Text ", []string{"image:pic.png", "para: after."}},
		{"", []string{"code:code first", "para:then text"}},
	}
	for i, tt := range tests {
		item := list.Items[i]
		if got := runsText(item.Runs); got != tt.text {
			t.Errorf("item %d: text %q, want %q", i+1, got, tt.text)
		}
		if got := blockNames(item.Blocks); strings.Join(got, "|") != strings.Join(tt.blocks, "|") {
			t.Errorf("item %d: blocks %q, want %q", i+1, got, tt.blocks)
		}
	}
	if n := len(list.Items[0].Children); n != 1 {
		t.Errorf("item 1 has %d nested items, want 1", n)
	}
}

// Footnotes, and lists inside callouts, are shown as paragraphs: nothing in
// them is dropped on the way.
func TestNestedBlocksAreFlattenedNotDropped(t *testing.T) {
	tests := []struct{ name, md string }{
		{"footnote", "Text[^1].\n\n[^1]: Note.\n\n    ```\n    CODE\n    ```\n\n    - ITEM\n\n    > QUOTE\n"},
		{"list in callout", "> [!NOTE]\n> - item\n>\n>   ```\n>   CODE\n>   ```\n>\n>   | ITEM |\n>   |---|\n>   | QUOTE |\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseFlavor([]byte(tt.md), flavor.Default(), "")
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			for _, elem := range doc.Elements {
				var inner []ast.Element
				switch e := elem.(type) {
				case ast.FootnoteDefinition:
					inner = e.Elements
				case ast.Alert:
					inner = e.Elements
				}
				for _, in := range inner {
					p, ok := in.(ast.Paragraph)
					if !ok {
						t.Errorf("%T is not a paragraph", in)
						continue
					}
					text.WriteString(runsText(p.Runs) + "\n")
				}
			}
			for _, want := range []string{"CODE", "ITEM", "QUOTE"} {
				if !strings.Contains(text.String(), want) {
					t.Errorf("%s is missing from:\n%s", want, text.String())
				}
			}
		})
	}
}
