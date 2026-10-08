package mathml

import (
	"strings"
	"testing"
)

// outline writes a tree as nested tags with the text of tokens.
func outline(n *Node) string {
	if n.IsToken() {
		return n.Tag + "(" + n.Text + ")"
	}
	parts := make([]string, len(n.Kids))
	for i, kid := range n.Kids {
		parts[i] = outline(kid)
	}
	return n.Tag + "[" + strings.Join(parts, " ") + "]"
}

func TestParse(t *testing.T) {
	tests := []struct{ tex, want string }{
		{`x^2`, `mrow[msup[mi(x) mn(2)]]`},
		{`\frac{a}{b}`, `mrow[mfrac[mi(a) mi(b)]]`},
		{`\sqrt{x+1}`, `mrow[msqrt[mrow[mi(x) mo(+) mn(1)]]]`},
		{`\alpha \le \infty`, `mrow[mi(α) mo(≤) mi(∞)]`},
		{`\text{if } x`, "mrow[mtext(if\u00a0) mi(x)]"},
		{`\operatorname{rank} A`, `mrow[mpadded[mi(r) mi(a) mi(n) mi(k)] mi(A)]`},
		{`\boxed{x} \label{eq:1}`, `mrow[mi(x)]`},
		{`\mathbb{R}`, `mrow[mi(ℝ)]`},
		// Lines broken with \\ need no environment around them
		{"a &= b \\\\ &= c", `mrow[mtable[mtr[mtd[mi(a)] mtd[mo(=) mi(b)]] mtr[mtd[] mtd[mo(=) mi(c)]]]]`},
		{"\\begin{gather*} a \\\\ b \\end{gather*}", `mrow[mtable[mtr[mtd[mi(a)]] mtr[mtd[mi(b)]]]]`},
		{"\\begin{align*} a &= b \\end{align*}", `mrow[mtable[mtr[mtd[mi(a)] mtd[mo(=) mi(b)]]]]`},
		{"\\begin{equation} x \\end{equation}", `mrow[mi(x)]`},
	}
	for _, tt := range tests {
		root, err := Parse(tt.tex, true)
		if err != nil {
			t.Errorf("%s: %v", tt.tex, err)
			continue
		}
		if got := outline(root); got != tt.want {
			t.Errorf("%s:\n got  %s\n want %s", tt.tex, got, tt.want)
		}
	}
}

// What cannot be typeset is an error that says why, not a tree with holes.
func TestParseErrors(t *testing.T) {
	for tex, want := range map[string]string{
		`x^{`:                            "curly brace",
		`\nosuchcommand{x}`:              `\nosuchcommand is not a command`,
		strings.Repeat("x", MaxLength+1): "longer than",
	} {
		_, err := Parse(tex, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.20s: error %v, want one mentioning %q", tex, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "<") {
			t.Errorf("%.20s: the error carries markup: %v", tex, err)
		}
	}
}

// The letters of a name such as \operatorname{rank} are upright.
func TestUprightNames(t *testing.T) {
	root, err := Parse(`\operatorname{rank}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if mi := find(root, "mi"); mi.Attr["mathvariant"] != "normal" {
		t.Errorf("the letters have mathvariant %q, want normal", mi.Attr["mathvariant"])
	}
}

func TestAttributes(t *testing.T) {
	root, err := Parse(`\left( x \right) \begin{aligned} a &= b \end{aligned}`, true)
	if err != nil {
		t.Fatal(err)
	}
	fence := find(root, "mo")
	if !fence.Is("stretchy") || !fence.Is("fence") {
		t.Errorf("( of \\left is %+v, want a stretchy fence", fence.Attr)
	}
	if cell := find(root, "mtd"); cell.CSS("text-align") != "right" {
		t.Errorf("first cell of aligned has text-align %q, want right", cell.CSS("text-align"))
	}
}

func TestStyledLetters(t *testing.T) {
	for styled, want := range map[rune]struct {
		base    rune
		variant Variant
	}{
		'𝐀': {'A', Bold}, '𝑥': {'x', Italic}, 'ℎ': {'h', Italic}, 'ℝ': {'R', DoubleStruck},
		'𝔸': {'A', DoubleStruck}, 'ℒ': {'L', Script}, '𝒜': {'A', Script}, '𝟏': {'1', Bold},
		'x': {'x', Plain}, 'α': {'α', Plain}, '∑': {'∑', Plain}, '𝛼': {'α', Italic}, '𝜔': {'ω', Italic},
	} {
		if base, variant := Unstyle(styled); base != want.base || variant != want.variant {
			t.Errorf("%c: got %c/%d, want %c/%d", styled, base, variant, want.base, want.variant)
		}
	}
	for plain, want := range map[rune]rune{'x': '𝑥', 'A': '𝐴', 'h': 'ℎ', 'α': '𝛼', '2': '2', 'Γ': 'Γ'} {
		if got := ItalicLetter(plain); got != want {
			t.Errorf("italic %c: got %c, want %c", plain, got, want)
		}
	}
}
