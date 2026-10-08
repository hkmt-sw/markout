package render

import (
	"encoding/xml"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// drawnAt is a piece of text in a PDF trace, with where and how it is drawn.
type drawnAt struct {
	x, y, size float64
	font, text string
}

var traceTextAt = regexp.MustCompile(`^p\d+ text  x=([\d.]+) y=([\d.]+) ([^/\s]+)/([\d.]+) ("(?:[^"\\]|\\.)*")`)

func drawnWith(t *testing.T, trace string) []drawnAt {
	t.Helper()
	var out []drawnAt
	for _, line := range strings.Split(trace, "\n") {
		m := traceTextAt.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		size, _ := strconv.ParseFloat(m[4], 64)
		text, _ := strconv.Unquote(m[5])
		out = append(out, drawnAt{x, y, size, m[3], text})
	}
	return out
}

func find(t *testing.T, texts []drawnAt, text string) drawnAt {
	t.Helper()
	for _, d := range texts {
		if d.text == text {
			return d
		}
	}
	t.Fatalf("%q is not drawn; drawn: %+v", text, texts)
	return drawnAt{}
}

// A formula is typeset in the math font: variables in italics, an exponent
// smaller and higher than its base.
func TestFormulaIsTypeset(t *testing.T) {
	trace, warnings := pdfWarnings(t, "The area is $x^2$.\n")
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	texts := drawnWith(t, trace)
	base, exponent := find(t, texts, "𝑥"), find(t, texts, "2")
	if base.font != "Math" || exponent.font != "Math" {
		t.Errorf("the formula is drawn in %s and %s, want the math font", base.font, exponent.font)
	}
	if exponent.size >= base.size || exponent.y >= base.y || exponent.x <= base.x {
		t.Errorf("the exponent (%+v) should be smaller than, above and right of its base (%+v)", exponent, base)
	}
	if strings.Contains(trace, `"x^2"`) {
		t.Error("the source of the formula is drawn too")
	}
}

// A formula in a line of text stands on the baseline of the text.
func TestInlineFormulaIsOnTheBaseline(t *testing.T) {
	texts := drawnWith(t, pdfTrace(t, "Text $x$ more.\n"))
	word, variable := find(t, texts, "Text"), find(t, texts, "𝑥")
	wordBaseline := word.y + typoAscent(fontSansRegular, 0)*word.size
	formulaBaseline := variable.y + typoAscent(fontMath, 0)*variable.size
	if math.Abs(wordBaseline-formulaBaseline) > 0.15 {
		t.Errorf("the text's baseline is at %.2f and the formula's at %.2f", wordBaseline, formulaBaseline)
	}
	if variable.x <= word.x {
		t.Errorf("the formula (x=%.1f) should follow the word (x=%.1f)", variable.x, word.x)
	}
}

func traceLines(trace, kind string) []string {
	var out []string
	for _, line := range strings.Split(trace, "\n") {
		if strings.Contains(line, " "+kind+" ") {
			out = append(out, line)
		}
	}
	return out
}

// The parts of a formula are where they belong.
func TestFormulaLayout(t *testing.T) {
	t.Run("fraction", func(t *testing.T) {
		trace := pdfTrace(t, "$$\n\\frac{a}{b}\n$$\n")
		texts := drawnWith(t, trace)
		num, den := find(t, texts, "𝑎"), find(t, texts, "𝑏")
		bars := traceLines(trace, "line")
		if len(bars) != 1 {
			t.Fatalf("%d lines are drawn, want the fraction bar: %q", len(bars), bars)
		}
		barY, _ := strconv.ParseFloat(regexp.MustCompile(`y=([\d.]+)`).FindStringSubmatch(bars[0])[1], 64)
		// Text is placed by its top; the bar is between the two baselines
		ascent := typoAscent(fontMath, 0)
		if numBase, denBase := num.y+ascent*num.size, den.y+ascent*den.size; !(numBase < barY && barY < denBase) {
			t.Errorf("numerator baseline %.1f, bar %.1f, denominator baseline %.1f: not in that order", numBase, barY, denBase)
		}
	})

	t.Run("limits of a sum", func(t *testing.T) {
		texts := drawnWith(t, pdfTrace(t, "$$\n\\sum_{i=1}^{n} x\n$$\n"))
		sum, lower, upper := find(t, texts, "∑"), find(t, texts, "𝑖"), find(t, texts, "𝑛")
		if !(upper.y < sum.y && sum.y < lower.y) {
			t.Errorf("on its own line a sum has its limits over (y=%.1f) and under (y=%.1f) it (y=%.1f)", upper.y, lower.y, sum.y)
		}
		if sum.size <= lower.size*1.5 {
			t.Errorf("the sum sign (%.1f pt) should be large", sum.size)
		}

		// In a line of text there is no room for that
		texts = drawnWith(t, pdfTrace(t, "Sum $\\sum_{i=1}^{n} x$.\n"))
		sum, lower = find(t, texts, "∑"), find(t, texts, "𝑖")
		if lower.x <= sum.x {
			t.Errorf("in a line of text the limits go beside the sum: sum x=%.1f, limit x=%.1f", sum.x, lower.x)
		}
	})

	t.Run("brackets grow", func(t *testing.T) {
		tall := pdfTrace(t, "$$\n\\left( \\frac{a}{b} \\right)\n$$\n")
		if n := len(traceLines(tall, "curve")); n != 2 {
			t.Errorf("%d curves are drawn around the fraction, want 2 brackets", n)
		}
		if strings.Contains(tall, `"("`) {
			t.Error("the bracket is drawn as a character as well")
		}
		short := pdfTrace(t, "$$\n\\left( a \\right) + (b)\n$$\n")
		if n := len(traceLines(short, "curve")); n != 0 {
			t.Errorf("%d curves are drawn around a letter, want the bracket characters", n)
		}
	})

	t.Run("matrix", func(t *testing.T) {
		texts := drawnWith(t, pdfTrace(t, "$$\n\\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix}\n$$\n"))
		a, b, c, d := find(t, texts, "𝑎"), find(t, texts, "𝑏"), find(t, texts, "𝑐"), find(t, texts, "𝑑")
		if a.y != b.y || c.y != d.y || a.y >= c.y {
			t.Errorf("rows are at y=%.1f/%.1f and %.1f/%.1f", a.y, b.y, c.y, d.y)
		}
		if a.x >= b.x || math.Abs(a.x-c.x) > 1 || math.Abs(b.x-d.x) > 1 {
			t.Errorf("columns are at x=%.1f/%.1f and %.1f/%.1f", a.x, c.x, b.x, d.x)
		}
	})

	t.Run("root", func(t *testing.T) {
		trace := pdfTrace(t, "$$\n\\sqrt{x}\n$$\n")
		if n := len(traceLines(trace, "line")); n != 4 {
			t.Errorf("the root sign is %d lines, want 4", n)
		}
	})
}

// A formula that cannot be typeset is shown as its source, and the
// conversion says so.
func TestFormulaFallsBackToItsSource(t *testing.T) {
	trace, warnings := pdfWarnings(t, "Good $a+b$, bad $\\nosuchcommand{x}$ and $y^{$.\n\n$$\n\\alsonot{z}\n$$\n")
	for _, source := range []string{`\\nosuchcommand{x}`, `y^{`, `\\alsonot{z}`} {
		if !strings.Contains(trace, source) {
			t.Errorf("the source %s is not shown:\n%s", source, trace)
		}
	}
	if !strings.Contains(trace, "#"+theme.Default().PDF.Colors.Math.Hex()) {
		t.Error("the source is not in the color of formulas")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "3 formulas") || !strings.Contains(warnings[0], "nosuchcommand") {
		t.Errorf("warnings: %q", warnings)
	}
	if find(t, drawnWith(t, trace), "𝑎").font != "Math" {
		t.Error("the good formula is not typeset")
	}
}

// A formula wider than the text is made smaller to fit, and one far too
// wide is shown as its source, broken into lines.
func TestWideFormulas(t *testing.T) {
	page := theme.Default().PDF.Page
	right := page.Width - page.MarginRight

	terms := make([]string, 22)
	for i := range terms {
		terms[i] = "a_{" + strconv.Itoa(i+10) + "}"
	}
	texts := drawnWith(t, pdfTrace(t, "$$\n"+strings.Join(terms, " + ")+"\n$$\n"))
	last := texts[len(texts)-1]
	if last.font != "Math" || last.x > right {
		t.Errorf("the last piece is %+v, want it typeset left of x=%.1f", last, right)
	}
	if first := find(t, texts, "𝑎"); first.size >= theme.Default().PDF.Text.Size {
		t.Errorf("the formula is set at %.1f pt, want it smaller than the text to fit", first.size)
	}

	trace, warnings := pdfWarnings(t, "$$\n"+strings.Repeat("x + ", 150)+"y\n$$\n")
	if strings.Contains(trace, " Math/") {
		t.Error("a formula many lines long is typeset")
	}
	if len(warnings) != 0 {
		t.Errorf("a formula that is only too wide is not a problem to report: %q", warnings)
	}
}

func TestMathSpacing(t *testing.T) {
	if got := mathSpace(mathOrd, mathRel, 0); got <= mathSpace(mathOrd, mathBin, 0) || got <= 0 {
		t.Errorf("a relation has %.3f em around it, want more than an operation", got)
	}
	if got := mathSpace(mathOrd, mathBin, 1); got != 0 {
		t.Errorf("an operation in a script has %.3f em around it, want none", got)
	}
	if got := mathSpace(mathOrd, mathOrd, 0); got != 0 {
		t.Errorf("two letters have %.3f em between them, want none", got)
	}
	// The minus of -x is a sign, not an operation
	texts := drawnWith(t, pdfTrace(t, "$-x$ and $a-x$\n"))
	var minus, x []drawnAt
	for _, d := range texts {
		switch d.text {
		case "−":
			minus = append(minus, d)
		case "𝑥":
			x = append(x, d)
		}
	}
	if len(minus) != 2 || len(x) != 2 {
		t.Fatalf("drawn: %+v", texts)
	}
	if sign, operation := x[0].x-minus[0].x, x[1].x-minus[1].x; sign >= operation {
		t.Errorf("x is %.2f after a sign and %.2f after an operation; want it closer to the sign", sign, operation)
	}
}

// Whatever is between the dollar signs, the conversion finishes: formulas
// that are empty, lopsided or nested deep are typeset or shown as source.
func TestOddFormulas(t *testing.T) {
	odd := []string{
		``, ` `, `^`, `_`, `x^`, `{}`, `\frac{}{}`, `\sqrt{}`, `\sqrt[]{x}`, `\left( \right)`, `\left. x \right|`,
		`\begin{matrix}\end{matrix}`, `\begin{pmatrix} a & \\ & d \end{pmatrix}`, `\begin{cases}\end{cases}`,
		`\hat{}`, `\vec{}`, `\overline{}`, `\underbrace{}_{}`, `\sum_{}^{}`, `\int`, `\lim`, `\text{}`, `\mathbf{}`,
		`x_1^2_3`, `a'''`, `\color{nosuchcolor}{x}`, `\\`, `&`, `\,\;\quad`, `|`, `\|`, `\{`, `\}`, `%`,
		strings.Repeat(`\frac{1}{`, 40) + `x` + strings.Repeat(`}`, 40),
		strings.Repeat(`\sqrt{`, 40) + `x` + strings.Repeat(`}`, 40),
		strings.Repeat(`x^{`, 40) + `y` + strings.Repeat(`}`, 40),
		strings.Repeat(`\left(`, 30) + `x` + strings.Repeat(`\right)`, 30),
		`x \in 你好 🎉 ∀ é`,
	}
	var md strings.Builder
	for _, tex := range odd {
		md.WriteString("Inline $" + tex + "$ here.\n\n$$\n" + tex + "\n$$\n\n")
	}
	doc, err := parse.ParseFlavor([]byte(md.String()), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderPdf(doc, filepath.Join(dir, "out.pdf"), Options{}); err != nil {
		t.Errorf("PDF: %v", err)
	}
	out := filepath.Join(dir, "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatalf("DOCX: %v", err)
	}
	if err := xml.Unmarshal([]byte(docxPart(t, out, "word/document.xml")), new(struct{})); err != nil {
		t.Errorf("document.xml is not well-formed: %v", err)
	}
}
