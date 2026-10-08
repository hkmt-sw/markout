package parse

import (
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// allText gathers the text of a document, tables and code included.
func allText(elems []ast.Element) string {
	var b strings.Builder
	for _, elem := range elems {
		switch e := elem.(type) {
		case ast.Paragraph:
			b.WriteString(runsText(e.Runs) + "\n")
		case ast.Heading:
			b.WriteString(runsText(e.Runs) + "\n")
		case ast.CodeBlock:
			b.WriteString("CODE[" + e.Language + "]:" + e.Code)
		case ast.Table:
			b.WriteString("TABLE:")
			for _, cell := range e.Header.Cells {
				b.WriteString(runsText(cell.Runs) + "|")
			}
			for _, row := range e.Rows {
				b.WriteString("/")
				for _, cell := range row.Cells {
					b.WriteString(runsText(cell.Runs) + "|")
				}
			}
			b.WriteString("\n")
		case ast.Blockquote:
			b.WriteString(allText(e.Elements))
		case ast.Alert:
			b.WriteString(e.Title + "\n" + allText(e.Elements))
		}
	}
	return b.String()
}

func parseDoc(t *testing.T, id, md string) *ast.Document {
	t.Helper()
	fl, ok := flavor.ByID(id)
	if !ok {
		t.Fatalf("no flavor %s", id)
	}
	doc, err := ParseFlavor([]byte(md), fl, "")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// Nothing a document says goes missing without a word: what a directive
// holds is shown, or the conversion says that it was left out.
func TestDirectivesAreShownOrReported(t *testing.T) {
	md := "```{eval-rst}\n.. note:: RST\n```\n\n```{only} html\nONLY\n```\n\n```{raw} html\n<b>RAW</b>\n```\n\n" +
		"```{toctree}\nintro\n```\n\n```{toctree}\nmore\n```\n\n```{mystery} ARGUMENT\nBODY\n```\n"
	doc := parseDoc(t, "myst", md)
	text := allText(doc.Elements)
	for _, want := range []string{"CODE[rst]:.. note:: RST", "ONLY", "ARGUMENT", "BODY"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the document:\n%s", want, text)
		}
	}
	for _, gone := range []string{"RAW", "intro"} {
		if strings.Contains(text, gone) {
			t.Errorf("%q should be left out:\n%s", gone, text)
		}
	}
	if len(doc.Warnings) != 2 {
		t.Fatalf("warnings: %q", doc.Warnings)
	}
	if w := doc.Warnings[0]; !strings.Contains(w, "3 blocks") || !strings.Contains(w, "{raw}") || !strings.Contains(w, "{toctree} (2)") {
		t.Errorf("left out: %s", w)
	}
	if w := doc.Warnings[1]; !strings.Contains(w, "1 directive is not known") || !strings.Contains(w, "{mystery}") {
		t.Errorf("not known: %s", w)
	}

	// A document with nothing of the kind has nothing to report
	if doc := parseDoc(t, "myst", "```{note}\nplain\n```\n\n```python\nx\n```\n"); len(doc.Warnings) != 0 {
		t.Errorf("unexpected warnings: %q", doc.Warnings)
	}
	// Widgets of Azure DevOps wikis
	if doc := parseDoc(t, "azure", "::: video\n<iframe></iframe>\n:::\n"); len(doc.Warnings) != 1 || !strings.Contains(doc.Warnings[0], "::: video") {
		t.Errorf("warnings: %q", doc.Warnings)
	}
}

func TestMySTTables(t *testing.T) {
	tests := []struct{ name, md, want string }{
		{"list-table", "```{list-table} Caption\n:header-rows: 1\n\n* - Name\n  - Value\n* - a | b\n  - spans\n    two lines\n```\n",
			"Caption\nTABLE:Name|Value|/a | b|spans two lines|\n"},
		{"list-table without a header", "```{list-table}\n* - a\n  - b\n```\n", "TABLE:||/a|b|\n"},
		{"csv-table", "```{csv-table} Caption\n:header: \"x\", \"y\"\n\n\"a, with comma\", 1\nb, 2\n```\n",
			"Caption\nTABLE:x|y|/a, with comma|1|/b|2|\n"},
		{"not a list of rows", "```{list-table}\njust text\n```\n", "just text\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parseDoc(t, "myst", tt.md)
			if got := allText(doc.Elements); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
			if len(doc.Warnings) != 0 {
				t.Errorf("warnings: %q", doc.Warnings)
			}
		})
	}
}

// The title of a code block, and the labels of tabs, are text of the
// document.
func TestTitlesAndTabLabels(t *testing.T) {
	text := allText(parseDoc(t, "mkdocs", "``` py title=\"main.py\"\nprint()\n```\n").Elements)
	if !strings.Contains(text, "main.py\nCODE[py]:print()") {
		t.Errorf("code block title: %q", text)
	}
	mdx := "<Tabs>\n  <TabItem value=\"npm\" label=\"With npm\">\n\n  Run npm.\n\n  </TabItem>\n  <TabItem value=\"yarn\">\n\n  Run yarn.\n\n  </TabItem>\n</Tabs>\n"
	text = allText(parseDoc(t, "docusaurus", mdx).Elements)
	if text != "With npm\nRun npm.\nyarn\nRun yarn.\n" {
		t.Errorf("tabs: %q", text)
	}
}
