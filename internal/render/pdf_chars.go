package render

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/signintech/gopdf"
)

// A font draws only the characters it has glyphs for. gopdf puts a space in
// place of any other and says nothing, so the renderer asks first: usable
// leaves such characters out of the text and remembers them, and the
// conversion ends with a warning that names them.

// glyphOption makes gopdf report a character the font lacks, which is how
// drawable finds out.
func (r *PdfRenderer) glyphOption() gopdf.TtfOption {
	return gopdf.TtfOption{OnGlyphNotFound: func(rune) { r.glyphMissed = true }}
}

// drawable reports whether the current font has a glyph for c.
func (r *PdfRenderer) drawable(c rune) bool {
	if c == ' ' {
		return true
	}
	known := r.glyphs[r.face]
	if known == nil {
		known = map[rune]bool{}
		r.glyphs[r.face] = known
	}
	ok, seen := known[c]
	if !seen {
		r.glyphMissed = false
		r.pdf.MeasureTextWidth(string(c))
		ok = !r.glyphMissed
		known[c] = ok
	}
	switch {
	case !ok:
		r.noteMissing(c)
	case unicode.In(c, unicode.Hebrew, unicode.Arabic):
		r.rightToLeft = true
	}
	return ok
}

// usable returns text without the characters the current font cannot draw.
func (r *PdfRenderer) usable(text string) string {
	clean := true
	for _, c := range text {
		if !r.drawable(c) {
			clean = false
		}
	}
	if clean {
		return text
	}
	var b strings.Builder
	for _, c := range text {
		if r.drawable(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// noteMissing remembers a character that was left out. Characters that draw
// nothing of their own (control and formatting characters, joiners,
// combining marks) are not worth a warning.
func (r *PdfRenderer) noteMissing(c rune) {
	if unicode.In(c, unicode.Cc, unicode.Cf, unicode.M) || r.missingSeen[c] {
		return
	}
	if r.missingSeen == nil {
		r.missingSeen = map[rune]bool{}
	}
	r.missingSeen[c] = true
	r.missing = append(r.missing, c)
}

// warnings says what the PDF does not show the way the document has it.
func (r *PdfRenderer) warnings() []string {
	var out []string
	if n := len(r.missing); n > 0 {
		const show = 12
		shown := r.missing
		more := ""
		if n > show {
			shown = shown[:show]
			more = fmt.Sprintf(" and %d more", n-show)
		}
		chars := make([]string, len(shown))
		for i, c := range shown {
			chars[i] = string(c)
		}
		what := "characters are"
		if n == 1 {
			what = "character is"
		}
		out = append(out, fmt.Sprintf("%d %s not in the PDF fonts and left out: %s%s. A theme can use a font that has them.",
			n, what, strings.Join(chars, " "), more))
	}
	if w := r.mathProblems.warning(); w != "" {
		out = append(out, w)
	}
	if r.rightToLeft {
		out = append(out, "right-to-left text (Hebrew, Arabic) is not laid out in PDF: its letters come out in reverse order.")
	}
	return out
}

// charWidth is the width of one character in the current font and size.
func (r *PdfRenderer) charWidth(c rune) float64 {
	widths := r.widths[r.font]
	if widths == nil {
		widths = map[rune]float64{}
		r.widths[r.font] = widths
	}
	w, ok := widths[c]
	if !ok {
		w, _ = r.pdf.MeasureTextWidth(string(c))
		widths[c] = w
	}
	return w
}

// wrapChars breaks text that is wider than width into pieces that fit. It
// breaks at any character: code and URLs have no better place.
func (r *PdfRenderer) wrapChars(text string, width float64) []string {
	if w, _ := r.pdf.MeasureTextWidth(text); w <= width {
		return []string{text}
	}
	var out []string
	start, x := 0, 0.0
	for i, c := range text {
		w := r.charWidth(c)
		if x+w > width && i > start {
			out = append(out, text[start:i])
			start, x = i, 0
		}
		x += w
	}
	return append(out, text[start:])
}

// tabWidth is the distance between tab stops in code, in characters.
const tabWidth = 4

// codeLines prepares source code for drawing in the current font: one entry
// per line on the page, with tabs expanded and lines wider than width broken.
func (r *PdfRenderer) codeLines(code string, width float64) []string {
	var out []string
	// The code ends with a line break, which is not a line of its own
	for _, line := range strings.Split(strings.TrimSuffix(code, "\n"), "\n") {
		line = r.usable(expandTabs(strings.TrimSuffix(line, "\r")))
		if line == "" {
			line = " "
		}
		out = append(out, r.wrapChars(line, width)...)
	}
	return out
}

func expandTabs(line string) string {
	out, _ := expandTabsFrom(line, 0)
	return out
}

// expandTabsFrom expands the tabs of a piece of a line that starts at column
// col, and returns the column the piece ends at.
func expandTabsFrom(text string, col int) (string, int) {
	if !strings.Contains(text, "\t") {
		return text, col + len([]rune(text))
	}
	var b strings.Builder
	for _, c := range text {
		if c != '\t' {
			b.WriteRune(c)
			col++
			continue
		}
		n := tabWidth - col%tabWidth
		b.WriteString(strings.Repeat(" ", n))
		col += n
	}
	return b.String(), col
}

// wrapCode prepares highlighted code for drawing in the current font, as
// codeLines does plain code: one entry per line on the page, with tabs
// expanded, undrawable characters left out and lines wider than width
// broken, the colors kept.
func (r *PdfRenderer) wrapCode(lines [][]codeSpan, width float64) [][]codeSpan {
	var out [][]codeSpan
	for _, line := range lines {
		var cur []codeSpan
		col, x := 0, 0.0
		for _, span := range line {
			var text string
			text, col = expandTabsFrom(span.text, col)
			text = r.usable(text)
			start := 0
			for i, c := range text {
				w := r.charWidth(c)
				if x+w > width && x > 0 {
					if i > start {
						cur = append(cur, codeSpan{text[start:i], span.color})
					}
					out, cur = append(out, cur), nil
					start, x = i, 0
				}
				x += w
			}
			if start < len(text) {
				cur = append(cur, codeSpan{text[start:], span.color})
			}
		}
		if len(cur) == 0 {
			cur = []codeSpan{{" ", r.t.Code.BlockColor}}
		}
		out = append(out, cur)
	}
	return out
}

// minPanelLines is how many lines of a panel must fit under what is already
// on the page for a panel that will be split anyway to start there.
const minPanelLines = 3

// panel draws a box holding n lines of text, each lineHeight tall, inside
// padding. A panel that fits on one page is kept together. A taller one runs
// on over the following pages, the part on each page in a box of its own.
// box draws the background of a part and line draws line i at y.
func (r *PdfRenderer) panel(n int, lineHeight, padding float64, box func(y, h float64), line func(i int, y float64)) {
	bottom := r.pageHeight - r.marginBottom
	if full := float64(n)*lineHeight + 2*padding; full <= bottom-r.marginTop {
		r.checkPageBreak(full)
	} else {
		r.checkPageBreak(minPanelLines*lineHeight + 2*padding)
	}
	for i := 0; i < n; {
		fit := int((bottom-r.currentY-2*padding)/lineHeight + 1e-6)
		if fit < 1 {
			fit = 1 // a page too small for one line still has to take it
		}
		if fit > n-i {
			fit = n - i
		}
		h := float64(fit)*lineHeight + 2*padding
		box(r.currentY, h)
		for k := 0; k < fit; k++ {
			line(i+k, r.currentY+padding+float64(k)*lineHeight)
		}
		r.currentY += h
		if i += fit; i < n {
			r.addPage()
			r.currentY = r.marginTop
		}
	}
}
