package mermaid

import (
	"math"
	"sort"
)

// A flowchart is laid out in layers, the way such charts usually are (the
// method is Sugiyama's): every node gets a rank so that connections point
// from a lower rank to a higher one; the nodes of a rank are ordered to
// cross as few connections as they can; then each is moved sideways towards
// the nodes it is connected to. A connection that spans several ranks gets a
// stand-in node in each rank between, which is what bends it around the
// nodes in its way; its text sits on one of those.
//
// The layout is worked out for a chart that flows from the top down, and
// turned or mirrored at the end for the other directions.

// Metrics tell the layout how big text is.
type Metrics struct {
	Width      func(line string) float64 // of a line of text, at the chart's font size
	LineHeight float64
}

// Point is a place in a drawing. Y grows downwards.
type Point struct{ X, Y float64 }

// Drawing is a chart laid out: what to draw, and where.
type Drawing struct {
	W, H   float64
	Frames []Frame // subgraphs, outer ones first
	Boxes  []Box
	Links  []Link
}

// Frame is the frame of a subgraph.
type Frame struct {
	X, Y, W, H float64 // the top left corner and the size
	Title      string
	TitleAt    Point // the center of the title
}

// Box is a node: an outline with text in it.
type Box struct {
	Outline []Point   // a closed shape
	Marks   [][]Point // further lines of the shape, such as the rim of a cylinder
	Text    []Text
}

// Text is a line of text centered on a point.
type Text struct {
	At   Point
	Line string
}

// Link is a connection: a curve, with what it ends in and its text.
type Link struct {
	Curve      [][4]Point // Bézier segments, each from [0] to [3]
	Line       Line
	Start, End Tip
	StartDir   Point // unit vectors pointing out of the ends, for drawing the tips
	EndDir     Point
	Label      []Text
	LabelBox   [4]float64 // x, y, w, h of the patch behind the text
}

const (
	padX, padY = 12.0, 8.0 // room around the text of a node
	nodeGap    = 28.0      // between neighbors in a rank
	rankGap    = 46.0      // between ranks
	bendGap    = 14.0      // between connections running side by side
	framePad   = 12.0      // between a subgraph's frame and its nodes
	margin     = 4.0
	tipLength  = 7.0
)

// Layout places the nodes and connections of the graph.
func (g *Graph) Layout(m Metrics) *Drawing {
	flat := g.Dir == LeftRight || g.Dir == RightLeft
	l := &layout{g: g, m: m, flat: flat}
	l.sizeNodes()
	l.rank()
	l.addBends()
	l.order()
	l.place()
	return l.draw()
}

type layout struct {
	g    *Graph
	m    Metrics
	flat bool // the chart flows sideways: ranks are columns

	nodes []*Node // the graph's nodes, then the bends
	edges []*Edge // without connections of a node to itself
	loops []*Edge
	ranks [][]int // node indexes by rank, in order
}

// along and across are a node's sizes in the direction of the flow and
// across it.
func (l *layout) along(n *Node) float64 {
	if l.flat {
		return n.W
	}
	return n.H
}

func (l *layout) across(n *Node) float64 {
	if l.flat {
		return n.H
	}
	return n.W
}

func (l *layout) textSize(lines []string) (w, h float64) {
	for _, line := range lines {
		w = math.Max(w, l.m.Width(line))
	}
	return w, float64(len(lines)) * l.m.LineHeight
}

func (l *layout) sizeNodes() {
	l.nodes = append(l.nodes, l.g.Nodes...)
	for _, n := range l.nodes {
		n.rank, n.order = 0, 0 // a graph may be laid out more than once
		w, h := l.textSize(n.Label)
		n.W, n.H = w+2*padX, h+2*padY
		switch n.Shape {
		case Diamond:
			n.W, n.H = n.W*1.3+n.H*0.6, n.H*1.3+10
		case Circle:
			d := math.Max(n.W, n.H) + 6
			n.W, n.H = d, d
		case Stadium, Hexagon:
			n.W += n.H * 0.5
		case Parallelogram, Trapezoid, Flag:
			n.W += n.H * 0.6
		case Cylinder:
			n.H += 12
		case Subroutine:
			n.W += 12
		}
		n.W, n.H = math.Max(n.W, 36), math.Max(n.H, 26)
	}
	for _, e := range l.g.Edges {
		if e.From == e.To {
			l.loops = append(l.loops, e)
			continue
		}
		e.reversed, e.chain = false, nil
		l.edges = append(l.edges, e)
	}
}

// ends returns the nodes a connection runs between in the layout, where
// connections that close a cycle are turned around.
func ends(e *Edge) (from, to int) {
	if e.reversed {
		return e.To, e.From
	}
	return e.From, e.To
}

// rank gives every node a rank. Ranks come in pairs: nodes are on the even
// ones, and the odd ones between hold the text of connections.
func (l *layout) rank() {
	n := len(l.nodes)
	out := make([][]*Edge, n)
	for _, e := range l.edges {
		out[e.From] = append(out[e.From], e)
	}

	// A connection that leads back to a node on the way to it would make a
	// cycle; it is laid out the other way around and drawn as written.
	const (
		unseen = iota
		open
		done
	)
	state := make([]int, n)
	var visit func(v int)
	visit = func(v int) {
		state[v] = open
		for _, e := range out[v] {
			switch state[e.To] {
			case open:
				e.reversed = true
			case unseen:
				visit(e.To)
			}
		}
		state[v] = done
	}
	for v := range l.nodes {
		if state[v] == unseen {
			visit(v)
		}
	}

	// The longest path to a node is its rank
	in := make([]int, n)
	next := make([][]*Edge, n)
	for _, e := range l.edges {
		from, to := ends(e)
		next[from] = append(next[from], e)
		in[to]++
	}
	var queue []int
	for v := range l.nodes {
		if in[v] == 0 {
			queue = append(queue, v)
		}
	}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, e := range next[v] {
			_, to := ends(e)
			if r := l.nodes[v].rank + 2*e.Length; r > l.nodes[to].rank {
				l.nodes[to].rank = r
			}
			if in[to]--; in[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	// A node nothing leads to goes next to what it leads to, not to the top
	for v, node := range l.nodes {
		if len(next[v]) == 0 {
			continue
		}
		hasIn := false
		for _, e := range l.edges {
			if _, to := ends(e); to == v {
				hasIn = true
				break
			}
		}
		if hasIn {
			continue
		}
		low := math.MaxInt
		for _, e := range next[v] {
			_, to := ends(e)
			low = min(low, l.nodes[to].rank-2*e.Length)
		}
		node.rank = max(low, 0)
	}
}

// addBends gives a connection a stand-in node in every rank it passes, and
// puts its text on the one in the middle.
func (l *layout) addBends() {
	for _, e := range l.edges {
		from, to := ends(e)
		e.chain = []int{from}
		first, last := l.nodes[from].rank, l.nodes[to].rank
		labelAt := first + (last-first)/2
		if labelAt%2 == 0 {
			labelAt-- // text goes between the ranks of nodes
		}
		for r := first + 1; r < last; r++ {
			bend := &Node{Group: -1, dummy: true, rank: r}
			if r == labelAt && len(e.Label) > 0 {
				w, h := l.textSize(e.Label)
				bend.W, bend.H = w+8, h+4
				bend.labelW = w
			}
			l.nodes = append(l.nodes, bend)
			e.chain = append(e.chain, len(l.nodes)-1)
		}
		e.chain = append(e.chain, to)
	}

	top := 0
	for _, n := range l.nodes {
		top = max(top, n.rank)
	}
	l.ranks = make([][]int, top+1)
	for i, n := range l.nodes {
		l.ranks[n.rank] = append(l.ranks[n.rank], i)
	}
}

// topGroup is the outermost subgraph a node is in, or -1.
func (l *layout) topGroup(n *Node) int {
	group := n.Group
	for group >= 0 && l.g.Groups[group].Parent >= 0 {
		group = l.g.Groups[group].Parent
	}
	return group
}

// order arranges the nodes of every rank so that connections cross as
// little as they can: each node is moved to the average position of the
// nodes it is connected to in the rank before, down the chart and then up
// it, a number of times. Nodes of a subgraph are kept side by side.
func (l *layout) order() {
	up := make([][]int, len(l.nodes)) // neighbors in the rank above
	down := make([][]int, len(l.nodes))
	for _, e := range l.edges {
		for i := 0; i+1 < len(e.chain); i++ {
			a, b := e.chain[i], e.chain[i+1]
			down[a] = append(down[a], b)
			up[b] = append(up[b], a)
		}
	}
	number := func() {
		for _, rank := range l.ranks {
			for i, v := range rank {
				l.nodes[v].order = i
			}
		}
	}
	number()

	crossings := func() int {
		total := 0
		for _, rank := range l.ranks {
			var pairs [][2]int
			for _, v := range rank {
				for _, w := range down[v] {
					pairs = append(pairs, [2]int{l.nodes[v].order, l.nodes[w].order})
				}
			}
			if len(pairs) > 600 {
				continue // too many to count; the sweeps still help
			}
			for i := range pairs {
				for j := i + 1; j < len(pairs); j++ {
					if (pairs[i][0]-pairs[j][0])*(pairs[i][1]-pairs[j][1]) < 0 {
						total++
					}
				}
			}
		}
		return total
	}

	sweep := func(rank []int, toward [][]int) {
		want := make(map[int]float64, len(rank))
		for _, v := range rank {
			want[v] = float64(l.nodes[v].order)
			if len(toward[v]) > 0 {
				sum := 0.0
				for _, w := range toward[v] {
					sum += float64(l.nodes[w].order)
				}
				want[v] = sum / float64(len(toward[v]))
			}
		}
		// A subgraph moves as one, to the average of its nodes
		groupWant, groupSize := map[int]float64{}, map[int]float64{}
		for _, v := range rank {
			if group := l.topGroup(l.nodes[v]); group >= 0 {
				groupWant[group] += want[v]
				groupSize[group]++
			}
		}
		key := func(v int) (float64, int) {
			if group := l.topGroup(l.nodes[v]); group >= 0 {
				return groupWant[group] / groupSize[group], group
			}
			return want[v], -1 - v
		}
		sort.SliceStable(rank, func(i, j int) bool {
			ki, gi := key(rank[i])
			kj, gj := key(rank[j])
			if gi == gj {
				return want[rank[i]] < want[rank[j]]
			}
			if ki != kj {
				return ki < kj
			}
			return gi < gj
		})
		for i, v := range rank {
			l.nodes[v].order = i
		}
	}

	best, bestCount := l.copyRanks(), crossings()
	for round := 0; round < 12 && bestCount > 0; round++ {
		for r := 1; r < len(l.ranks); r++ {
			sweep(l.ranks[r], up)
		}
		for r := len(l.ranks) - 2; r >= 0; r-- {
			sweep(l.ranks[r], down)
		}
		if count := crossings(); count < bestCount {
			best, bestCount = l.copyRanks(), count
		}
	}
	l.ranks = best
	number()
}

func (l *layout) copyRanks() [][]int {
	out := make([][]int, len(l.ranks))
	for i, rank := range l.ranks {
		out[i] = append([]int(nil), rank...)
	}
	return out
}

// place gives the nodes their positions: across the flow by pulling each
// towards its neighbors while keeping the order and the gaps, along the flow
// by stacking the ranks.
func (l *layout) place() {
	gap := func(a, b *Node) float64 {
		space := nodeGap
		switch {
		case a.dummy && b.dummy:
			space = bendGap
		case a.dummy || b.dummy:
			space = bendGap + 4
		}
		if !a.dummy && !b.dummy && a.Group != b.Group {
			space += 2 * framePad // room for the frames between them
		}
		return (l.across(a)+l.across(b))/2 + space
	}

	// Start with every rank packed, and centered on the widest
	pos := make([]float64, len(l.nodes))
	widest := 0.0
	for _, rank := range l.ranks {
		for i, v := range rank {
			if i > 0 {
				pos[v] = pos[rank[i-1]] + gap(l.nodes[rank[i-1]], l.nodes[v])
			}
		}
		if len(rank) > 0 {
			widest = math.Max(widest, pos[rank[len(rank)-1]])
		}
	}
	for _, rank := range l.ranks {
		if len(rank) == 0 {
			continue
		}
		shift := (widest - pos[rank[len(rank)-1]]) / 2
		for _, v := range rank {
			pos[v] += shift
		}
	}

	up := make([][]int, len(l.nodes))
	down := make([][]int, len(l.nodes))
	for _, e := range l.edges {
		for i := 0; i+1 < len(e.chain); i++ {
			a, b := e.chain[i], e.chain[i+1]
			down[a] = append(down[a], b)
			up[b] = append(up[b], a)
		}
	}
	pull := func(rank []int, toward [][]int) {
		if len(rank) == 0 {
			return
		}
		want := make([]float64, len(rank))
		for i, v := range rank {
			want[i] = pos[v]
			sum, weight := 0.0, 0.0
			for _, w := range toward[v] {
				// The bends of a connection pull hardest, to keep it straight
				k := 1.0
				if l.nodes[v].dummy && l.nodes[w].dummy {
					k = 4
				} else if l.nodes[v].dummy || l.nodes[w].dummy {
					k = 2
				}
				sum += k * pos[w]
				weight += k
			}
			if weight > 0 {
				want[i] = sum / weight
			}
		}
		// As far left as the gaps allow, as far right as they allow, and
		// halfway between, which keeps the gaps too
		left := append([]float64(nil), want...)
		for i := 1; i < len(rank); i++ {
			left[i] = math.Max(left[i], left[i-1]+gap(l.nodes[rank[i-1]], l.nodes[rank[i]]))
		}
		right := append([]float64(nil), want...)
		for i := len(rank) - 2; i >= 0; i-- {
			right[i] = math.Min(right[i], right[i+1]-gap(l.nodes[rank[i]], l.nodes[rank[i+1]]))
		}
		for i, v := range rank {
			pos[v] = (left[i] + right[i]) / 2
		}
	}
	for round := 0; round < 16; round++ {
		for r := 1; r < len(l.ranks); r++ {
			pull(l.ranks[r], up)
		}
		for r := len(l.ranks) - 2; r >= 0; r-- {
			pull(l.ranks[r], down)
		}
	}

	// Along the flow: nodes are on the even ranks, a rank gap apart; the
	// odd ranks between take the room the text on them needs
	depth := make([]float64, len(l.ranks))
	for r, rank := range l.ranks {
		for _, v := range rank {
			depth[r] = math.Max(depth[r], l.along(l.nodes[v]))
		}
	}
	at := 0.0
	center := make([]float64, len(l.ranks))
	for r := range l.ranks {
		if r > 0 {
			space := rankGap / 2
			if r%2 == 1 && depth[r] > 0 {
				space = math.Max(space, 10)
			}
			at += depth[r-1]/2 + space + depth[r]/2
		}
		center[r] = at
	}

	for i, n := range l.nodes {
		n.X, n.Y = pos[i], center[n.rank]
	}
}

// turn maps a point of the top-down layout to where it is in the chart's
// own direction.
func (l *layout) turn(p Point, depth float64) Point {
	switch l.g.Dir {
	case BottomUp:
		return Point{p.X, depth - p.Y}
	case LeftRight:
		return Point{p.Y, p.X}
	case RightLeft:
		return Point{depth - p.Y, p.X}
	}
	return p
}

// draw turns the placed graph into a drawing.
func (l *layout) draw() *Drawing {
	depth := 0.0
	for _, n := range l.nodes {
		depth = math.Max(depth, n.Y+l.along(n)/2)
	}

	d := &Drawing{}
	// Connections, worked out in the top-down layout and then turned
	type path struct {
		segments [][4]Point
		edge     *Edge
		label    *Node
	}
	var paths []path
	for _, e := range l.edges {
		var pts []Point
		var label *Node
		first, last := l.nodes[e.chain[0]], l.nodes[e.chain[len(e.chain)-1]]
		for i, v := range e.chain {
			n := l.nodes[v]
			switch {
			case i == 0:
				pts = append(pts, Point{n.X, n.Y + l.along(n)/2})
			case i == len(e.chain)-1:
				pts = append(pts, Point{n.X, n.Y - l.along(n)/2})
			default:
				pts = append(pts, Point{n.X, n.Y})
				if n.labelW > 0 {
					label = n
				}
			}
		}
		var segs [][4]Point
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			mid := (a.Y + b.Y) / 2
			segs = append(segs, [4]Point{a, {a.X, mid}, {b.X, mid}, b})
		}
		if e.reversed {
			// A connection that leads back leaves and arrives at the sides
			// of its nodes: the faces along the flow belong to the
			// connections that follow it
			const reach = 22.0
			side := func(n *Node, toward float64) (Point, float64) {
				sign := 1.0
				if toward < n.X {
					sign = -1
				}
				return Point{n.X + sign*l.across(n)/2, n.Y}, sign
			}
			n := len(segs) - 1
			if n == 0 {
				a, _ := side(first, first.X+1)
				b, _ := side(last, last.X+1)
				out := math.Max(a.X, b.X) + reach
				segs[0] = [4]Point{a, {out, a.Y}, {out, b.Y}, b}
			} else {
				a, _ := side(first, segs[0][3].X)
				to := segs[0][3]
				segs[0] = [4]Point{a, {to.X, a.Y}, {to.X, (a.Y + to.Y) / 2}, to}
				b, _ := side(last, segs[n][0].X)
				from := segs[n][0]
				segs[n] = [4]Point{from, {from.X, (from.Y + b.Y) / 2}, {from.X, b.Y}, b}
			}
		}
		paths = append(paths, path{segs, e, label})
	}

	// Turn the nodes; their sizes are already the chart's own
	for _, n := range l.nodes {
		p := l.turn(Point{n.X, n.Y}, depth)
		n.X, n.Y = p.X, p.Y
	}

	for _, p := range paths {
		link := Link{Line: p.edge.Line, Start: p.edge.Start, End: p.edge.End}
		for _, seg := range p.segments {
			link.Curve = append(link.Curve, [4]Point{
				l.turn(seg[0], depth), l.turn(seg[1], depth), l.turn(seg[2], depth), l.turn(seg[3], depth),
			})
		}
		if p.edge.reversed {
			// Laid out backwards; drawn the way it was written
			for i, j := 0, len(link.Curve)-1; i < j; i, j = i+1, j-1 {
				link.Curve[i], link.Curve[j] = link.Curve[j], link.Curve[i]
			}
			for i, seg := range link.Curve {
				link.Curve[i] = [4]Point{seg[3], seg[2], seg[1], seg[0]}
			}
		}
		if p.label != nil {
			link.Label = textLines(p.edge.Label, Point{p.label.X, p.label.Y}, l.m.LineHeight)
			w, h := p.label.labelW+6, float64(len(p.edge.Label))*l.m.LineHeight+2
			link.LabelBox = [4]float64{p.label.X - w/2, p.label.Y - h/2, w, h}
		}
		finishLink(&link)
		d.Links = append(d.Links, link)
	}
	for _, e := range l.loops {
		// A connection from a node to itself: a loop at its side
		n := l.nodes[e.From]
		x, reach := n.X+n.W/2, 26.0
		link := Link{Line: e.Line, Start: e.Start, End: e.End, Curve: [][4]Point{{
			{x, n.Y - n.H/4}, {x + reach, n.Y - n.H/2 - 6}, {x + reach, n.Y + n.H/2 + 6}, {x, n.Y + n.H/4},
		}}}
		if len(e.Label) > 0 {
			w, _ := l.textSize(e.Label)
			at := Point{x + reach + w/2, n.Y}
			link.Label = textLines(e.Label, at, l.m.LineHeight)
			link.LabelBox = [4]float64{at.X - w/2 - 3, at.Y - float64(len(e.Label))*l.m.LineHeight/2 - 1, w + 6, float64(len(e.Label))*l.m.LineHeight + 2}
		}
		finishLink(&link)
		d.Links = append(d.Links, link)
	}

	for _, n := range l.g.Nodes {
		box := Box{Text: textLines(n.Label, Point{n.X, n.Y}, l.m.LineHeight)}
		box.Outline, box.Marks = outline(n)
		d.Boxes = append(d.Boxes, box)
	}

	// Frames around the nodes of subgraphs, inner ones first so that an
	// outer one goes around them
	bounds := make([][4]float64, len(l.g.Groups)) // left, top, right, bottom
	used := make([]bool, len(l.g.Groups))
	grow := func(i int, left, top, right, bottom float64) {
		if !used[i] {
			bounds[i], used[i] = [4]float64{left, top, right, bottom}, true
			return
		}
		b := &bounds[i]
		b[0], b[1] = math.Min(b[0], left), math.Min(b[1], top)
		b[2], b[3] = math.Max(b[2], right), math.Max(b[3], bottom)
	}
	for i := len(l.g.Groups) - 1; i >= 0; i-- {
		for _, n := range l.g.Nodes {
			if n.Group == i {
				grow(i, n.X-n.W/2, n.Y-n.H/2, n.X+n.W/2, n.Y+n.H/2)
			}
		}
		for j, inner := range l.g.Groups {
			if inner.Parent == i && used[j] {
				grow(i, bounds[j][0], bounds[j][1], bounds[j][2], bounds[j][3])
			}
		}
		if !used[i] {
			continue
		}
		title := l.g.Groups[i].Title
		head := 0.0
		if title != "" {
			head = l.m.LineHeight
		}
		b := &bounds[i]
		b[0], b[1], b[2], b[3] = b[0]-framePad, b[1]-framePad-head, b[2]+framePad, b[3]+framePad
		// Wide enough for its title
		if w := l.m.Width(title) + 2*framePad; w > b[2]-b[0] {
			extra := (w - (b[2] - b[0])) / 2
			b[0], b[2] = b[0]-extra, b[2]+extra
		}
	}
	for i, group := range l.g.Groups {
		if !used[i] {
			continue
		}
		b := bounds[i]
		d.Frames = append(d.Frames, Frame{
			X: b[0], Y: b[1], W: b[2] - b[0], H: b[3] - b[1],
			Title: group.Title, TitleAt: Point{(b[0] + b[2]) / 2, b[1] + framePad/2 + l.m.LineHeight/2},
		})
	}

	// The nodes say where they are in the finished drawing
	dx, dy := d.fit()
	for _, n := range l.g.Nodes {
		n.X, n.Y = n.X+dx, n.Y+dy
	}
	return d
}

// textLines centers lines of text on a point, one under the other.
func textLines(lines []string, at Point, lineHeight float64) []Text {
	out := make([]Text, len(lines))
	top := at.Y - float64(len(lines)-1)*lineHeight/2
	for i, line := range lines {
		out[i] = Text{At: Point{at.X, top + float64(i)*lineHeight}, Line: line}
	}
	return out
}

// finishLink works out which way the ends of a connection point, and stops
// the curve short of an arrowhead so that the line does not show through
// its point.
func finishLink(link *Link) {
	if len(link.Curve) == 0 {
		return
	}
	dir := func(from, to Point) Point {
		dx, dy := to.X-from.X, to.Y-from.Y
		if length := math.Hypot(dx, dy); length > 0 {
			return Point{dx / length, dy / length}
		}
		return Point{0, 1}
	}
	first, last := &link.Curve[0], &link.Curve[len(link.Curve)-1]
	link.StartDir = dir(first[1], first[0])
	link.EndDir = dir(last[2], last[3])
	if first[1] == first[0] {
		link.StartDir = dir(first[3], first[0])
	}
	if last[2] == last[3] {
		link.EndDir = dir(last[0], last[3])
	}
	if link.End == Arrow {
		last[3].X -= link.EndDir.X * tipLength * 0.8
		last[3].Y -= link.EndDir.Y * tipLength * 0.8
	}
	if link.Start == Arrow {
		first[0].X -= link.StartDir.X * tipLength * 0.8
		first[0].Y -= link.StartDir.Y * tipLength * 0.8
	}
}

// TipAt returns where the tip at an end of a link is: the point of the
// arrow, or the place of the dot or cross.
func (link *Link) TipAt(end bool) Point {
	if end {
		p := link.Curve[len(link.Curve)-1][3]
		if link.End == Arrow {
			return Point{p.X + link.EndDir.X*tipLength*0.8, p.Y + link.EndDir.Y*tipLength*0.8}
		}
		return p
	}
	p := link.Curve[0][0]
	if link.Start == Arrow {
		return Point{p.X + link.StartDir.X*tipLength*0.8, p.Y + link.StartDir.Y*tipLength*0.8}
	}
	return p
}

// ArrowHead returns the triangle of an arrowhead with its point at tip,
// pointing along dir.
func ArrowHead(tip, dir Point) []Point {
	back := Point{tip.X - dir.X*tipLength, tip.Y - dir.Y*tipLength}
	side := Point{-dir.Y * tipLength * 0.45, dir.X * tipLength * 0.45}
	return []Point{tip, {back.X + side.X, back.Y + side.Y}, {back.X - side.X, back.Y - side.Y}}
}

// TipSize is the size of the dot or cross a connection can end in.
const TipSize = 3.5

// fit moves the drawing so that its top left corner is at the margin, and
// measures it. It returns how far it moved it.
func (d *Drawing) fit() (dx, dy float64) {
	left, top := math.Inf(1), math.Inf(1)
	right, bottom := math.Inf(-1), math.Inf(-1)
	see := func(p Point) {
		left, top = math.Min(left, p.X), math.Min(top, p.Y)
		right, bottom = math.Max(right, p.X), math.Max(bottom, p.Y)
	}
	for _, f := range d.Frames {
		see(Point{f.X, f.Y})
		see(Point{f.X + f.W, f.Y + f.H})
	}
	for _, b := range d.Boxes {
		for _, p := range b.Outline {
			see(p)
		}
	}
	for _, link := range d.Links {
		for _, seg := range link.Curve {
			for _, p := range seg {
				see(p)
			}
		}
		if len(link.Label) > 0 {
			see(Point{link.LabelBox[0], link.LabelBox[1]})
			see(Point{link.LabelBox[0] + link.LabelBox[2], link.LabelBox[1] + link.LabelBox[3]})
		}
		see(link.TipAt(true))
		see(link.TipAt(false))
	}
	if math.IsInf(left, 0) {
		return 0, 0
	}
	dx, dy = margin-left, margin-top
	move := func(p *Point) { p.X, p.Y = p.X+dx, p.Y+dy }
	for i := range d.Frames {
		d.Frames[i].X += dx
		d.Frames[i].Y += dy
		move(&d.Frames[i].TitleAt)
	}
	for i := range d.Boxes {
		b := &d.Boxes[i]
		for j := range b.Outline {
			move(&b.Outline[j])
		}
		for _, mark := range b.Marks {
			for j := range mark {
				move(&mark[j])
			}
		}
		for j := range b.Text {
			move(&b.Text[j].At)
		}
	}
	for i := range d.Links {
		link := &d.Links[i]
		for j := range link.Curve {
			for k := range link.Curve[j] {
				move(&link.Curve[j][k])
			}
		}
		for j := range link.Label {
			move(&link.Label[j].At)
		}
		link.LabelBox[0] += dx
		link.LabelBox[1] += dy
	}
	d.W, d.H = right-left+2*margin, bottom-top+2*margin
	return dx, dy
}

// outline returns the shape of a node around its center, and any further
// lines the shape has.
func outline(n *Node) (shape []Point, marks [][]Point) {
	l, r := n.X-n.W/2, n.X+n.W/2
	t, b := n.Y-n.H/2, n.Y+n.H/2
	arc := func(cx, cy, rx, ry, from, to float64) []Point {
		// Enough corners for the curve to look round
		steps := int(math.Max(4, math.Abs(to-from)/7.5))
		pts := make([]Point, 0, steps+1)
		for i := 0; i <= steps; i++ {
			a := (from + (to-from)*float64(i)/float64(steps)) * math.Pi / 180
			pts = append(pts, Point{cx + rx*math.Cos(a), cy + ry*math.Sin(a)})
		}
		return pts
	}
	rounded := func(radius float64) []Point {
		radius = math.Min(radius, math.Min(n.W, n.H)/2)
		var pts []Point
		pts = append(pts, arc(r-radius, t+radius, radius, radius, -90, 0)...)
		pts = append(pts, arc(r-radius, b-radius, radius, radius, 0, 90)...)
		pts = append(pts, arc(l+radius, b-radius, radius, radius, 90, 180)...)
		pts = append(pts, arc(l+radius, t+radius, radius, radius, 180, 270)...)
		return pts
	}
	slant := n.H * 0.3
	switch n.Shape {
	case Round:
		return rounded(6), nil
	case Stadium:
		return rounded(n.H / 2), nil
	case Circle:
		return append(arc(n.X, n.Y, n.W/2, n.H/2, 0, 180), arc(n.X, n.Y, n.W/2, n.H/2, 180, 360)[1:]...), nil
	case Diamond:
		return []Point{{n.X, t}, {r, n.Y}, {n.X, b}, {l, n.Y}}, nil
	case Hexagon:
		in := n.H / 4
		return []Point{{l + in, t}, {r - in, t}, {r, n.Y}, {r - in, b}, {l + in, b}, {l, n.Y}}, nil
	case Parallelogram:
		return []Point{{l + slant, t}, {r, t}, {r - slant, b}, {l, b}}, nil
	case Trapezoid:
		return []Point{{l + slant, t}, {r - slant, t}, {r, b}, {l, b}}, nil
	case Flag:
		return []Point{{l, t}, {r, t}, {r, b}, {l, b}, {l + slant, n.Y}}, nil
	case Subroutine:
		rail := 6.0
		return []Point{{l, t}, {r, t}, {r, b}, {l, b}}, [][]Point{{{l + rail, t}, {l + rail, b}}, {{r - rail, t}, {r - rail, b}}}
	case Cylinder:
		rim := 6.0
		shape = append(shape, arc(n.X, t+rim, n.W/2, rim, 180, 360)...)
		shape = append(shape, arc(n.X, b-rim, n.W/2, rim, 0, 180)...)
		return shape, [][]Point{arc(n.X, t+rim, n.W/2, rim, 0, 180)}
	}
	return []Point{{l, t}, {r, t}, {r, b}, {l, b}}, nil
}
