package dollarmath

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// collect returns the text of every InlineMath and the lines of every
// MathBlock in the document, in order, the lines of a block joined by "|".
func collect(t *testing.T, src string) (inline, block []string) {
	t.Helper()
	source := []byte(src)
	md := goldmark.New(goldmark.WithExtensions(Extension))
	doc := md.Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *InlineMath:
			var sb strings.Builder
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				sb.Write(c.(*ast.Text).Segment.Value(source))
			}
			inline = append(inline, sb.String())
		case *MathBlock:
			var lines []string
			for i := 0; i < n.Lines().Len(); i++ {
				l := n.Lines().At(i)
				lines = append(lines, strings.TrimRight(string(l.Value(source)), "\n"))
			}
			block = append(block, strings.Join(lines, "|"))
		}
		return ast.WalkContinue, nil
	})
	return inline, block
}

func TestInlineMath(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"one", "so $x^2$ here", []string{"x^2"}},
		{"one space each side is dropped", "$ x $", []string{"x"}},
		{"one space on one side only is kept", "$ x$", []string{" x"}},
		{"double dollars", "a $$x$$ b", []string{"x"}},
		{"a shorter run does not close a longer one", "x $$a$b$$ y", []string{"a$b"}},
		{"over two lines", "$a\nb$", []string{"a\nb"}},
		{"no closing dollar", "costs $5 today", nil},
		{"empty", "$$ and $", nil},
		{"two apart", "$a$ and $b$", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := collect(t, tt.in)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("collect(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMathBlock(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"one", "$$\na+b\n$$\n", []string{"a+b"}},
		{"several lines", "$$\na\nb\n$$\n", []string{"a|b"}},
		{"indent of the opening line is taken off", "  $$\n  a\n    b\n  $$\n", []string{"a|  b"}},
		{"ends a paragraph", "text\n$$\na\n$$\n", []string{"a"}},
		{"no closing line runs to the end", "$$\na\nb\n", []string{"a|b"}},
		{"four spaces is code, not math", "    $$\n    a\n    $$\n", nil},
		{"one dollar is not a block", "$\na\n$\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := collect(t, tt.in)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("collect(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNoPanic feeds the parsers lines that mix dollar signs with the other
// block markers, which is where a block can end in the middle of a container.
func TestNoPanic(t *testing.T) {
	pieces := []string{"$", "$$", " ", "    ", "\n", "a", "- ", "> ", "1. ", "`", "*"}
	// A small deterministic walk over the pieces instead of a random one.
	for seed := 0; seed < 20000; seed++ {
		var sb strings.Builder
		for n, x := 0, seed; n < 10; n++ {
			sb.WriteString(pieces[x%len(pieces)])
			x = x/len(pieces) + seed%7 + n
		}
		collect(t, sb.String())
	}
}
