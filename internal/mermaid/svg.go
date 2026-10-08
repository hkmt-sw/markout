package mermaid

import (
	"fmt"
	"html"
	"strings"
)

// Style is how a drawing looks: colors as "#RRGGBB", and the font the
// layout measured its text in.
type Style struct {
	Font        string
	FontSize    float64
	NodeFill    string
	NodeStroke  string
	Text        string
	Line        string
	FrameFill   string
	FrameStroke string
	Page        string // behind the text of connections
}

// Stroke widths of connections.
const (
	LineWidth  = 1.1
	ThickWidth = 2.4
)

// SVG writes the drawing as an SVG picture.
func (d *Drawing) SVG(s Style) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.1f" height="%.1f" viewBox="0 0 %.1f %.1f" font-family="%s" font-size="%.1f">`,
		d.W, d.H, d.W, d.H, html.EscapeString(s.Font), s.FontSize)

	text := func(t Text, fill string) {
		// Text is placed by its baseline, a little under its middle
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" fill="%s">%s</text>`,
			t.At.X, t.At.Y+0.35*s.FontSize, fill, html.EscapeString(t.Line))
	}
	points := func(pts []Point) string {
		parts := make([]string, len(pts))
		for i, p := range pts {
			parts[i] = fmt.Sprintf("%.1f,%.1f", p.X, p.Y)
		}
		return strings.Join(parts, " ")
	}

	for _, f := range d.Frames {
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="4" fill="%s" stroke="%s" stroke-width="0.8"/>`,
			f.X, f.Y, f.W, f.H, s.FrameFill, s.FrameStroke)
		if f.Title != "" {
			text(Text{At: f.TitleAt, Line: f.Title}, s.Text)
		}
	}

	for _, link := range d.Links {
		if link.Line == Invisible {
			continue
		}
		width, dash := LineWidth, ""
		switch link.Line {
		case Thick:
			width = ThickWidth
		case Dotted:
			dash = ` stroke-dasharray="2.5 3"`
		}
		var path strings.Builder
		for i, seg := range link.Curve {
			if i == 0 {
				fmt.Fprintf(&path, "M%.1f %.1f", seg[0].X, seg[0].Y)
			}
			fmt.Fprintf(&path, " C%.1f %.1f %.1f %.1f %.1f %.1f", seg[1].X, seg[1].Y, seg[2].X, seg[2].Y, seg[3].X, seg[3].Y)
		}
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="%.1f"%s/>`, path.String(), s.Line, width, dash)

		tip := func(kind Tip, at, dir Point) {
			switch kind {
			case Arrow:
				fmt.Fprintf(&b, `<polygon points="%s" fill="%s"/>`, points(ArrowHead(at, dir)), s.Line)
			case Dot:
				fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s" stroke="%s"/>`, at.X-dir.X*TipSize, at.Y-dir.Y*TipSize, TipSize, s.Page, s.Line)
			case Cross:
				c := Point{at.X - dir.X*TipSize, at.Y - dir.Y*TipSize}
				fmt.Fprintf(&b, `<path d="M%.1f %.1f L%.1f %.1f M%.1f %.1f L%.1f %.1f" stroke="%s" stroke-width="1.2"/>`,
					c.X-TipSize, c.Y-TipSize, c.X+TipSize, c.Y+TipSize, c.X-TipSize, c.Y+TipSize, c.X+TipSize, c.Y-TipSize, s.Line)
			}
		}
		tip(link.End, link.TipAt(true), link.EndDir)
		tip(link.Start, link.TipAt(false), link.StartDir)
	}

	for _, box := range d.Boxes {
		fmt.Fprintf(&b, `<polygon points="%s" fill="%s" stroke="%s" stroke-width="1"/>`, points(box.Outline), s.NodeFill, s.NodeStroke)
		for _, mark := range box.Marks {
			fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1"/>`, points(mark), s.NodeStroke)
		}
		for _, t := range box.Text {
			text(t, s.Text)
		}
	}

	// The text of connections goes over everything it might cross
	for _, link := range d.Links {
		if len(link.Label) == 0 || link.Line == Invisible {
			continue
		}
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
			link.LabelBox[0], link.LabelBox[1], link.LabelBox[2], link.LabelBox[3], s.Page)
		for _, t := range link.Label {
			text(t, s.Text)
		}
	}
	b.WriteString(`</svg>`)
	return []byte(b.String())
}
