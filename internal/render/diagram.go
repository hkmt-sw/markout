package render

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hkmt-sw/markout/internal/mermaid"
	"github.com/hkmt-sw/markout/internal/theme"
)

// Mermaid flowcharts are drawn; see internal/mermaid for how one is read
// and laid out. Diagrams of other kinds, and flowcharts that cannot be
// read, are shown as their source in a panel, and the conversion says so.

// chartFontScale is the size of the text of a chart, relative to the text
// of the document.
const chartFontScale = 0.9

// minChartScale is how far a chart is shrunk to fit the page before it is
// given up on: below this its text cannot be read.
const minChartScale = 0.35

// chartStyle is the look of a chart in a theme: its boxes in the colors of
// the diagram panel, its frames in those of the metadata box.
func chartStyle(t theme.Theme, font string) mermaid.Style {
	hex := func(c theme.Color) string { return "#" + c.Hex() }
	return mermaid.Style{
		Font: font, FontSize: t.Text.Size * chartFontScale,
		NodeFill: hex(t.Diagram.Background), NodeStroke: hex(t.Diagram.Border), Text: hex(t.Diagram.Text),
		Line: hex(t.Text.Muted), FrameFill: hex(t.Box.Background), FrameStroke: hex(t.Box.Border), Page: "#FFFFFF",
	}
}

// diagramProblems collects the diagrams that were not drawn.
type diagramProblems struct {
	kinds     []string // kinds of diagram that are not flowcharts
	kindCount int
	seen      map[string]bool
	unread    int // flowcharts that could not be read
	first     string
}

// note records why a diagram is shown as its source.
func (p *diagramProblems) note(source string, err error) {
	if errors.Is(err, mermaid.ErrNotFlowchart) {
		kind := mermaid.Kind(source)
		if kind == "" {
			kind = "(empty)"
		}
		if p.seen == nil {
			p.seen = map[string]bool{}
		}
		if !p.seen[kind] {
			p.seen[kind] = true
			p.kinds = append(p.kinds, kind)
		}
		p.kindCount++
		return
	}
	if p.unread == 0 {
		p.first = err.Error()
	}
	p.unread++
}

func (p *diagramProblems) warnings() []string {
	var out []string
	if p.kindCount > 0 {
		what := "diagrams are shown as their source"
		if p.kindCount == 1 {
			what = "diagram is shown as its source"
		}
		out = append(out, fmt.Sprintf("%d %s: markout draws flowcharts, not %s", p.kindCount, what, strings.Join(p.kinds, ", ")))
	}
	switch {
	case p.unread == 1:
		out = append(out, "1 flowchart could not be drawn and is shown as its source: "+p.first)
	case p.unread > 1:
		out = append(out, fmt.Sprintf("%d flowcharts could not be drawn and are shown as their source; the first: %s", p.unread, p.first))
	}
	return out
}

// errChartTooLarge is why a chart that would have to be shrunk past reading
// is not drawn.
var errChartTooLarge = errors.New("the chart is too large for the page")

// fitChart lays a chart out to go into a space of the given width and
// height, and returns the drawing with the scale it is to be drawn at. A
// chart that flows sideways and is too wide for that is laid out from the
// top down instead: a long chain of boxes reads as well downwards, and far
// better than not at all.
func fitChart(graph *mermaid.Graph, m mermaid.Metrics, width, height float64) (*mermaid.Drawing, float64, error) {
	lay := func() (*mermaid.Drawing, float64) {
		d := graph.Layout(m)
		scale := 1.0
		if d.W > width {
			scale = width / d.W
		}
		if d.H*scale > height {
			scale = height / d.H
		}
		return d, scale
	}
	drawing, scale := lay()
	if scale < minChartScale && (graph.Dir == mermaid.LeftRight || graph.Dir == mermaid.RightLeft) {
		graph.Dir = mermaid.TopDown
		drawing, scale = lay()
	}
	if drawing.W <= 0 || drawing.H <= 0 || scale < minChartScale {
		return nil, 0, errChartTooLarge
	}
	return drawing, scale, nil
}
