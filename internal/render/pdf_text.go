package render

import (
	"math"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// Every block of text in the PDF (paragraphs, headings, list items, table
// cells, quotes, callouts, footnotes) goes through the same two steps: layout
// breaks the inline runs into lines of styled fragments, and drawLines puts
// them on the page. Laying out first is what lets a table row or a callout
// size its box before anything is drawn.

// textStyle is the base look of a block of text; the formatting of individual
// runs is applied on top of it.
type textStyle struct {
	font       string  // font family
	size       float64 // font size of normal text, in points
	bold       bool
	italic     bool
	color      theme.Color
	lineHeight float64
}

// bodyStyle is the style of ordinary text.
func (r *PdfRenderer) bodyStyle() textStyle {
	return textStyle{font: r.bodyFont(), size: r.t.Text.Size, color: r.t.Text.Color, lineHeight: r.t.Text.LineHeight}
}

// fragment is a piece of one run on one line: one or more words that share
// their formatting and are drawn with a single call.
type fragment struct {
	text     string
	x, width float64 // x is relative to the start of the line
	run      ast.InlineRun
	font     string // family passed to setFont
	style    string // "", "B", "I" or "BI"
	size     float64
	yShift   float64 // superscripts and subscripts leave the line
	color    theme.Color
	chip     bool     // a color swatch is drawn before the text
	math     *mathBox // a typeset formula, drawn in place of text
}

type textLine struct {
	frags []fragment
}

// width is the extent of the line's text.
func (l textLine) width() float64 {
	if len(l.frags) == 0 {
		return 0
	}
	last := l.frags[len(l.frags)-1]
	return last.x + last.width
}

const chipWidth = 12.0

// layout breaks runs into lines. The first line may be narrower or wider than
// the rest (a list item's first line starts after its bullet).
func (r *PdfRenderer) layout(runs []ast.InlineRun, base textStyle, firstWidth, restWidth float64) []textLine {
	// The gap between two runs is a space of the surrounding text, not of the
	// run that follows: a monospace space before inline code is too wide.
	baseStyle := ""
	if base.bold {
		baseStyle = "B"
	}
	r.setFont(base.font, baseStyle, base.size)
	baseSpace, _ := r.pdf.MeasureTextWidth(" ")

	b := lineBreaker{width: firstWidth, restWidth: restWidth, narrowest: math.Min(firstWidth, restWidth), baseSpace: baseSpace}
	for _, run := range runs {
		text := run.Text
		if run.FootnoteIndex > 0 {
			text = "[" + itoa(run.FootnoteIndex) + "]"
		}
		if text == "" {
			continue
		}
		f := r.fragmentOf(run, base)
		if run.Math && r.layoutMath(&b, f, base) {
			continue
		}
		r.layoutText(&b, f, text)
	}
	return b.finish()
}

// lineBreaker collects fragments into lines.
type lineBreaker struct {
	lines     []textLine
	line      textLine // the line being filled
	x         float64  // where its text ends
	width     float64  // how wide it may be
	restWidth float64  // how wide the lines after it may be
	narrowest float64
	baseSpace float64 // a space of the surrounding text
	needSpace bool    // the previous run ended in whitespace
}

func (b *lineBreaker) breakLine() {
	b.lines = append(b.lines, b.line)
	b.line = textLine{}
	b.x = 0
	b.width = b.restWidth
}

func (b *lineBreaker) add(f fragment) {
	b.line.frags = append(b.line.frags, f)
	b.x += f.width
}

func (b *lineBreaker) finish() []textLine {
	if len(b.line.frags) > 0 {
		b.lines = append(b.lines, b.line)
	}
	return b.lines
}

// fragmentOf is a fragment with the look of a run, with no text or place yet.
func (r *PdfRenderer) fragmentOf(run ast.InlineRun, base textStyle) fragment {
	f := fragment{run: run, font: base.font, size: base.size, color: base.color}
	bold, italic := run.Bold || base.bold, run.Italic || run.Math || base.italic
	switch {
	case bold && italic:
		f.style = "BI"
	case bold:
		f.style = "B"
	case italic:
		f.style = "I"
	}
	// Superscripts and subscripts are set smaller and shifted off the line.
	if run.Superscript || run.Subscript {
		f.size = math.Round(base.size * 0.73)
		f.yShift = -1
		if run.Subscript {
			f.yShift = base.size - f.size + 2
		}
	}
	if run.Code {
		f.font, f.style, f.size = r.codeFont(), "", base.size-1
		// Text is placed by the top of its box, and the monospace font
		// has a lower ascent: move it down onto the shared baseline.
		f.yShift = r.family(base.font).ascent*base.size - r.family(f.font).ascent*f.size
	}
	if c, ok := r.runColor(run); ok {
		f.color = c
	}
	return f
}

// runColor is the color of a run that has one of its own.
func (r *PdfRenderer) runColor(run ast.InlineRun) (theme.Color, bool) {
	switch {
	case run.Math:
		return r.t.Colors.Math, true
	case run.Link != "":
		return r.t.Link.Color, true
	case run.Inserted:
		return r.t.Colors.Inserted, true
	case run.Deleted:
		return r.t.Colors.Deleted, true
	case run.Code:
		return r.t.Code.Color, true
	case run.FootnoteIndex > 0:
		return r.t.Link.Color, true
	case run.Strikethrough:
		return r.t.Text.Faint, true
	}
	return theme.Color{}, false
}

// layoutMath typesets a formula, which takes its place in the line like a
// word. It reports false for one that cannot be typeset or is wider than
// the line, which is shown as its source.
func (r *PdfRenderer) layoutMath(b *lineBreaker, f fragment, base textStyle) bool {
	box, ok := r.typeset(f.run.Text, false, base.size, base.color)
	if !ok || box.w > b.narrowest {
		return false
	}
	gap := b.needSpace && b.x > 0
	if need := box.w + b.baseSpace; b.x+need > b.width && b.x > 0 {
		b.breakLine()
		gap = false
	}
	if gap {
		b.x += b.baseSpace
	}
	f.color, f.style = base.color, ""
	f.x, f.width, f.math = b.x, box.w, &box
	b.add(f)
	b.needSpace = false
	return true
}

// layoutText puts the words of a run into lines.
//
// A space is only laid out where the source has whitespace, so adjacent runs
// ("x" + superscript "2", bold text followed by a comma) stay together.
func (r *PdfRenderer) layoutText(b *lineBreaker, f fragment, text string) {
	r.setFont(f.font, f.style, f.size)
	// Characters the font cannot draw are left out
	if text = r.usable(text); text == "" {
		return
	}
	spaceWidth, _ := r.pdf.MeasureTextWidth(" ")
	p := runPlacer{b: b, f: f, spaceWidth: spaceWidth, chip: f.run.ColorChip != ""}

	leadingSpace := strings.TrimLeft(text, " \t\n") != text
	for i, word := range strings.Fields(text) {
		// The gap before a run is a space of the text around it
		gapWidth := spaceWidth
		if i == 0 {
			gapWidth = b.baseSpace
		}
		for pi, part := range r.wordParts(word, b.narrowest) {
			partWidth, _ := r.pdf.MeasureTextWidth(part)
			gap := pi == 0 && (i > 0 || leadingSpace || b.needSpace) && b.x > 0
			p.place(part, partWidth, gap, gapWidth)
		}
	}
	b.needSpace = strings.TrimRight(text, " \t\n") != text
}

// wordParts is a word as the pieces that are laid out. A word wider than a
// line (a URL, a path) is broken into parts that fit, which follow each
// other without a space.
func (r *PdfRenderer) wordParts(word string, narrowest float64) []string {
	if w, _ := r.pdf.MeasureTextWidth(word); w > narrowest && narrowest > 0 {
		return r.wrapChars(word, narrowest)
	}
	return []string{word}
}

// runPlacer puts the words of one run into lines. Words of the run that
// follow each other on a line are merged into one fragment.
type runPlacer struct {
	b          *lineBreaker
	f          fragment // the look of the run
	spaceWidth float64  // a space in the font of the run
	chip       bool     // a color swatch is still to be drawn before the text
	open       bool     // a fragment of this run is open at the end of the line
}

// place puts a word, or a part of one, at the end of the line or on a new
// line. gap says whether a space of gapWidth comes before it.
func (p *runPlacer) place(part string, partWidth float64, gap bool, gapWidth float64) {
	b := p.b
	need := partWidth
	if gap {
		need += gapWidth
	}
	if p.chip {
		need += chipWidth
	}
	if b.x+need > b.width && b.x > 0 {
		b.breakLine()
		gap, p.open = false, false
	}

	if p.open && gap && !p.chip {
		last := &b.line.frags[len(b.line.frags)-1]
		last.text += " " + part
		last.width += p.spaceWidth + partWidth
		b.x += p.spaceWidth + partWidth
		return
	}

	if gap {
		b.x += gapWidth
	}
	piece := p.f
	piece.text, piece.x, piece.width, piece.chip = part, b.x, partWidth, p.chip
	if p.chip {
		piece.x += chipWidth
		b.x += chipWidth
		p.chip = false
	}
	b.add(piece)
	p.open = true
}

// drawLinesAt draws laid-out lines with their first baseline box at (x, y),
// without touching the page position. firstX applies to the first line.
func (r *PdfRenderer) drawLinesAt(lines []textLine, firstX, restX, y, lineHeight float64) {
	for i, line := range lines {
		x := restX
		if i == 0 {
			x = firstX
		}
		r.drawLine(line, x, y+float64(i)*lineHeight, lineHeight)
	}
}

// drawLines draws laid-out lines at the current position, moving down the
// page and onto a new one as needed.
func (r *PdfRenderer) drawLines(lines []textLine, firstX, restX, lineHeight float64) {
	for i, line := range lines {
		r.checkPageBreak(lineHeight)
		x := restX
		if i == 0 {
			x = firstX
		}
		r.drawLine(line, x, r.currentY, lineHeight)
		r.currentY += lineHeight
	}
	r.resetText()
}

func (r *PdfRenderer) drawLine(line textLine, x0, y0, lineHeight float64) {
	for _, f := range line.frags {
		x, y := x0+f.x, y0+f.yShift
		if f.math != nil {
			// On the baseline of the text around it
			if f.math.draw != nil {
				f.math.draw(x, y0+r.family(f.font).baseline*f.size)
			}
			r.strokeColor(theme.Black)
			r.lineWidth(0.5)
			continue
		}
		r.setFont(f.font, f.style, f.size)
		r.textColor(f.color)

		if f.chip {
			r.fillColor(hexStringToRGB(strings.TrimPrefix(f.run.ColorChip, "#")))
			r.rect(x-chipWidth, y0, 10, 10, "F")
			r.fillColor(theme.White)
		}
		if f.run.Highlight {
			r.fillColor(r.t.Colors.Highlight)
			r.rect(x, y0-1, f.width, f.size+3, "F")
			r.fillColor(theme.White)
		}

		r.pdf.SetX(x)
		r.pdf.SetY(y)
		r.cell(f.text)

		if f.run.Link != "" {
			r.linkTo(f.run.Link, x, y0-2, f.width, lineHeight)
		}

		underline := f.run.Underline || f.run.Inserted
		strike := f.run.Strikethrough || f.run.Deleted
		if underline || strike {
			r.strokeColor(f.color)
			r.lineWidth(0.6)
			if underline {
				ly := y + f.size + 1
				r.line(x, ly, x+f.width, ly)
			}
			if strike {
				ly := y + f.size*0.58
				r.line(x, ly, x+f.width, ly)
			}
			r.strokeColor(theme.Black)
			r.lineWidth(0.5)
		}
	}
}

// resetText restores the font and color the rest of the renderer starts from.
func (r *PdfRenderer) resetText() {
	r.textColor(theme.Black)
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
}

// renderRuns lays out and draws runs as a block of text starting at x that
// may use width, in the given base style.
func (r *PdfRenderer) renderRuns(runs []ast.InlineRun, base textStyle, x, width float64) {
	lines := r.layout(runs, base, width, width)
	if len(lines) == 0 {
		if len(runs) > 0 {
			r.currentY += base.lineHeight // text that cannot be drawn still takes its line
		}
		r.resetText()
		return
	}
	r.drawLines(lines, x, x, base.lineHeight)
}
