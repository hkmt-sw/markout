package parse

import (
	"os"
	"path/filepath"
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
	md := "```{eval-rst}\n.. note:: RST\n```\n\n```{only} html\nONLY\n```\n\n```{raw} html\n<b>RAWHTML</b>\n```\n\n" +
		"```{raw} latex\n\\newpage RAWLATEX\n```\n\n```{bibliography}\n```\n\n```{bibliography}\n```\n\n" +
		"```{toctree}\n:caption: Guide\n:maxdepth: 2\n\nintro\nSetting up <install>\nself\n```\n\n```{mystery} ARGUMENT\nBODY\n```\n"
	doc := parseDoc(t, "myst", md)
	text := allText(doc.Elements)
	for _, want := range []string{"CODE[rst]:.. note:: RST", "ONLY", "RAWHTML", "Guide", "ARGUMENT", "BODY"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the document:\n%s", want, text)
		}
	}
	// The pages a toctree names are listed
	list, ok := findList(doc.Elements)
	if !ok || len(list.Items) != 2 || runsText(list.Items[0].Runs) != "intro" || runsText(list.Items[1].Runs) != "Setting up" {
		t.Errorf("toctree: %+v", list)
	}
	if strings.Contains(text, "RAWLATEX") {
		t.Errorf("markup for LaTeX should be left out:\n%s", text)
	}
	if len(doc.Warnings) != 2 {
		t.Fatalf("warnings: %q", doc.Warnings)
	}
	if w := doc.Warnings[0]; !strings.Contains(w, "3 parts") || !strings.Contains(w, "{raw} latex") || !strings.Contains(w, "{bibliography} (2)") {
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
		{"list-table without a header", "```{list-table}\n* - a\n  - b\n```\n", "TABLE:/a|b|\n"},
		{"csv-table", "```{csv-table} Caption\n:header: \"x\", \"y\"\n\n\"a, with comma\", 1\nb, 2\n```\n",
			"Caption\nTABLE:x|y|/a, with comma|1|/b|2|\n"},
		{"as MyST's own documentation writes them", "```{list-table}\n:header-rows: 1\n\n*   - Treat\n    - Price\n*   - Frog\n    - 1.49\n```\n\n" +
			"```{csv-table}\n:header: >\n:    \"Treat\", \"Price\"\n:widths: 15, 10\n\n\"Frog\", 1.49\n```\n",
			"TABLE:Treat|Price|/Frog|1.49|\nTABLE:Treat|Price|/Frog|1.49|\n"},
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

func findList(elems []ast.Element) (ast.List, bool) {
	for _, elem := range elems {
		if list, ok := elem.(ast.List); ok {
			return list, true
		}
	}
	return ast.List{}, false
}

// Found by converting MyST's own documentation: a formula closed with its
// label swallowed the rest of the document.
func TestMySTMath(t *testing.T) {
	doc := parseDoc(t, "myst", "$$\na = b\n$$ (my-label)\n\nAFTER the formula.\n\n```{math}\n:label: two\nx = 1\n\ny = 2\n```\n\n\\begin{align}\nc &= d\n\\end{align}\n\nEND\n")
	var formulas []string
	for _, elem := range doc.Elements {
		if m, ok := elem.(ast.MathBlock); ok {
			formulas = append(formulas, strings.TrimSpace(m.Expression))
		}
	}
	want := []string{"a = b", "x = 1", "y = 2", "\\begin{align}\nc &= d\n\\end{align}"}
	if strings.Join(formulas, "|") != strings.Join(want, "|") {
		t.Errorf("formulas: %q, want %q", formulas, want)
	}
	if text := allText(doc.Elements); text != "AFTER the formula.\nEND\n" {
		t.Errorf("text around the formulas: %q", text)
	}
}

func TestSphinxDirectives(t *testing.T) {
	tests := []struct{ name, md, want string }{
		{"versionadded", ":::{versionadded} 1.2\nA new thing.\n:::\n", "Added in version 1.2:\nA new thing.\n"},
		{"deprecated, nothing more", "```{deprecated} 3.0\n```\n", "Deprecated since version 3.0\n"},
		{"table caption", ":::{table} The caption\n| a |\n|---|\n| b |\n:::\n", "The caption\nTABLE:a|/b|\n"},
		{"tab set", "::::{tab-set}\n:::{tab-item} One\nfirst\n:::\n:::{tab-item} Two\nsecond\n:::\n::::\n", "One\nfirst\nTwo\nsecond\n"},
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

// A JSX component that stands by itself shows nothing in a document; the
// conversion names it. One that wraps content keeps the content.
func TestMDXComponentsAreReported(t *testing.T) {
	mdx := "<Chart data={points} />\n\nText with <Badge /> and `<InCode />`.\n\n<Wrapper>\n\nKEPT\n\n</Wrapper>\n\n" +
		"```jsx\n<InFence />\n```\n\n<Chart />\n\n<br />\n"
	doc := parseDoc(t, "docusaurus", mdx)
	if text := allText(doc.Elements); !strings.Contains(text, "KEPT") || strings.Contains(text, "Chart") {
		t.Errorf("text: %q", text)
	}
	if len(doc.Warnings) != 1 {
		t.Fatalf("warnings: %q", doc.Warnings)
	}
	w := doc.Warnings[0]
	for _, want := range []string{"3 parts", "<Chart /> (2)", "<Badge />"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning does not have %q: %s", want, w)
		}
	}
	for _, not := range []string{"InCode", "InFence", "Wrapper", "br"} {
		if strings.Contains(w, not) {
			t.Errorf("warning names %s: %s", not, w)
		}
	}
	// Other flavors do not read JSX
	if doc := parseDoc(t, "github", "<Chart />\n"); len(doc.Warnings) != 0 {
		t.Errorf("warnings: %q", doc.Warnings)
	}
}

// What MkDocs Material adds to Markdown: files put in with --8<--, keys,
// icons, and Markdown inside HTML.
func TestMaterial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part.md"), []byte("FROM **the** file.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "--8<-- \"part.md\"\n\n--8<-- \"missing.md\"\n\n--8<--\npart.md\n; off.md\n../outside.md\n--8<--\n\n" +
		"Press ++ctrl+alt+del++ then ++arrow-up++, :material-check: done. C++ and a++b++ stay.\n\n" +
		"<div class=\"grid cards\" markdown>\n\n- CARD\n\n</div>\n\n```\n++ctrl++ :material-x: --8<-- in code\n```\n"
	fl, _ := flavor.ByID("mkdocs")
	doc, err := ParseFlavor([]byte(md), fl, dir)
	if err != nil {
		t.Fatal(err)
	}
	text := allText(doc.Elements)
	for _, want := range []string{"FROM the file.\nFROM the file.\n", "Press Ctrl+Alt+Del then Arrow Up,  done. C++ and a++b++ stay.", "++ctrl++ :material-x: --8<-- in code"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the document:\n%s", want, text)
		}
	}
	if list, ok := findList(doc.Elements); !ok || runsText(list.Items[0].Runs) != "CARD" {
		t.Errorf("the Markdown inside the div is not read:\n%s", text)
	}
	if strings.Contains(text, "<div") || strings.Contains(text, "--8<-- \"") {
		t.Errorf("markup is left in the text:\n%s", text)
	}
	if len(doc.Warnings) != 2 {
		t.Fatalf("warnings: %q", doc.Warnings)
	}
	if w := doc.Warnings[0]; !strings.Contains(w, "1 part of the source") || !strings.Contains(w, "icons") {
		t.Errorf("left out: %s", w)
	}
	if w := doc.Warnings[1]; !strings.Contains(w, "2 files named by --8<--") || !strings.Contains(w, `"missing.md"`) || !strings.Contains(w, `"../outside.md"`) {
		t.Errorf("unread: %s", w)
	}
	// Other flavors leave all of it alone
	if doc := parseDoc(t, "github", "Press ++ctrl++ :material-check:\n"); !strings.Contains(allText(doc.Elements), "++ctrl++ :material-check:") {
		t.Errorf("GitHub flavor changed it: %q", allText(doc.Elements))
	}
}

// In MDX indentation does not make code: what is indented inside a
// component, or anywhere outside a list, is text, and fenced code keeps its
// own indentation.
func TestMDXIndentation(t *testing.T) {
	mdx := "<Tabs>\n  <TabItem value=\"a\" label=\"One\">\n    Indented with `code`.\n\n    ```js\n    if (x) {\n      y();\n    }\n    ```\n  </TabItem>\n</Tabs>\n\n" +
		"<details>\n<summary>S</summary>\n\n    Deep text.\n\n</details>\n\n    Loose indented text.\n\n- item\n\n      code in the item\n\nEND\n"
	doc := parseDoc(t, "docusaurus", mdx)
	text := allText(doc.Elements)
	for _, want := range []string{"One\nIndented with code.\nCODE[js]:if (x) {\n  y();\n}\n", "Deep text.\n", "Loose indented text.\n", "END\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the document:\n%s", want, text)
		}
	}
	// Under GitHub the same indentation makes code blocks
	if text := allText(parseDoc(t, "github", "A\n\n    Loose indented text.\n").Elements); !strings.Contains(text, "CODE[]:Loose indented text.") {
		t.Errorf("GitHub: %q", text)
	}
}

// A table whose header row is empty has no header.
func TestHeaderlessTable(t *testing.T) {
	doc := parseDoc(t, "github", "|   |   |\n|---|---|\n| a | b |\n| c | d |\n")
	table, ok := doc.Elements[0].(ast.Table)
	if !ok || len(table.Header.Cells) != 0 || len(table.Rows) != 2 {
		t.Errorf("table: %+v", doc.Elements[0])
	}
	doc = parseDoc(t, "github", "| h | i |\n|---|---|\n| a | b |\n")
	if table := doc.Elements[0].(ast.Table); len(table.Header.Cells) != 2 {
		t.Errorf("a table with a header lost it: %+v", table)
	}
}
