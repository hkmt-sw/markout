package mathml

import (
	"fmt"
	"strconv"
	"strings"
)

// OMML writes a formula as Office Math Markup, the form Word keeps
// equations in: the m:oMath element for a formula in a line of text, wrapped
// in m:oMathPara for one that stands on its own. Word typesets it with its
// own math font, and the equation can be edited there.
func OMML(root *Node, display bool) string {
	var b strings.Builder
	if display {
		b.WriteString(`<m:oMathPara><m:oMath>`)
	} else {
		b.WriteString(`<m:oMath>`)
	}
	writeNode(&b, root)
	if display {
		b.WriteString(`</m:oMath></m:oMathPara>`)
	} else {
		b.WriteString(`</m:oMath>`)
	}
	return b.String()
}

// unwrap looks through rows that only group one element.
func unwrap(n *Node) *Node {
	for (n.Tag == "mrow" || n.Tag == "mpadded" || n.Tag == "mstyle") && len(n.Kids) == 1 {
		n = n.Kids[0]
	}
	return n
}

// writeRow writes elements that follow each other on the line. A big
// operator takes what follows it as its operand, which is how Word wants a
// sum or an integral: with the thing summed inside it.
func writeRow(b *strings.Builder, kids []*Node) {
	for i := 0; i < len(kids); i++ {
		n := unwrap(kids[i])
		op, lower, upper, stacked, ok := bigOperator(n)
		if !ok {
			writeNode(b, n)
			continue
		}
		end := i + 1
		for end < len(kids) && !endsOperand(unwrap(kids[end])) {
			end++
		}
		b.WriteString(`<m:nary><m:naryPr><m:chr m:val="` + escape(op) + `"/>`)
		if stacked {
			b.WriteString(`<m:limLoc m:val="undOvr"/>`)
		} else {
			b.WriteString(`<m:limLoc m:val="subSup"/>`)
		}
		if lower == nil {
			b.WriteString(`<m:subHide m:val="1"/>`)
		}
		if upper == nil {
			b.WriteString(`<m:supHide m:val="1"/>`)
		}
		b.WriteString(`</m:naryPr>`)
		writeArg(b, "m:sub", lower)
		writeArg(b, "m:sup", upper)
		b.WriteString(`<m:e>`)
		if end > i+1 {
			writeRow(b, kids[i+1:end])
		} else {
			b.WriteString(emptyRun) // an empty operand is shown as a dotted box
		}
		b.WriteString(`</m:e></m:nary>`)
		i = end - 1
	}
}

// emptyRun holds a zero-width space: Word draws a placeholder box where an
// argument has nothing in it.
const emptyRun = `<m:r><m:t>` + "\u200b" + `</m:t></m:r>`

const bigOperators = "∑∏∐∫∬∭∮∯∰⋃⋂⋁⋀⨁⨂⨀⨄⨆"

// bigOperator reports whether an element is a sum, integral or the like,
// alone or with limits, and returns its symbol and limits. stacked says the
// limits go under and over it and not beside it.
func bigOperator(n *Node) (op string, lower, upper *Node, stacked, ok bool) {
	base := n
	switch n.Tag {
	case "msub", "munder":
		if len(n.Kids) == 2 {
			base, lower = unwrap(n.Kids[0]), n.Kids[1]
		}
	case "msup", "mover":
		if len(n.Kids) == 2 {
			base, upper = unwrap(n.Kids[0]), n.Kids[1]
		}
	case "msubsup", "munderover":
		if len(n.Kids) == 3 {
			base, lower, upper = unwrap(n.Kids[0]), n.Kids[1], n.Kids[2]
		}
	}
	if base.Tag != "mo" || len([]rune(base.Text)) != 1 || !strings.Contains(bigOperators, base.Text) {
		return "", nil, nil, false, false
	}
	return base.Text, lower, upper, strings.HasPrefix(n.Tag, "mu") || strings.HasPrefix(n.Tag, "mo") && n != base, true
}

const relations = "=<>≤≥≠≈≡∼≃≅∝→←↔⇒⇐⇔↦∈∉∋⊂⊃⊆⊇≪≫,;"

// endsOperand reports whether an element ends what a big operator applies
// to: a relation, a comma, or a closing bracket.
func endsOperand(n *Node) bool {
	if n.Tag != "mo" {
		return false
	}
	return strings.ContainsAny(n.Text, relations) || n.Attr["form"] == "postfix"
}

func writeArg(b *strings.Builder, tag string, n *Node) {
	b.WriteString("<" + tag + ">")
	if n != nil {
		writeNode(b, n)
	}
	b.WriteString("</" + tag + ">")
}

func writeNode(b *strings.Builder, n *Node) {
	kid := func(i int) *Node {
		if i < len(n.Kids) {
			return n.Kids[i]
		}
		return nil
	}
	switch n.Tag {
	case "mi", "mn", "mo", "mtext", "ms":
		writeRuns(b, n)

	case "mfrac":
		b.WriteString(`<m:f>`)
		if n.Attr["linethickness"] == "0" {
			b.WriteString(`<m:fPr><m:type m:val="noBar"/></m:fPr>`)
		}
		writeArg(b, "m:num", kid(0))
		writeArg(b, "m:den", kid(1))
		b.WriteString(`</m:f>`)

	case "msqrt":
		b.WriteString(`<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/><m:e>`)
		writeRow(b, n.Kids)
		b.WriteString(`</m:e></m:rad>`)

	case "mroot":
		b.WriteString(`<m:rad>`)
		writeArg(b, "m:deg", kid(1))
		writeArg(b, "m:e", kid(0))
		b.WriteString(`</m:rad>`)

	case "msub":
		b.WriteString(`<m:sSub>`)
		writeArg(b, "m:e", kid(0))
		writeArg(b, "m:sub", kid(1))
		b.WriteString(`</m:sSub>`)

	case "msup":
		b.WriteString(`<m:sSup>`)
		writeArg(b, "m:e", kid(0))
		writeArg(b, "m:sup", kid(1))
		b.WriteString(`</m:sSup>`)

	case "msubsup":
		b.WriteString(`<m:sSubSup>`)
		writeArg(b, "m:e", kid(0))
		writeArg(b, "m:sub", kid(1))
		writeArg(b, "m:sup", kid(2))
		b.WriteString(`</m:sSubSup>`)

	case "munder":
		writeUnder(b, kid(0), kid(1))

	case "mover":
		writeOver(b, kid(0), kid(1), n.Is("accent"))

	case "munderover":
		// The part above belongs to the base, the part below to both
		over := &Node{Tag: "mover", Attr: n.Attr, Kids: []*Node{kid(0), kid(2)}}
		writeUnder(b, over, kid(1))

	case "mtable":
		writeMatrix(b, n)

	case "mspace":
		b.WriteString(`<m:r><m:t xml:space="preserve">` + spaceFor(n.Attr["width"]) + `</m:t></m:r>`)

	case "mphantom":
		b.WriteString(`<m:phant><m:phantPr><m:show m:val="0"/></m:phantPr><m:e>`)
		writeRow(b, n.Kids)
		b.WriteString(`</m:e></m:phant>`)

	case "mrow":
		if open, close, inner, ok := fenced(n); ok {
			b.WriteString(`<m:d><m:dPr><m:begChr m:val="` + escape(open) + `"/><m:endChr m:val="` + escape(close) + `"/></m:dPr><m:e>`)
			writeRow(b, inner)
			b.WriteString(`</m:e></m:d>`)
			return
		}
		writeRow(b, n.Kids)

	default: // mstyle, mpadded, menclose and whatever else only groups
		writeRow(b, n.Kids)
	}
}

const (
	openers = "([{|‖⟨⌊⌈"
	closers = ")]}|‖⟩⌋⌉"
)

// fenced reports whether a row is something between brackets that grow with
// it, as \left( ... \right) is, and returns the brackets and what is inside.
// The closing bracket may be missing: \begin{cases} has only a brace.
func fenced(n *Node) (open, close string, inner []*Node, ok bool) {
	if len(n.Kids) < 2 {
		return "", "", nil, false
	}
	isFence := func(k *Node, chars string) bool {
		return k.Tag == "mo" && (k.Is("fence") || k.Is("stretchy")) && k.Text != "" && strings.Contains(chars, k.Text)
	}
	first, last := n.Kids[0], n.Kids[len(n.Kids)-1]
	if !isFence(first, openers) {
		return "", "", nil, false
	}
	if isFence(last, closers) && len(n.Kids) >= 2 {
		return first.Text, last.Text, n.Kids[1 : len(n.Kids)-1], true
	}
	return first.Text, "", n.Kids[1:], true
}

// accents are the marks Word puts on a base, by the character the formula
// has for them.
var accents = map[string]string{
	"^": "\u0302", "ˆ": "\u0302", "~": "\u0303", "˜": "\u0303", "¯": "\u0304", "˙": "\u0307",
	"¨": "\u0308", "⃗": "\u20d7", "→": "\u20d7", "ˇ": "\u030c", "˘": "\u0306", "´": "\u0301", "`": "\u0300",
}

func writeOver(b *strings.Builder, base, over *Node, accent bool) {
	mark := ""
	if over != nil {
		mark = strings.TrimSpace(unwrap(over).Text)
	}
	switch {
	case mark == "‾" || mark == "―" || mark == "_" && accent:
		b.WriteString(`<m:bar><m:barPr><m:pos m:val="top"/></m:barPr>`)
		writeArg(b, "m:e", base)
		b.WriteString(`</m:bar>`)
	case mark == "\u23de":
		b.WriteString(`<m:groupChr><m:groupChrPr><m:chr m:val="` + mark + `"/><m:pos m:val="top"/><m:vertJc m:val="bot"/></m:groupChrPr>`)
		writeArg(b, "m:e", base)
		b.WriteString(`</m:groupChr>`)
	case accent && accents[mark] != "":
		b.WriteString(`<m:acc><m:accPr><m:chr m:val="` + accents[mark] + `"/></m:accPr>`)
		writeArg(b, "m:e", base)
		b.WriteString(`</m:acc>`)
	default:
		b.WriteString(`<m:limUpp>`)
		writeArg(b, "m:e", base)
		writeArg(b, "m:lim", over)
		b.WriteString(`</m:limUpp>`)
	}
}

func writeUnder(b *strings.Builder, base, under *Node) {
	mark := ""
	if under != nil {
		mark = strings.TrimSpace(unwrap(under).Text)
	}
	switch mark {
	case "\u23df":
		b.WriteString(`<m:groupChr><m:groupChrPr><m:chr m:val="` + mark + `"/><m:pos m:val="bot"/></m:groupChrPr>`)
		writeArg(b, "m:e", base)
		b.WriteString(`</m:groupChr>`)
	case "_":
		b.WriteString(`<m:bar><m:barPr><m:pos m:val="bot"/></m:barPr>`)
		writeArg(b, "m:e", base)
		b.WriteString(`</m:bar>`)
	default:
		b.WriteString(`<m:limLow>`)
		writeArg(b, "m:e", base)
		writeArg(b, "m:lim", under)
		b.WriteString(`</m:limLow>`)
	}
}

// writeMatrix writes a table: a matrix, the lines of an aligned equation, or
// the cases of a definition.
func writeMatrix(b *strings.Builder, table *Node) {
	var rows [][]*Node
	columns := 0
	for _, tr := range table.Kids {
		if tr.Tag != "mtr" {
			continue
		}
		var cells []*Node
		for _, td := range tr.Kids {
			if td.Tag == "mtd" {
				cells = append(cells, td)
			}
		}
		rows = append(rows, cells)
		if len(cells) > columns {
			columns = len(cells)
		}
	}
	if columns == 0 {
		return
	}

	b.WriteString(`<m:m><m:mPr><m:mcs>`)
	for c := 0; c < columns; c++ {
		align := "center"
		for _, cells := range rows {
			if c < len(cells) {
				if a := CellAlign(cells[c], table); a != "" {
					align = a
				}
				break
			}
		}
		b.WriteString(`<m:mc><m:mcPr><m:count m:val="1"/><m:mcJc m:val="` + align + `"/></m:mcPr></m:mc>`)
	}
	b.WriteString(`</m:mcs></m:mPr>`)
	for _, cells := range rows {
		b.WriteString(`<m:mr>`)
		for c := 0; c < columns; c++ {
			b.WriteString(`<m:e>`)
			if c < len(cells) && len(cells[c].Kids) > 0 {
				writeRow(b, cells[c].Kids)
			} else {
				b.WriteString(emptyRun)
			}
			b.WriteString(`</m:e>`)
		}
		b.WriteString(`</m:mr>`)
	}
	b.WriteString(`</m:m>`)
}

// CellAlign is how a cell of a table is aligned: "left", "center" or
// "right".
func CellAlign(cell, table *Node) string {
	for _, a := range []string{cell.CSS("text-align"), cell.Attr["columnalign"], table.Attr["columnalign"]} {
		switch a {
		case "left", "center", "right":
			return a
		}
	}
	return "center"
}

// Ems reads a length in em, as "0.17em", and returns 0 for anything else.
func Ems(length string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(length), "em"), 64)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(length), "em") {
		return 0
	}
	return n
}

// spaceFor picks space characters for a width.
func spaceFor(width string) string {
	switch em := Ems(width); {
	case em <= 0:
		return ""
	case em < 0.2:
		return "\u2009" // thin
	case em < 0.4:
		return "\u2005" // a quarter em
	case em < 0.9:
		return "\u2002" // half an em
	default:
		return strings.Repeat("\u2003", int(em+0.5))
	}
}

// writeRuns writes the text of a token as runs, each in one style.
func writeRuns(b *strings.Builder, n *Node) {
	if n.Tag == "mtext" {
		b.WriteString(`<m:r><m:rPr><m:nor/></m:rPr><m:t xml:space="preserve">` + escape(n.Text) + `</m:t></m:r>`)
		return
	}

	// Word sets letters in italics by itself, which is right for a
	// variable but not for a name such as "sin" or an upright letter
	text := []rune(n.Text)
	upright := n.Tag == "mi" && (len(text) > 1 || n.Attr["mathvariant"] == "normal")

	var run []rune
	style := ""
	flush := func() {
		if len(run) == 0 {
			return
		}
		b.WriteString(`<m:r>`)
		if style != "" {
			b.WriteString(`<m:rPr>` + style + `</m:rPr>`)
		}
		b.WriteString(`<m:t xml:space="preserve">` + escape(string(run)) + `</m:t></m:r>`)
		run = nil
	}
	for _, c := range text {
		if c >= 0x2061 && c <= 0x2064 {
			continue // operators that are not shown
		}
		base, variant := Unstyle(c)
		s := ""
		switch variant {
		case Bold:
			s = `<m:sty m:val="b"/>`
		case Italic:
			s = `<m:sty m:val="i"/>`
		case BoldItalic:
			s = `<m:sty m:val="bi"/>`
		case Script:
			s = `<m:scr m:val="script"/>`
		case Fraktur:
			s = `<m:scr m:val="fraktur"/>`
		case DoubleStruck:
			s = `<m:scr m:val="double-struck"/>`
		case SansSerif:
			s = `<m:scr m:val="sans-serif"/>`
		case Monospace:
			s = `<m:scr m:val="monospace"/>`
		default:
			if upright {
				s = `<m:sty m:val="p"/>`
			}
		}
		if s != style {
			flush()
			style = s
		}
		run = append(run, base)
	}
	flush()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// Describe is a short text for a formula that failed to parse, for warnings.
func Describe(tex string, err error) string {
	tex = strings.Join(strings.Fields(tex), " ")
	if len([]rune(tex)) > 40 {
		tex = string([]rune(tex)[:40]) + "…"
	}
	return fmt.Sprintf("%s (%v)", tex, err)
}
