package render

import (
	"math"

	"github.com/signintech/gopdf"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/mermaid"
	"github.com/hkmt-sw/markout/internal/theme"
)

func (r *PdfRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) {
	if err := r.drawChart(diagram.Source); err != nil {
		r.diagramProblems.note(diagram.Source, err)
		r.renderDiagramSource(diagram)
	}
}

// drawChart draws a flowchart as lines, shapes and text, centered, made
// smaller if it is wider than the text or taller than a page.
func (r *PdfRenderer) drawChart(source string) error {
	graph, err := mermaid.Parse(source)
	if err != nil {
		return err
	}
	size := r.t.Text.Size * chartFontScale
	r.setFont(r.bodyFont(), "", size)
	drawing, scale, err := fitChart(graph, mermaid.Metrics{
		Width: func(line string) float64 {
			w, _ := r.pdf.MeasureTextWidth(r.usable(line))
			return w
		},
		LineHeight: size * 1.3,
	}, r.contentWidth, r.pageHeight-r.marginTop-r.marginBottom)
	if err != nil {
		return err
	}

	r.checkPageBreak(drawing.H * scale)
	left := r.marginLeft + (r.contentWidth-drawing.W*scale)/2
	top := r.currentY
	at := func(p mermaid.Point) (float64, float64) { return left + p.X*scale, top + p.Y*scale }
	points := func(pts []mermaid.Point) []gopdf.Point {
		out := make([]gopdf.Point, len(pts))
		for i, p := range pts {
			out[i].X, out[i].Y = at(p)
		}
		return out
	}
	style := r.t.Diagram
	lineColor, page := r.t.Text.Muted, theme.White
	text := func(t mermaid.Text, color theme.Color) {
		line := r.usable(t.Line)
		r.setFont(r.bodyFont(), "", size*scale)
		r.textColor(color)
		width, _ := r.pdf.MeasureTextWidth(line)
		x, y := at(t.At)
		// Text is placed by its top; its middle is a little above its baseline
		r.pdf.SetX(x - width/2)
		r.pdf.SetY(y + 0.35*size*scale - r.family(r.bodyFont()).baseline*size*scale)
		r.cell(line)
	}

	for _, f := range drawing.Frames {
		r.fillColor(r.t.Box.Background)
		r.strokeColor(r.t.Box.Border)
		r.lineWidth(0.8 * scale)
		x, y := at(mermaid.Point{X: f.X, Y: f.Y})
		r.rect(x, y, f.W*scale, f.H*scale, "FD")
		if f.Title != "" {
			text(mermaid.Text{At: f.TitleAt, Line: f.Title}, style.Text)
		}
	}

	for _, link := range drawing.Links {
		if link.Line == mermaid.Invisible {
			continue
		}
		r.strokeColor(lineColor)
		r.lineWidth(mermaid.LineWidth * scale)
		switch link.Line {
		case mermaid.Thick:
			r.lineWidth(mermaid.ThickWidth * scale)
		case mermaid.Dotted:
			r.pdf.SetCustomLineType([]float64{2.5 * scale, 3 * scale}, 0)
		}
		for _, seg := range link.Curve {
			x0, y0 := at(seg[0])
			x1, y1 := at(seg[1])
			x2, y2 := at(seg[2])
			x3, y3 := at(seg[3])
			r.curve(x0, y0, x1, y1, x2, y2, x3, y3)
		}
		r.pdf.SetLineType("solid")

		tip := func(kind mermaid.Tip, where, dir mermaid.Point) {
			r.lineWidth(mermaid.LineWidth * scale)
			switch kind {
			case mermaid.Arrow:
				r.fillColor(lineColor)
				r.polygon(points(mermaid.ArrowHead(where, dir)), "F")
			case mermaid.Dot, mermaid.Cross:
				c := mermaid.Point{X: where.X - dir.X*mermaid.TipSize, Y: where.Y - dir.Y*mermaid.TipSize}
				if kind == mermaid.Cross {
					s := mermaid.TipSize
					x0, y0 := at(mermaid.Point{X: c.X - s, Y: c.Y - s})
					x1, y1 := at(mermaid.Point{X: c.X + s, Y: c.Y + s})
					r.line(x0, y0, x1, y1)
					r.line(x0, y1, x1, y0)
					return
				}
				// Sixteen corners are round enough for a dot
				var ring []mermaid.Point
				for i := 0; i < 16; i++ {
					a := float64(i) * math.Pi / 8
					ring = append(ring, mermaid.Point{X: c.X + mermaid.TipSize*math.Cos(a), Y: c.Y + mermaid.TipSize*math.Sin(a)})
				}
				r.fillColor(page)
				r.polygon(points(ring), "FD")
			}
		}
		tip(link.End, link.TipAt(true), link.EndDir)
		tip(link.Start, link.TipAt(false), link.StartDir)
	}

	for _, box := range drawing.Boxes {
		r.fillColor(style.Background)
		r.strokeColor(style.Border)
		r.lineWidth(scale)
		r.polygon(points(box.Outline), "FD")
		for _, mark := range box.Marks {
			pts := points(mark)
			for i := 0; i+1 < len(pts); i++ {
				r.line(pts[i].X, pts[i].Y, pts[i+1].X, pts[i+1].Y)
			}
		}
		for _, t := range box.Text {
			text(t, style.Text)
		}
	}

	// The text of connections goes over everything it might cross
	for _, link := range drawing.Links {
		if len(link.Label) == 0 || link.Line == mermaid.Invisible {
			continue
		}
		r.fillColor(page)
		x, y := at(mermaid.Point{X: link.LabelBox[0], Y: link.LabelBox[1]})
		r.rect(x, y, link.LabelBox[2]*scale, link.LabelBox[3]*scale, "F")
		for _, t := range link.Label {
			text(t, style.Text)
		}
	}

	r.currentY = top + drawing.H*scale + r.t.Code.SpaceAfter
	r.resetText()
	r.strokeColor(theme.Black)
	r.lineWidth(0.5)
	return nil
}

// polygon draws a closed shape: filled ("F"), outlined ("D") or both.
func (r *PdfRenderer) polygon(points []gopdf.Point, style string) {
	if len(points) < 3 {
		return
	}
	left, top, right, bottom := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		left, top = min(left, p.X), min(top, p.Y)
		right, bottom = max(right, p.X), max(bottom, p.Y)
	}
	r.logf("shape x=%.1f y=%.1f w=%.1f h=%.1f points=%d %s fill=%s stroke=%s/%.1f",
		left, top, right-left, bottom-top, len(points), style, r.fill, r.stroke, r.strokeWidth)
	r.pdf.Polygon(points, style)
}
