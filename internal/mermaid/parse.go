// Package mermaid reads Mermaid flowcharts and lays them out as a drawing:
// shapes, connecting lines and text with positions, which the renderers put
// on a page. Other kinds of Mermaid diagrams are not drawn.
package mermaid

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
)

// Shape is the outline of a node.
type Shape int

const (
	Rect Shape = iota
	Round
	Stadium
	Subroutine
	Cylinder
	Circle
	Diamond
	Hexagon
	Parallelogram
	Trapezoid
	Flag
)

// Line is how a connection is drawn.
type Line int

const (
	Solid Line = iota
	Dotted
	Thick
	Invisible
)

// Tip is what a connection ends in.
type Tip int

const (
	NoTip Tip = iota
	Arrow
	Dot
	Cross
)

// Direction is the way the chart flows.
type Direction int

const (
	TopDown Direction = iota
	BottomUp
	LeftRight
	RightLeft
)

// Node is a box of the chart.
type Node struct {
	ID    string
	Label []string // lines of text
	Shape Shape
	Group int // index of the subgraph it is in, or -1

	// Set by the layout: the center and the size.
	X, Y, W, H float64

	dummy  bool // a bend of a long connection, not a box
	rank   int
	order  int
	labelW float64
}

// Edge is a connection between two nodes.
type Edge struct {
	From, To   int // indexes of nodes
	Label      []string
	Line       Line
	Start, End Tip
	Length     int // how many ranks it spans at least; 1 normally

	reversed bool
	chain    []int // nodes it passes through, From and To included
}

// Group is a subgraph: a titled frame around nodes.
type Group struct {
	ID, Title string
	Parent    int // index of the subgraph it is in, or -1
}

// Graph is a flowchart as written.
type Graph struct {
	Dir    Direction
	Nodes  []*Node
	Edges  []*Edge
	Groups []*Group

	index map[string]int
}

// ErrNotFlowchart is returned for Mermaid diagrams of another kind.
var ErrNotFlowchart = errors.New("not a flowchart")

// Limits on what is drawn. A chart beyond them is shown as its source.
const (
	MaxNodes = 400
	MaxEdges = 800
)

// Kind returns the keyword a diagram starts with, such as "flowchart",
// "sequenceDiagram" or "gantt": what kind of diagram it is.
func Kind(source string) string {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") || line == "---" {
			continue
		}
		if i := strings.IndexAny(line, " \t;:"); i > 0 {
			line = line[:i]
		}
		return line
	}
	return ""
}

var (
	header   = regexp.MustCompile(`^(?:graph|flowchart)(?:\s+(TB|TD|BT|LR|RL))?\s*;?\s*$`)
	subgraph = regexp.MustCompile(`^subgraph\s+(.*)$`)
	skipped  = regexp.MustCompile(`^(?:style|classDef|class|linkStyle|click|accTitle|accDescr|direction)\b`)
	titled   = regexp.MustCompile(`^(\S+?)\s*\[(.*)\]$`)
	classRef = regexp.MustCompile(`^:::[\w-]+`)
)

// Parse reads a flowchart. It returns ErrNotFlowchart for another kind of
// diagram, and an error that says what was not understood for a flowchart
// it cannot read.
func Parse(source string) (*Graph, error) {
	g := &Graph{index: map[string]int{}}
	var open []int // subgraphs being read, innermost last
	started := false

	for number, raw := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if i := strings.Index(raw, "%%"); i >= 0 {
			raw = raw[:i] // a comment
		}
		for _, stmt := range splitStatements(raw) {
			if !started {
				if stmt == "---" {
					continue // a front matter fence; its content does not parse as a header
				}
				m := header.FindStringSubmatch(stmt)
				if m == nil {
					return nil, ErrNotFlowchart
				}
				switch m[1] {
				case "BT":
					g.Dir = BottomUp
				case "LR":
					g.Dir = LeftRight
				case "RL":
					g.Dir = RightLeft
				}
				started = true
				continue
			}

			switch {
			case stmt == "end":
				if len(open) > 0 {
					open = open[:len(open)-1]
				}
			case subgraph.MatchString(stmt):
				id, title := subgraphName(subgraph.FindStringSubmatch(stmt)[1])
				parent := -1
				if len(open) > 0 {
					parent = open[len(open)-1]
				}
				g.Groups = append(g.Groups, &Group{ID: id, Title: title, Parent: parent})
				open = append(open, len(g.Groups)-1)
			case skipped.MatchString(stmt):
				// Colors and links are the theme's business
			default:
				group := -1
				if len(open) > 0 {
					group = open[len(open)-1]
				}
				if err := g.statement(stmt, group); err != nil {
					return nil, fmt.Errorf("line %d (%s): %w", number+1, shorten(stmt), err)
				}
			}
			if len(g.Nodes) > MaxNodes || len(g.Edges) > MaxEdges {
				return nil, fmt.Errorf("the chart has more than %d boxes or %d connections", MaxNodes, MaxEdges)
			}
		}
	}
	if !started {
		return nil, ErrNotFlowchart
	}
	if len(g.Nodes) == 0 {
		return nil, errors.New("the chart is empty")
	}
	g.connectGroups()
	return g, nil
}

// shorten cuts a statement down to what fits in a message.
func shorten(stmt string) string {
	if r := []rune(stmt); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return stmt
}

// splitStatements splits a line at the semicolons that are not inside
// quotes or brackets.
func splitStatements(line string) []string {
	var out []string
	depth, quoted, start := 0, false, 0
	for i, c := range line {
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '[' || c == '(' || c == '{':
			depth++
		case c == ']' || c == ')' || c == '}':
			depth--
		case c == ';' && depth <= 0:
			out = append(out, line[start:i])
			start = i + 1
		}
	}
	out = append(out, line[start:])
	clean := out[:0]
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	return clean
}

// subgraphName reads what follows "subgraph": an id, a title, or both as
// id [title].
func subgraphName(rest string) (id, title string) {
	rest = strings.TrimSpace(rest)
	if m := titled.FindStringSubmatch(rest); m != nil {
		return m[1], strings.Join(labelLines(m[2]), " ")
	}
	title = strings.Join(labelLines(rest), " ")
	return rest, title
}

// statement reads "A --> B --> C": nodes, alone or with the connections
// between them.
func (g *Graph) statement(s string, group int) error {
	p := &scanner{s: s}
	left, err := g.nodeList(p, group)
	if err != nil {
		return err
	}
	for {
		p.space()
		if p.done() {
			return nil
		}
		edge, ok := p.link()
		if !ok {
			return fmt.Errorf("%q is not understood", strings.TrimSpace(p.rest()))
		}
		right, err := g.nodeList(p, group)
		if err != nil {
			return err
		}
		for _, from := range left {
			for _, to := range right {
				e := edge
				e.From, e.To = from, to
				g.Edges = append(g.Edges, &e)
			}
		}
		left = right
	}
}

// nodeList reads "A & B & C".
func (g *Graph) nodeList(p *scanner, group int) ([]int, error) {
	var out []int
	for {
		p.space()
		n, err := g.node(p, group)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		p.space()
		if !p.eat("&") {
			return out, nil
		}
	}
}

// The brackets that give a node its shape, longest first, with what closes
// each.
var shapes = []struct {
	open, close string
	shape       Shape
}{
	{"(((", ")))", Circle}, {"([", "])", Stadium}, {"[[", "]]", Subroutine}, {"[(", ")]", Cylinder},
	{"((", "))", Circle}, {"{{", "}}", Hexagon}, {"[/", "/]", Parallelogram}, {"[/", `\]`, Trapezoid},
	{`[\`, `\]`, Parallelogram}, {`[\`, "/]", Trapezoid}, {"[", "]", Rect}, {"(", ")", Round},
	{"{", "}", Diamond}, {">", "]", Flag},
}

// node reads an id with an optional shape and text, and returns the node's
// index, adding the node if it is new.
func (g *Graph) node(p *scanner, group int) (int, error) {
	id := p.identifier()
	if id == "" {
		if strings.TrimSpace(p.rest()) == "" {
			return 0, errors.New("a connection leads nowhere")
		}
		return 0, fmt.Errorf("%q is not understood", strings.TrimSpace(p.rest()))
	}
	if strings.HasPrefix(p.rest(), "@{") {
		return 0, errors.New("the @{ } form of a node is not read")
	}

	var label []string
	shape, shaped := Rect, false
	for _, sh := range shapes {
		if !strings.HasPrefix(p.rest(), sh.open) {
			continue
		}
		text, ok := p.bracketed(sh.open, sh.close)
		if !ok {
			continue // "[/x\]" opens like "[/x/]"
		}
		label, shape, shaped = labelLines(text), sh.shape, true
		break
	}
	if !shaped && strings.ContainsAny(firstChar(p.rest()), "[({") {
		return 0, fmt.Errorf("the brackets of %q are not closed", id)
	}
	if m := classRef.FindString(p.rest()); m != "" {
		p.at += len(m)
	}

	i, known := g.index[id]
	if !known {
		i = len(g.Nodes)
		g.index[id] = i
		g.Nodes = append(g.Nodes, &Node{ID: id, Label: []string{id}, Group: group})
	}
	n := g.Nodes[i]
	if shaped {
		n.Shape = shape
		if len(label) > 0 {
			n.Label = label
		}
	}
	// A node belongs to the first subgraph it is written in, whether or not
	// it was written outside of one before
	if n.Group < 0 && group >= 0 {
		n.Group = group
	}
	return i, nil
}

func firstChar(s string) string {
	if s == "" {
		return "\x00"
	}
	return s[:1]
}

var (
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>|\\n`)
	htmlTag   = regexp.MustCompile(`</?[a-zA-Z][^<>]*>`)
	entity    = regexp.MustCompile(`#(\w+);`)
)

// labelLines turns the text of a node or a connection into lines: quotes
// and markup are taken off, <br> breaks the line.
func labelLines(text string) []string {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	text = strings.Trim(text, "`")
	var lines []string
	for _, line := range lineBreak.Split(text, -1) {
		line = htmlTag.ReplaceAllString(line, "")
		line = entity.ReplaceAllString(line, "&$1;") // Mermaid writes entities as #quot;
		line = html.UnescapeString(line)
		line = strings.NewReplacer("**", "", "__", "").Replace(line)
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// connectGroups makes a connection to a subgraph a connection to a node in
// it: a frame is drawn around nodes after they are placed, and has no place
// of its own.
func (g *Graph) connectGroups() {
	byID := map[string]int{}
	for i, group := range g.Groups {
		byID[group.ID] = i
	}
	inGroup := func(node, group int) bool {
		for at := g.Nodes[node].Group; at >= 0; at = g.Groups[at].Parent {
			if at == group {
				return true
			}
		}
		return false
	}
	stand := map[int]int{} // node that is really a subgraph -> a node inside it
	for i, n := range g.Nodes {
		group, isGroup := byID[n.ID]
		if !isGroup {
			continue
		}
		for j := range g.Nodes {
			if j != i && inGroup(j, group) {
				stand[i] = j
				break
			}
		}
	}
	if len(stand) == 0 {
		return
	}
	for _, e := range g.Edges {
		if to, ok := stand[e.From]; ok {
			e.From = to
		}
		if to, ok := stand[e.To]; ok {
			e.To = to
		}
	}
	// The stand-ins for subgraphs are not boxes of their own
	keep := make([]int, len(g.Nodes))
	var nodes []*Node
	for i, n := range g.Nodes {
		if _, gone := stand[i]; gone {
			keep[i] = -1
			continue
		}
		keep[i] = len(nodes)
		nodes = append(nodes, n)
	}
	g.Nodes = nodes
	for _, e := range g.Edges {
		e.From, e.To = keep[e.From], keep[e.To]
	}
}

// scanner reads a statement from left to right.
type scanner struct {
	s  string
	at int
}

func (p *scanner) rest() string { return p.s[p.at:] }
func (p *scanner) done() bool   { return p.at >= len(p.s) }

func (p *scanner) space() {
	for p.at < len(p.s) && (p.s[p.at] == ' ' || p.s[p.at] == '\t') {
		p.at++
	}
}

func (p *scanner) eat(prefix string) bool {
	if strings.HasPrefix(p.rest(), prefix) {
		p.at += len(prefix)
		return true
	}
	return false
}

// identifier reads the id of a node: letters, digits and underscores, with
// single hyphens and dots inside. A hyphen that begins a connection ends it.
func (p *scanner) identifier() string {
	start := p.at
	rest := p.rest()
	end := 0
	for i, c := range rest {
		ok := unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_'
		if !ok && (c == '-' || c == '.') && i > 0 {
			next := rest[i+1:]
			ok = next != "" && !strings.HasPrefix(next, "-") && !strings.HasPrefix(next, ".") && !strings.HasPrefix(next, ">") &&
				(unicode.IsLetter(rune(next[0])) || unicode.IsDigit(rune(next[0])) || next[0] == '_')
			if c == '-' && rest[i-1] == '-' {
				ok = false
			}
		}
		if !ok {
			break
		}
		end = i + len(string(c))
	}
	// "A--o B" and "A--x B": the letter belongs to the connection
	p.at = start + end
	return rest[:end]
}

// bracketed reads text between an opening and a closing bracket. The text
// may be in quotes, which lets it hold brackets itself.
func (p *scanner) bracketed(open, close string) (string, bool) {
	rest := p.rest()[len(open):]
	if trimmed := strings.TrimLeft(rest, " \t"); strings.HasPrefix(trimmed, `"`) {
		if end := strings.Index(trimmed[1:], `"`); end >= 0 {
			after := strings.TrimLeft(trimmed[end+2:], " \t")
			if strings.HasPrefix(after, close) {
				consumed := len(rest) - len(after) + len(close)
				p.at += len(open) + consumed
				return trimmed[1 : end+1], true
			}
		}
	}
	end := strings.Index(rest, close)
	if end < 0 {
		return "", false
	}
	// "[/text\]" must not be closed by the "]" of "/]" read as "[" ... "]"
	if open == "[" || open == "(" || open == "{" {
		if strings.ContainsAny(rest[:end], "[({") && !strings.Contains(rest[:end], `"`) {
			// Nested brackets without quotes: take the last closer on the way
			depth := 0
			for i := 0; i < len(rest); i++ {
				switch rest[i] {
				case open[0]:
					depth++
				case close[0]:
					if depth == 0 {
						end = i
						i = len(rest)
					} else {
						depth--
					}
				}
			}
		}
	}
	p.at += len(open) + end + len(close)
	return rest[:end], true
}

// link reads a connection: "-->", "---", "-.->", "==>", "--o", "<-->", with
// its text as "-- text -->" or "-->|text|".
func (p *scanner) link() (Edge, bool) {
	e := Edge{Length: 1}
	r := linkReader{s: p.rest()}
	e.Start, r.i = linkStart(r.s)

	var ok bool
	if r.linkStroke, ok = strokeAt(r.s[r.i:]); !ok {
		return e, false
	}
	e.Line = r.line

	// The line itself, and for "-- text -->" the text and the line again
	textStart := r.i + 2
	n := r.text(textStart, r.strokes(), &e)

	e.End = r.tip()
	if e.End == NoTip && !r.dotted() {
		n-- // "---" is as long as "-->"
	}
	e.Length = r.length(n)

	if !r.pipeText(&e) {
		return e, false
	}
	p.at += r.i
	return e, true
}

// linkStart reads what a connection starts with ("<", "o" or "x") and
// returns how many characters that took.
func linkStart(s string) (Tip, int) {
	switch {
	case strings.HasPrefix(s, "<"):
		return Arrow, 1
	case len(s) > 2 && (s[0] == 'o' || s[0] == 'x') && (s[1] == '-' || s[1] == '='):
		if s[0] == 'x' {
			return Cross, 1
		}
		return Dot, 1
	}
	return NoTip, 0
}

// linkStroke is how a kind of line is written: a row of one character, or
// for a dotted line dots between two hyphens.
type linkStroke struct {
	line   Line
	stroke byte
	mid    byte // the dot of a dotted line; 0 for the others
	closer byte // what follows the dots
}

// strokeAt tells the kind of line from how it begins.
func strokeAt(s string) (linkStroke, bool) {
	switch {
	case strings.HasPrefix(s, "--"):
		return linkStroke{line: Solid, stroke: '-'}, true
	case strings.HasPrefix(s, "=="):
		return linkStroke{line: Thick, stroke: '='}, true
	case strings.HasPrefix(s, "-."):
		return linkStroke{line: Dotted, stroke: '-', mid: '.', closer: '-'}, true
	case strings.HasPrefix(s, "~~~"):
		return linkStroke{line: Invisible, stroke: '~'}, true
	}
	return linkStroke{}, false
}

func (k linkStroke) dotted() bool { return k.mid != 0 }

// closing is the line that follows the text of "-- text -->".
func (k linkStroke) closing() string {
	switch {
	case k.dotted():
		return ".-"
	case k.stroke == '-':
		return "--"
	case k.stroke == '=':
		return "=="
	}
	return ""
}

// length is how many ranks a line written with n strokes spans.
func (k linkStroke) length(n int) int {
	switch {
	case !k.dotted() && n > 2:
		return n - 1
	case k.dotted() && n > 1:
		return n
	}
	return 1
}

// linkReader reads a connection of one kind of line from left to right.
type linkReader struct {
	linkStroke
	s string
	i int
}

func (r *linkReader) at(c byte) bool { return r.i < len(r.s) && r.s[r.i] == c }

// skip passes a row of one character and returns how long it was.
func (r *linkReader) skip(c byte) int {
	n := 0
	for r.at(c) {
		r.i++
		n++
	}
	return n
}

func (r *linkReader) skipOne(c byte) {
	if r.at(c) {
		r.i++
	}
}

// strokes passes the line and returns its length.
func (r *linkReader) strokes() int {
	if !r.dotted() {
		return r.skip(r.stroke)
	}
	r.skipOne(r.stroke)
	n := r.skip(r.mid) // a dotted line is as long as it has dots
	r.skipOne(r.closer)
	return n
}

// text reads the text that may follow a short line of n strokes, as in
// "-- text -->", "-. text .->" and "== text ==>", and the line after it.
// It returns the length of that line, or n if there is no text.
func (r *linkReader) text(textStart, n int, e *Edge) int {
	if n != 2 && !(r.dotted() && n == 1) {
		return n
	}
	after := r.s[textStart:]
	if !strings.HasPrefix(after, " ") || r.dotted() && r.i != textStart {
		return n
	}
	end := strings.Index(after, " "+r.closing())
	if end < 0 {
		return n
	}
	e.Label = labelLines(after[:end])
	r.i = textStart + end + 1
	if !r.dotted() {
		return r.strokes()
	}
	r.i++ // the dot
	r.skip(r.mid)
	r.skipOne(r.closer)
	return 1
}

// tip reads what the connection ends in: ">", or an "o" or "x" that is not
// the start of a word.
func (r *linkReader) tip() Tip {
	switch {
	case r.at('>'):
		r.i++
		return Arrow
	case r.at('o') || r.at('x'):
		if next := r.i + 1; next == len(r.s) || r.s[next] == ' ' || r.s[next] == '|' {
			r.i++
			if r.s[r.i-1] == 'x' {
				return Cross
			}
			return Dot
		}
	}
	return NoTip
}

// pipeText reads the text of "-->|text|", if there is one. It reports false
// when the text is not closed.
func (r *linkReader) pipeText(e *Edge) bool {
	rest := strings.TrimLeft(r.s[r.i:], " \t")
	if !strings.HasPrefix(rest, "|") {
		return true
	}
	end := strings.Index(rest[1:], "|")
	if end < 0 {
		return false
	}
	e.Label = labelLines(rest[1 : end+1])
	r.i = len(r.s) - len(rest) + end + 2
	return true
}
