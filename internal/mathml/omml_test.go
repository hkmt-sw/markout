package mathml

import (
	"encoding/xml"
	"strings"
	"testing"
)

func omml(t *testing.T, tex string, display bool) string {
	t.Helper()
	root, err := Parse(tex, display)
	if err != nil {
		t.Fatalf("%s: %v", tex, err)
	}
	return OMML(root, display)
}

func TestOMML(t *testing.T) {
	run := func(text string) string { return `<m:r><m:t xml:space="preserve">` + text + `</m:t></m:r>` }
	tests := []struct{ name, tex, want string }{
		{"variable", `x`, run("x")},
		{"superscript", `x^2`, `<m:sSup><m:e>` + run("x") + `</m:e><m:sup>` + run("2") + `</m:sup></m:sSup>`},
		{"fraction", `\frac{a}{b}`, `<m:f><m:num>` + run("a") + `</m:num><m:den>` + run("b") + `</m:den></m:f>`},
		{"binomial has no bar", `\binom{n}{k}`, `<m:fPr><m:type m:val="noBar"/></m:fPr>`},
		{"square root", `\sqrt{x}`, `<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/><m:e>` + run("x") + `</m:e></m:rad>`},
		{"cube root", `\sqrt[3]{x}`, `<m:rad><m:deg>` + run("3") + `</m:deg><m:e>` + run("x") + `</m:e></m:rad>`},
		{"function name is upright", `\sin x`, `<m:r><m:rPr><m:sty m:val="p"/></m:rPr><m:t xml:space="preserve">sin</m:t></m:r>`},
		{"text", `\text{if}`, `<m:r><m:rPr><m:nor/></m:rPr><m:t xml:space="preserve">if</m:t></m:r>`},
		{"bold", `\mathbf{A}`, `<m:r><m:rPr><m:sty m:val="b"/></m:rPr><m:t xml:space="preserve">A</m:t></m:r>`},
		{"blackboard bold", `\mathbb{R}`, `<m:r><m:rPr><m:scr m:val="double-struck"/></m:rPr><m:t xml:space="preserve">R</m:t></m:r>`},
		{"brackets that grow", `\left( x \right)`, `<m:d><m:dPr><m:begChr m:val="("/><m:endChr m:val=")"/></m:dPr><m:e>` + run("x") + `</m:e></m:d>`},
		{"sum takes its operand", `\sum_{i=1}^{n} x_i = S`,
			`<m:nary><m:naryPr><m:chr m:val="∑"/><m:limLoc m:val="undOvr"/></m:naryPr><m:sub>` + run("i") + run("=") + run("1") + `</m:sub><m:sup>` + run("n") +
				`</m:sup><m:e><m:sSub><m:e>` + run("x") + `</m:e><m:sub>` + run("i") + `</m:sub></m:sSub></m:e></m:nary>` + run("=") + run("S")},
		{"integral has limits beside it", `\int_0^1 f`, `<m:limLoc m:val="subSup"/>`},
		{"sum without limits", `\sum x`, `<m:subHide m:val="1"/><m:supHide m:val="1"/>`},
		{"hat", `\hat{y}`, `<m:acc><m:accPr><m:chr m:val="` + "\u0302" + `"/></m:accPr><m:e>` + run("y") + `</m:e></m:acc>`},
		{"overline", `\overline{AB}`, `<m:bar><m:barPr><m:pos m:val="top"/></m:barPr><m:e>` + run("A") + run("B") + `</m:e></m:bar>`},
		{"limit", `\lim_{x \to 0} f`, `<m:limLow><m:e><m:r><m:rPr><m:sty m:val="p"/></m:rPr><m:t xml:space="preserve">lim</m:t></m:r></m:e><m:lim>`},
		{"matrix", `\begin{pmatrix} a & b \\ c & d \end{pmatrix}`,
			`<m:mr><m:e>` + run("a") + `</m:e><m:e>` + run("b") + `</m:e></m:mr><m:mr><m:e>` + run("c") + `</m:e><m:e>` + run("d") + `</m:e></m:mr></m:m>`},
		{"cases have one brace", `\begin{cases} 1 \\ 0 \end{cases}`, `<m:begChr m:val="{"/><m:endChr m:val=""/>`},
		{"aligned columns", `\begin{aligned} a &= b \end{aligned}`, `<m:mcJc m:val="right"/></m:mcPr></m:mc><m:mc><m:mcPr><m:count m:val="1"/><m:mcJc m:val="left"/>`},
		{"less than is escaped", `a < b`, run("&lt;")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := omml(t, tt.tex, false)
			if !strings.Contains(got, tt.want) {
				t.Errorf("%s:\n got  %s\n want %s", tt.tex, got, tt.want)
			}
		})
	}
}

// Whatever the formula, what is written is well-formed, and wrapped as Word
// expects.
func TestOMMLIsWellFormed(t *testing.T) {
	for _, tex := range []string{
		`x=\frac{-b\pm\sqrt{b^2-4ac}}{2a}`,
		`\sum_{i=1}^{n} x_i^2 \le \int_0^\infty e^{-x}\,dx`,
		`f(x) = \begin{cases} 1 & \text{if } x > 0 \\ 0 & \text{otherwise} \end{cases}`,
		`\underbrace{a+b}_{n} \vec{v} \cdot \tilde{w} \quad \mathcal{L} \& "q"`,
		`\prod_{k} \left[ \frac{1}{k} \right] \to \infty`,
		`\sum`,
	} {
		for _, display := range []bool{false, true} {
			got := omml(t, tex, display)
			wrapped := `<x xmlns:m="m">` + got + `</x>`
			if err := xml.Unmarshal([]byte(wrapped), new(struct{})); err != nil {
				t.Errorf("%s: not well-formed: %v\n%s", tex, err, got)
			}
			if display != strings.HasPrefix(got, `<m:oMathPara><m:oMath>`) || !strings.HasSuffix(got, `</m:oMath>`) && !display {
				t.Errorf("%s (display %v) is wrapped as %.40s", tex, display, got)
			}
			if strings.Contains(got, "<m:e></m:e>") || strings.Contains(got, "<m:e/>") {
				t.Errorf("%s: an empty argument, which Word shows as a box: %s", tex, got)
			}
		}
	}
}
