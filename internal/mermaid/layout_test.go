package mermaid

import (
	"encoding/xml"
	"math"
	"strings"
	"testing"
)

// testMetrics measure text as six units a character, twelve a line.
var testMetrics = Metrics{Width: func(s string) float64 { return 6 * float64(len([]rune(s))) }, LineHeight: 12}

func layoutOf(t *testing.T, src string) (*Graph, *Drawing) {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return g, g.Layout(testMetrics)
}

func nodeNamed(t *testing.T, g *Graph, id string) *Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

const sampleChart = `graph TD
A[Start] --> B{Is it valid?}
B -->|Yes| C[Process the data]
B -->|No| D[Show an error]
C --> E[(Database)]
D --> F([End])
E --> F
C -.-> G((Log))
F ==> A
subgraph Storage
E
G
end
X --> X
`

// Connections point down the chart, nodes do not overlap, and everything is
// inside the drawing.
func TestLayout(t *testing.T) {
	g, d := layoutOf(t, sampleChart)

	for _, pair := range [][2]string{{"A", "B"}, {"B", "C"}, {"B", "D"}, {"C", "E"}, {"D", "F"}, {"E", "F"}} {
		from, to := nodeNamed(t, g, pair[0]), nodeNamed(t, g, pair[1])
		if from.Y+from.H/2 >= to.Y-to.H/2 {
			t.Errorf("%s (y=%.0f) should be above %s (y=%.0f)", pair[0], from.Y, pair[1], to.Y)
		}
	}
	for i, a := range g.Nodes {
		for _, b := range g.Nodes[i+1:] {
			if math.Abs(a.X-b.X) < (a.W+b.W)/2 && math.Abs(a.Y-b.Y) < (a.H+b.H)/2 {
				t.Errorf("%s and %s overlap", a.ID, b.ID)
			}
		}
		if a.X-a.W/2 < 0 || a.Y-a.H/2 < 0 || a.X+a.W/2 > d.W || a.Y+a.H/2 > d.H {
			t.Errorf("%s is outside the drawing (%.0f x %.0f)", a.ID, d.W, d.H)
		}
	}
	if len(d.Boxes) != len(g.Nodes) || len(d.Links) != len(g.Edges) {
		t.Errorf("%d boxes for %d nodes, %d links for %d connections", len(d.Boxes), len(g.Nodes), len(d.Links), len(g.Edges))
	}
	for _, link := range d.Links {
		for _, seg := range link.Curve {
			for _, p := range seg {
				if p.X < -0.5 || p.Y < -0.5 || p.X > d.W+0.5 || p.Y > d.H+0.5 || math.IsNaN(p.X) || math.IsNaN(p.Y) {
					t.Errorf("a connection goes to %.1f,%.1f, outside the drawing", p.X, p.Y)
				}
			}
		}
	}

	// The frame of the subgraph is around its nodes, with its title
	if len(d.Frames) != 1 || d.Frames[0].Title != "Storage" {
		t.Fatalf("frames: %+v", d.Frames)
	}
	frame := d.Frames[0]
	for _, id := range []string{"E", "G"} {
		n := nodeNamed(t, g, id)
		if n.X-n.W/2 < frame.X || n.X+n.W/2 > frame.X+frame.W || n.Y-n.H/2 < frame.Y || n.Y+n.H/2 > frame.Y+frame.H {
			t.Errorf("%s is not inside the frame", id)
		}
	}
}

// The text of a connection is between the nodes it connects, and a wider
// label makes a wider node.
func TestLayoutText(t *testing.T) {
	g, d := layoutOf(t, "graph TD\nA -->|a label| B[\"A much longer label than the other\"]\n")
	a, b := nodeNamed(t, g, "A"), nodeNamed(t, g, "B")
	if b.W <= a.W {
		t.Errorf("widths %.0f and %.0f", a.W, b.W)
	}
	label := d.Links[0].Label
	if len(label) != 1 || label[0].Line != "a label" || label[0].At.Y <= a.Y || label[0].At.Y >= b.Y {
		t.Errorf("label %+v, between y=%.0f and y=%.0f", label, a.Y, b.Y)
	}
	if lines := d.Boxes[1].Text; len(lines) != 1 || math.Abs(lines[0].At.X-b.X) > 0.01 {
		t.Errorf("the text of B is at %+v, B at x=%.1f", lines, b.X)
	}
}

// The other directions turn the same layout.
func TestLayoutDirections(t *testing.T) {
	const chain = "\nA --> B --> C\n"
	check := func(dir string, before func(a, b *Node) bool) {
		t.Helper()
		g, _ := layoutOf(t, "graph "+dir+chain)
		a, b, c := nodeNamed(t, g, "A"), nodeNamed(t, g, "B"), nodeNamed(t, g, "C")
		if !before(a, b) || !before(b, c) {
			t.Errorf("%s: A at %.0f,%.0f B at %.0f,%.0f C at %.0f,%.0f", dir, a.X, a.Y, b.X, b.Y, c.X, c.Y)
		}
	}
	check("TD", func(a, b *Node) bool { return a.Y < b.Y && math.Abs(a.X-b.X) < 1 })
	check("BT", func(a, b *Node) bool { return a.Y > b.Y && math.Abs(a.X-b.X) < 1 })
	check("LR", func(a, b *Node) bool { return a.X < b.X && math.Abs(a.Y-b.Y) < 1 })
	check("RL", func(a, b *Node) bool { return a.X > b.X && math.Abs(a.Y-b.Y) < 1 })
}

// A connection that leads back is drawn as written: its arrow is at the
// node it points to, which is above.
func TestLayoutCycle(t *testing.T) {
	g, d := layoutOf(t, "graph TD\nA --> B\nB --> C\nC --> A\n")
	a, c := nodeNamed(t, g, "A"), nodeNamed(t, g, "C")
	back := d.Links[2]
	start, end := back.Curve[0][0], back.TipAt(true)
	near := func(p Point, n *Node) bool {
		return math.Abs(p.X-n.X) <= n.W/2+1 && math.Abs(p.Y-n.Y) <= n.H/2+1
	}
	if !near(start, c) || !near(end, a) {
		t.Errorf("the connection back runs from %.0f,%.0f to %.0f,%.0f; C is at %.0f,%.0f and A at %.0f,%.0f",
			start.X, start.Y, end.X, end.Y, c.X, c.Y, a.X, a.Y)
	}
}

// The same chart is laid out the same way every time, and twice in a row.
func TestLayoutIsStable(t *testing.T) {
	style := Style{Font: "x", FontSize: 10, NodeFill: "#fff", NodeStroke: "#000", Text: "#000", Line: "#000", FrameFill: "#eee", FrameStroke: "#999", Page: "#fff"}
	g, first := layoutOf(t, sampleChart)
	want := string(first.SVG(style))
	if got := string(g.Layout(testMetrics).SVG(style)); got != want {
		t.Error("laying the same graph out again gives another drawing")
	}
	for i := 0; i < 5; i++ {
		_, again := layoutOf(t, sampleChart)
		if string(again.SVG(style)) != want {
			t.Fatal("the same chart gives another drawing")
		}
	}
}

func TestSVG(t *testing.T) {
	_, d := layoutOf(t, "graph LR\nA[\"a < b & c\"] -->|\"x > y\"| B((Round))\nB -.-> C\nC ==> D\nD --o E\nE --x F\nF ~~~ G\n")
	svg := string(d.SVG(Style{Font: "Some Font", FontSize: 10, NodeFill: "#fff", NodeStroke: "#000", Text: "#111", Line: "#222", FrameFill: "#eee", FrameStroke: "#999", Page: "#fff"}))
	if err := xml.Unmarshal([]byte(svg), new(struct{})); err != nil {
		t.Fatalf("not well-formed: %v\n%s", err, svg)
	}
	for _, want := range []string{"a &lt; b &amp; c", "x &gt; y", "stroke-dasharray", `stroke-width="2.4"`, "<circle", `font-family="Some Font"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("the SVG has no %s", want)
		}
	}
	// Six connections are drawn; the invisible one is not
	if n := strings.Count(svg, `<path d="M`) - 1; n != 5 {
		t.Errorf("%d connection lines, want 5 (the cross is a path too)", n)
	}
}

// Charts of every shape and size are laid out without a hitch.
func TestLayoutOddCharts(t *testing.T) {
	var big strings.Builder
	big.WriteString("graph TD\n")
	for i := 0; i < 120; i++ {
		big.WriteString("n" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + " --> m" + string(rune('a'+(i*7)%26)) + "\n")
	}
	for name, src := range map[string]string{
		"one node": "graph TD\nA\n", "self only": "graph TD\nA --> A\n", "two cycles": "graph TD\nA --> B --> A\nC --> D --> C\n",
		"long span": "graph TD\nA ------> B\nA --> C --> D --> B\n", "empty subgraph": "graph TD\nsubgraph S\nend\nA\n",
		"nested":      "graph LR\nsubgraph a\nsubgraph b\nsubgraph c\nX --> Y\nend\nend\nend\nY --> Z\n",
		"unconnected": "graph TD\nA\nB\nC\nD --> E\n", "many": big.String(),
		"empty text": "graph TD\nA[\"\"] --> B[ ]\n",
	} {
		g, err := Parse(src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		d := g.Layout(testMetrics)
		if d.W <= 0 || d.H <= 0 || math.IsNaN(d.W) || math.IsInf(d.H, 0) || len(d.Boxes) != len(g.Nodes) {
			t.Errorf("%s: drawing %.0f x %.0f with %d boxes for %d nodes", name, d.W, d.H, len(d.Boxes), len(g.Nodes))
		}
	}
}
