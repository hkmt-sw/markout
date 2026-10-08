package render

import (
	"testing"

	"github.com/signintech/gopdf"
)

func TestGlyphBoxes(t *testing.T) {
	g, err := newGlyphBoxes(fontMath)
	if err != nil {
		t.Fatal(err)
	}
	within := func(what string, got, lo, hi float64) {
		t.Helper()
		if got < lo || got > hi {
			t.Errorf("%s is %.3f, want %.2f to %.2f", what, got, lo, hi)
		}
	}
	// A lower-case x sits on the baseline and is x-high
	lo, hi, ok := g.bounds('x')
	if !ok {
		t.Fatal("no x")
	}
	within("bottom of x", lo, -0.03, 0.03)
	within("top of x", hi, 0.4, 0.62)

	// p goes below the baseline, a capital goes higher than x
	if lo, _, _ := g.bounds('p'); lo > -0.1 {
		t.Errorf("bottom of p is %.3f, want it below the baseline", lo)
	}
	if _, capital, _ := g.bounds('H'); capital <= hi {
		t.Errorf("H (%.3f) is not taller than x (%.3f)", capital, hi)
	}
	// The italic x of formulas is outside the first 65536 characters
	if _, hi, ok := g.bounds('𝑥'); !ok || hi < 0.4 {
		t.Errorf("italic x: ok=%v top=%.3f", ok, hi)
	}
	// The minus sign floats at the height fraction bars are drawn at
	lo, hi, ok = g.bounds('−')
	if !ok {
		t.Fatal("no minus")
	}
	within("middle of the minus sign", (lo+hi)/2, 0.2, 0.4)

	if _, _, ok := g.bounds(' '); ok {
		t.Error("a space has no outline")
	}
	if _, _, ok := g.bounds(0x10FFFF); ok {
		t.Error("a character the font lacks has bounds")
	}
}

// The built-in text fonts can be read too: they have a simpler table.
func TestGlyphBoxesOfTextFont(t *testing.T) {
	g, err := newGlyphBoxes(fontSerifRegular)
	if err != nil {
		t.Fatal(err)
	}
	if _, hi, ok := g.bounds('x'); !ok || hi < 0.35 || hi > 0.6 {
		t.Errorf("x: ok=%v top=%.3f", ok, hi)
	}
	if _, hi, ok := g.bounds('é'); !ok || hi < 0.55 {
		t.Errorf("é: ok=%v top=%.3f", ok, hi)
	}
}

func TestGlyphBoxesRejectsGarbage(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not a font at all"), fontMath[:200]} {
		if _, err := newGlyphBoxes(data); err == nil {
			t.Errorf("%d bytes of garbage were read as a font", len(data))
		}
	}
}

// Widths read from the font agree with what gopdf measures.
func TestGlyphWidths(t *testing.T) {
	g, err := newGlyphBoxes(fontSansRegular)
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData("f", fontSansRegular); err != nil {
		t.Fatal(err)
	}
	pdf.SetFont("f", "", 10)
	for _, text := range []string{"Process the data", "Árvíztűrő tükörfúrógép", "Is it valid?", "i", "WWW"} {
		want, _ := pdf.MeasureTextWidth(text)
		if got := g.width(text, 10); got < want-0.2 || got > want+0.2 {
			t.Errorf("%q: %.2f, gopdf measures %.2f", text, got, want)
		}
	}
	if g.width("", 10) != 0 || g.advance(0x10FFFF) != 0 {
		t.Error("nothing should have no width")
	}
}
