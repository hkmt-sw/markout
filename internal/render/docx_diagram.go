package render

import (
	"sync"

	"github.com/hkmt-sw/markout/internal/mermaid"
)

// chartFont is the font charts in a DOCX are drawn in, and measured with:
// the picture is made here, not by the word processor, from an SVG that
// resvg draws with the embedded Liberation Sans.
const chartFont = "Liberation Sans"

var chartMetrics = sync.OnceValue(func() *glyphBoxes {
	boxes, _ := newGlyphBoxes(fontSansRegular)
	return boxes
})

// embedChart draws a flowchart and puts it into the document as a picture.
func (r *DocxRenderer) embedChart(source string) error {
	graph, err := mermaid.Parse(source)
	if err != nil {
		return err
	}
	style := chartStyle(r.t, chartFont)
	metrics := chartMetrics()
	if metrics == nil {
		return errChartTooLarge
	}
	page := r.t.Page
	drawing, scale, err := fitChart(graph, mermaid.Metrics{
		Width:      func(line string) float64 { return metrics.width(line, style.FontSize) },
		LineHeight: style.FontSize * 1.3,
	}, page.ContentWidth()-r.inset, page.Height-page.MarginTop-page.MarginBottom-2*r.t.Text.LineHeight)
	if err != nil {
		return err
	}
	// The drawing is in points; a picture's size is given in pixels, 96 to
	// the inch
	width := int(drawing.W*scale*96/72 + 0.5)
	png, err := rasterizeSVG(drawing.SVG(style), width)
	if err != nil {
		return err
	}
	return r.embedPicture(png, width, "")
}
