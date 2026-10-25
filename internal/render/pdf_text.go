package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/hyphen"
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
	justify    bool               // lines are set flush on both sides
	hyphen     *hyphen.Hyphenator // breaks words at the ends of lines; nil leaves them whole
}

// proseStyle is the style of running text (paragraphs, list items, quotes,
// callouts): bodyStyle, flush on both sides and hyphenated where the theme
// says so.
func (r *PdfRenderer) proseStyle() textStyle {
	style := r.bodyStyle()
	style.justify = r.t.Text.Justified()
	if r.t.Text.Hyphenated() {
		style.hyphen = r.hyphenator()
	}
	return style
}

// asideStyle is the style of body text in narrow or short blocks (table
// cells, definitions, footnotes). It is never justified, and hyphenated only
// by a theme that hyphenates everything.
func (r *PdfRenderer) asideStyle() textStyle {
	style := r.bodyStyle()
	if r.t.Text.Hyphenate == theme.HyphenateAlways {
		style.hyphen = r.hyphenator()
	}
	return style
}

// hyphenator is what hyphenates the language of the document. Where there is
// nothing to do it with, the text stays whole and the user is told.
func (r *PdfRenderer) hyphenator() *hyphen.Hyphenator {
	h := hyphen.For(r.opts.Lang)
	if h == nil {
		r.unhyphenated = true
	}
	return h
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
	spaced   bool     // a word space comes before it
	joined   bool     // it goes on from the fragment before it, in the same run
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

	b := lineBreaker{width: firstWidth, restWidth: restWidth, narrowest: math.Min(firstWidth, restWidth), baseSpace: baseSpace, justify: base.justify}
	b.hyphen, b.soft = base.hyphen, r.t.Text.Hyphenate != theme.HyphenateNever
	for _, run := range runs {
		text := run.Text
		if run.FootnoteIndex > 0 {
			text = "[" + strconv.Itoa(run.FootnoteIndex) + "]"
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
	lines := b.finish()
	if base.justify {
		justify(lines, firstWidth, restWidth, baseSpace)
	}
	return lines
}

// maxStretch is how many times its width a word space may grow to in a
// justified line. A line that would need more is left ragged: a few words
// far apart are harder to read than a short line.
const maxStretch = 3.0

// justify sets lines flush on both sides by widening their word spaces. The
// last line stays as it is. firstWidth and restWidth are what the first line
// and the others may take up.
func justify(lines []textLine, firstWidth, restWidth, space float64) {
	for i := range lines {
		width := restWidth
		if i == 0 {
			width = firstWidth
		}
		extra := 0.0
		if i < len(lines)-1 {
			extra = stretchOf(lines[i], width, space)
		}
		spread(lines[i].frags, extra)
	}
}

// stretchOf is how much each word space of a line grows by to make the line
// as wide as width, or 0 where the line is left ragged.
func stretchOf(line textLine, width, space float64) float64 {
	spaces := 0
	for _, f := range line.frags {
		if f.spaced {
			spaces++
		}
	}
	room := width - line.width()
	if spaces == 0 || room <= 0 {
		return 0
	}
	extra := room / float64(spaces)
	if space+extra > maxStretch*space {
		return 0
	}
	return extra
}

// spread moves the fragments of a line apart by extra at every word space. A
// fragment that goes on in the next one is widened up to it, so that what is
// drawn along a run (an underline, a highlight, the area of a link) has no
// holes at the spaces.
func spread(frags []fragment, extra float64) {
	shift := 0.0
	for i := range frags {
		if frags[i].spaced {
			shift += extra
		}
		frags[i].x += shift
		if i > 0 && frags[i].joined {
			frags[i-1].width = frags[i].x - frags[i-1].x
		}
	}
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
	justify   bool    // every word is a fragment of its own, to be moved apart

	hyphen *hyphen.Hyphenator // breaks words that do not fit; nil leaves them whole
	soft   bool               // a word is broken at the soft hyphens written into it
	broken bool               // the line being filled ends in a part of a word
	run    int                // how many lines before this one ended so, in a row
}

func (b *lineBreaker) breakLine() {
	b.run++
	if !b.broken {
		b.run = 0
	}
	b.broken = false
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
	f.x, f.width, f.math, f.spaced = b.x, box.w, &box, gap
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
	// Soft hyphens are places to break a word at, not characters to draw
	text, soft := softHyphens(text)
	// Characters the font cannot draw are left out
	if clean := r.usable(text); clean != text {
		text, soft = clean, nil // the places no longer hold
	}
	if text == "" {
		return
	}
	spaceWidth, _ := r.pdf.MeasureTextWidth(" ")
	p := runPlacer{b: b, f: f, spaceWidth: spaceWidth, chip: f.run.ColorChip != ""}

	leadingSpace := strings.TrimLeft(text, " \t\n") != text
	for i, word := range words(text) {
		// A no-break space is drawn as an ordinary space inside the word
		word = noBreakAsSpace(word)
		// The gap before a run is a space of the text around it
		gapWidth := spaceWidth
		if i == 0 {
			gapWidth = b.baseSpace
		}
		gap := (i > 0 || leadingSpace || b.needSpace) && b.x > 0
		parts := r.wordParts(word, b.narrowest)
		if len(parts) == 1 {
			r.placeWord(&p, word, soft[i], gap, gapWidth)
			continue
		}
		for pi, part := range parts {
			partWidth, _ := r.pdf.MeasureTextWidth(part)
			p.place(part, partWidth, gap && pi == 0, gapWidth)
		}
	}
	b.needSpace = strings.TrimRight(text, " \t\n") != text
}

// placeWord puts a word into the line. One that does not fit at the end of
// the line is broken there if it may be, and goes to the next line whole if
// not.
func (r *PdfRenderer) placeWord(p *runPlacer, word string, soft []int, gap bool, gapWidth float64) {
	b := p.b
	width, _ := r.pdf.MeasureTextWidth(word)
	if room := p.room(gap, gapWidth); width > room && b.x > 0 && b.run < maxBrokenLines {
		if head, tail, ok := r.fitting(word, b.breaksOf(word, soft), room); ok {
			headWidth, _ := r.pdf.MeasureTextWidth(head)
			p.place(head, headWidth, gap, gapWidth)
			b.broken = true
			b.breakLine()
			p.open, gap = false, false
			word = tail
			width, _ = r.pdf.MeasureTextWidth(word)
		}
	}
	p.place(word, width, gap, gapWidth)
}

// fitting breaks a word at the last of the places where what stays on the
// line takes no more than room.
func (r *PdfRenderer) fitting(word string, breaks []wordBreak, room float64) (head, tail string, ok bool) {
	letters := []rune(word)
	for i := len(breaks) - 1; i >= 0; i-- {
		head = string(letters[:breaks[i].at])
		if breaks[i].hyphen {
			head += "-"
		}
		if w, _ := r.pdf.MeasureTextWidth(head); w <= room {
			return head, string(letters[breaks[i].at:]), true
		}
	}
	return "", "", false
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

// room is what is left of the line for a word.
func (p *runPlacer) room(gap bool, gapWidth float64) float64 {
	room := p.b.width - p.b.x
	if gap {
		room -= gapWidth
	}
	if p.chip {
		room -= chipWidth
	}
	return room
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

	joined := p.open && gap && !p.chip
	if joined && !b.justify {
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
	piece.spaced, piece.joined = gap, joined
	if p.chip {
		piece.x += chipWidth
		b.x += chipWidth
		p.chip = false
	}
	b.add(piece)
	p.open = true
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
