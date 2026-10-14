// Package dollarmath is a goldmark extension that reads math written between
// dollar signs: $x^2$ in a line of text, and $$ lines around a block.
//
// It only builds the syntax tree. Markout walks that tree itself, so there is
// no HTML renderer here.
package dollarmath

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Extension is the goldmark extension to pass to goldmark.WithExtensions.
var Extension goldmark.Extender = extension{}

type extension struct{}

// Extend adds the block and the inline parser to goldmark.
func (extension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(util.Prioritized(blockParser{}, 701)),
		parser.WithInlineParsers(util.Prioritized(inlineParser{}, 501)),
	)
}

// KindMathBlock is the node kind of a MathBlock.
var KindMathBlock = ast.NewNodeKind("MathBlock")

// MathBlock is a run of lines between two lines of $$. The lines are in
// Lines(), as written.
type MathBlock struct {
	ast.BaseBlock

	// indent is the column of the opening $$, taken off the lines that follow.
	indent int
}

// Kind implements ast.Node.
func (n *MathBlock) Kind() ast.NodeKind { return KindMathBlock }

// IsRaw tells goldmark not to read the lines as Markdown.
func (n *MathBlock) IsRaw() bool { return true }

// Dump implements ast.Node.
func (n *MathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// KindInlineMath is the node kind of an InlineMath.
var KindInlineMath = ast.NewNodeKind("InlineMath")

// InlineMath is math inside a line of text. Its children are the raw text
// segments between the dollar signs.
type InlineMath struct {
	ast.BaseInline
}

// Kind implements ast.Node.
func (n *InlineMath) Kind() ast.NodeKind { return KindInlineMath }

// Inline marks the node as an inline one for goldmark.
func (n *InlineMath) Inline() {}

// Dump implements ast.Node.
func (n *InlineMath) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// isBlank reports whether the node holds nothing but white space.
func (n *InlineMath) isBlank(source []byte) bool {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok && !util.IsBlank(t.Segment.Value(source)) {
			return false
		}
	}
	return true
}

// countDollars returns how many '$' start line.
func countDollars(line []byte) int {
	n := 0
	for n < len(line) && line[n] == '$' {
		n++
	}
	return n
}

type blockParser struct{}

// Trigger returns nil: the parser is tried on every line.
func (blockParser) Trigger() []byte { return nil }

// Open starts a block at a line that begins with two or more dollar signs.
func (blockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos >= len(line) || countDollars(line[pos:]) < 2 {
		return nil, parser.NoChildren
	}
	return &MathBlock{indent: pos}, parser.NoChildren
}

// Continue adds a line to the block, or closes it at a line of two or more
// dollar signs with nothing else on it.
func (blockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, segment := reader.PeekLine()
	if width, pos := util.IndentWidth(line, 0); width < 4 {
		n := countDollars(line[pos:])
		if n >= 2 && util.IsBlank(line[pos+n:]) {
			reader.Advance(segment.Stop - segment.Start - segment.Padding)
			return parser.Close
		}
	}
	pos, padding := util.DedentPosition(line, 0, node.(*MathBlock).indent)
	node.Lines().Append(text.NewSegmentPadding(segment.Start+pos, segment.Stop, padding))
	reader.AdvanceAndSetPadding(segment.Stop-segment.Start-pos-1, padding)
	return parser.Continue | parser.NoChildren
}

// Close has nothing to finish.
func (blockParser) Close(ast.Node, text.Reader, parser.Context) {}

// CanInterruptParagraph lets a $$ line end a paragraph.
func (blockParser) CanInterruptParagraph() bool { return true }

// CanAcceptIndentedLine keeps a line indented by four or more from opening a block.
func (blockParser) CanAcceptIndentedLine() bool { return false }

type inlineParser struct{}

// Trigger makes goldmark call Parse at each dollar sign.
func (inlineParser) Trigger() []byte { return []byte{'$'} }

// Parse reads math that starts at a run of dollar signs and ends at the next
// run of the same length, over as many lines as it takes. A run with no match
// stays plain text.
func (inlineParser) Parse(_ ast.Node, reader text.Reader, _ parser.Context) ast.Node {
	line, first := reader.PeekLine()
	opener := countDollars(line)
	reader.Advance(opener)
	startLine, startPos := reader.Position()
	node := &InlineMath{}
	for {
		line, segment := reader.PeekLine()
		if line == nil {
			reader.SetPosition(startLine, startPos)
			return ast.NewTextSegment(first.WithStop(first.Start + opener))
		}
		if end := closingRun(line, opener); end >= 0 {
			if body := segment.WithStop(segment.Start + end - opener); !body.IsEmpty() {
				node.AppendChild(node, ast.NewRawTextSegment(body))
			}
			reader.Advance(end)
			break
		}
		if !util.IsBlank(line) {
			node.AppendChild(node, ast.NewRawTextSegment(segment))
		}
		reader.AdvanceLine()
	}
	trimOneSpace(node, reader.Source())
	return node
}

// closingRun returns the offset just after the first run of exactly n dollar
// signs in line, or -1. A run does not close when the character after the one
// that follows it is another dollar sign.
func closingRun(line []byte, n int) int {
	for i := 0; i < len(line); i++ {
		if line[i] != '$' {
			continue
		}
		start := i
		for i < len(line) && line[i] == '$' {
			i++
		}
		if i-start == n && (i+1 >= len(line) || line[i+1] != '$') {
			return i
		}
	}
	return -1
}

// trimOneSpace takes one space off each end of the math, when both ends have
// one, so that $ x $ is read as x.
func trimOneSpace(node *InlineMath, source []byte) {
	if node.isBlank(source) {
		return
	}
	first, ok1 := node.FirstChild().(*ast.Text)
	last, ok2 := node.LastChild().(*ast.Text)
	if !ok1 || !ok2 || first.Segment.IsEmpty() || last.Segment.IsEmpty() {
		return
	}
	if source[first.Segment.Start] != ' ' || source[last.Segment.Stop-1] != ' ' {
		return
	}
	first.Segment = first.Segment.WithStart(first.Segment.Start + 1)
	last.Segment = last.Segment.WithStop(last.Segment.Stop - 1)
}
