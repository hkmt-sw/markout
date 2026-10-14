package render

import (
	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// The cover page holds the document's title and subtitle, and under them the
// rest of its control block. The text starts on the page after it.

const (
	// coverSubtitleGap is the space between the title and the subtitle.
	coverSubtitleGap = 8.0
	// coverBlockGap is the space between them and the control block.
	coverBlockGap = 40.0
)

// renderCover draws the front matter as the cover page. The title and the
// subtitle are on the page in large type, so the control block leaves them
// out. The cover is not a heading: it is not in the outline.
func (r *PdfRenderer) renderCover(fm ast.FrontMatter) {
	c := r.t.Cover
	r.currentY = r.marginTop + c.Top

	// The lines of the cover keep the proportions of a first-level heading
	ratio := 1.2
	if h1 := r.t.Heading[0]; h1.Size > 0 {
		ratio = h1.LineHeight / h1.Size
	}
	drawn := false
	put := func(text string, size float64, color theme.Color, bold bool) {
		if text == "" {
			return
		}
		if drawn {
			r.currentY += coverSubtitleGap
		}
		style := textStyle{font: r.headingFont(), size: size, bold: bold, color: color, lineHeight: size * ratio}
		lines := r.layout([]ast.InlineRun{{Text: text}}, style, r.contentWidth, r.contentWidth)
		r.drawLines(lines, r.marginLeft, r.marginLeft, style.lineHeight)
		drawn = true
	}
	put(r.info.title, c.TitleSize, c.TitleColor, true)
	put(frontValue(fm, "subtitle"), c.SubtitleSize, c.SubtitleColor, false)

	// What the page says already is not a row of the block
	omit := []string{"subtitle"}
	if fm.Title != "" {
		omit = append(omit, "title")
	}
	fields := controlFields(fm, r.opts.Lang, r.t.Control.Labels, omit...)
	if len(fields) > 0 {
		if drawn {
			r.currentY += coverBlockGap
		}
		r.renderControlBlock(fields)
	}
}
