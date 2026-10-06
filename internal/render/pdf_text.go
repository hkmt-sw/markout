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
	chip     bool // a color swatch is drawn before the text
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
	var lines []textLine
	var line textLine
	x := 0.0
	needSpace := false // the previous run ended in whitespace
	width := firstWidth

	// The gap between two runs is a space of the surrounding text, not of the
	// run that follows: a monospace space before inline code is too wide.
	baseStyle := ""
	if base.bold {
		baseStyle = "B"
	}
	r.setFont(base.font, baseStyle, base.size)
	baseSpace, _ := r.pdf.MeasureTextWidth(" ")

	breakLine := func() {
		lines = append(lines, line)
		line = textLine{}
		x = 0
		width = restWidth
	}

	for _, run := range runs {
		text := run.Text
		if run.FootnoteIndex > 0 {
			text = "[" + itoa(run.FootnoteIndex) + "]"
		} else {
			// Strip emojis that can't be rendered by the embedded fonts
			text = stripEmojis(text)
		}
		if text == "" {
			continue
		}

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
		switch {
		case run.Math:
			f.color = r.t.Colors.Math
		case run.Link != "":
			f.color = r.t.Link.Color
		case run.Inserted:
			f.color = r.t.Colors.Inserted
		case run.Deleted:
			f.color = r.t.Colors.Deleted
		case run.Code:
			f.color = r.t.Code.Color
		case run.FootnoteIndex > 0:
			f.color = r.t.Link.Color
		case run.Strikethrough:
			f.color = r.t.Text.Faint
		}

		r.setFont(f.font, f.style, f.size)
		spaceWidth, _ := r.pdf.MeasureTextWidth(" ")
		chip := run.ColorChip != ""

		// A space is only laid out where the source has whitespace, so adjacent
		// runs ("x" + superscript "2", bold text followed by a comma) stay
		// together. Words of one run on one line are merged into one fragment.
		leadingSpace := strings.TrimLeft(text, " \t\n") != text
		open := false // a fragment of this run is open at the end of the line
		for i, word := range strings.Fields(text) {
			wordWidth, _ := r.pdf.MeasureTextWidth(word)
			gap := (i > 0 || leadingSpace || needSpace) && x > 0
			gapWidth := spaceWidth
			if i == 0 {
				gapWidth = baseSpace
			}
			need := wordWidth
			if gap {
				need += gapWidth
			}
			if chip {
				need += chipWidth
			}

			if x+need > width && x > 0 {
				breakLine()
				gap, open = false, false
			}

			if open && gap && !chip {
				last := &line.frags[len(line.frags)-1]
				last.text += " " + word
				last.width += spaceWidth + wordWidth
				x += spaceWidth + wordWidth
				continue
			}

			if gap {
				x += gapWidth
			}
			part := f
			part.text, part.x, part.width, part.chip = word, x, wordWidth, chip
			if chip {
				part.x += chipWidth
				x += chipWidth
				chip = false
			}
			line.frags = append(line.frags, part)
			x += wordWidth
			open = true
		}
		needSpace = strings.TrimRight(text, " \t\n") != text
	}

	if len(line.frags) > 0 {
		lines = append(lines, line)
	}
	return lines
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
			r.pdf.AddExternalLink(f.run.Link, x, y0-2, f.width, lineHeight)
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
