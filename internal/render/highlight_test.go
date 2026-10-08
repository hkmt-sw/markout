package render

import (
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// spansOf writes highlighted lines as text{color} pieces, with the theme's
// colors by name.
func spansOf(lines [][]codeSpan, t theme.Theme) string {
	s := t.Code.Syntax
	names := map[theme.Color]string{
		s.Keyword: "keyword", s.String: "string", s.Comment: "comment", s.Number: "number",
		s.Function: "function", s.Type: "type", t.Code.BlockColor: "",
	}
	names[t.Colors.Inserted], names[t.Colors.Deleted] = "inserted", "deleted"
	var b strings.Builder
	for _, line := range lines {
		for _, span := range line {
			b.WriteString(span.text)
			if name := names[span.color]; name != "" {
				b.WriteString("{" + name + "}")
			}
			b.WriteString("|")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestHighlight(t *testing.T) {
	th := theme.Default().PDF
	th.Colors.Deleted = theme.Hex("AA0000") // the default is the keyword color
	tests := []struct {
		name, lang, code, want string
	}{
		{"go", "go", "// note\nfunc main() {\n\treturn \"x\" + 1\n}\n",
			"// note{comment}|\nfunc {keyword}|main{function}|() {|\n\treturn {keyword}|\"x\" {string}|+ |1{number}|\n}|\n"},
		{"language with options", "python title=a.py", "x = 1\n", "x = |1{number}|\n"},
		{"attribute form", "{.python}", "x = 1\n", "x = |1{number}|\n"},
		{"diff", "diff", "-old\n+new\n same\n", "-old{deleted}|\n+new{inserted}|\n same|\n"},
		{"blank line kept", "go", "a()\n\nb()\n", "a{function}|()|\n|\nb{function}|()|\n"},
		{"no language", "", "func main() {}\n", "func main() {}|\n"},
		{"unknown language", "nosuchlang", "func main() {}\n", "func main() {}|\n"},
		{"plain text", "text", "func main() {}\n", "func main() {}|\n"},
		{"windows line ends", "go", "a()\r\nb()\r\n", "a{function}|()|\nb{function}|()|\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spansOf(highlight(ast.CodeBlock{Language: tt.lang, Code: tt.code}, th), th)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// Whatever the lexer makes of it, the code that comes out is the code that
// went in.
func TestHighlightKeepsTheCode(t *testing.T) {
	th := theme.Default().PDF
	for lang, code := range map[string]string{
		"go":       "package main\n\nfunc main() {\n\tprintln(`raw\nstring`)\n}",
		"html":     "<p class=\"a\">x &amp; y</p>\n<!-- c -->",
		"markdown": "# T\n\n- a\n- b\n\n```go\nx\n```",
		"yaml":     "a:\n  - b: 1\n  - c: \"d\"\n",
		"sh":       "echo \"$HOME\" # x\n\n\n",
		"json":     "{\"a\": [1, 2]}",
	} {
		want := strings.TrimSuffix(code, "\n")
		if got := joined(highlight(ast.CodeBlock{Language: lang, Code: code}, th)); got != want {
			t.Errorf("%s: got %q, want %q", lang, got, want)
		}
	}
}

func TestHighlightCanBeTurnedOff(t *testing.T) {
	th := theme.Default().PDF
	th.Code.Syntax.Highlight = false
	if got := spansOf(highlight(ast.CodeBlock{Language: "go", Code: "func main() {}\n"}, th), th); got != "func main() {}|\n" {
		t.Errorf("got %q", got)
	}
}

// A long block is not lexed.
func TestHighlightSkipsHugeBlocks(t *testing.T) {
	th := theme.Default().PDF
	code := strings.Repeat("func f() {}\n", maxHighlightBytes/12+10)
	lines := highlight(ast.CodeBlock{Language: "go", Code: code}, th)
	if len(lines[0]) != 1 || lines[0][0].color != th.Code.BlockColor {
		t.Errorf("first line is %+v, want one plain span", lines[0])
	}
}

// Highlighted code is laid out like plain code: tabs expanded across spans,
// long lines broken, colors kept on both sides of the break.
func TestHighlightedCodeInPDF(t *testing.T) {
	trace := pdfTrace(t, "```go\n\tx := \""+strings.Repeat("s", 150)+"\" // end\n```\n")
	texts := drawnTexts(t, trace)
	if texts[0].text != "    x := " {
		t.Errorf("first piece is %q, want the tab expanded", texts[0].text)
	}
	lines := map[float64]bool{}
	sCount := 0
	for _, d := range texts {
		lines[d.y] = true
		sCount += strings.Count(d.text, "s")
	}
	if len(lines) < 2 {
		t.Errorf("the long line is drawn on %d line(s)", len(lines))
	}
	if sCount != 150 {
		t.Errorf("%d of the 150 characters are drawn", sCount)
	}
	th := theme.Default().PDF
	for what, color := range map[string]theme.Color{"string": th.Code.Syntax.String, "comment": th.Code.Syntax.Comment} {
		if !strings.Contains(trace, "#"+color.Hex()) {
			t.Errorf("nothing is drawn in the %s color", what)
		}
	}
}
