package render

import (
	"math"
	"strings"

	"github.com/hkmt-sw/markout/internal/mathml"
	"github.com/hkmt-sw/markout/internal/theme"
)

// Formulas in a PDF are typeset here. A formula arrives as a tree of MathML
// elements (see internal/mathml) and is laid out as boxes: each element
// becomes a box with a width, a height above the baseline and a depth below
// it, built from the boxes of what it holds, in the manner of TeX though
// with far fewer rules. The boxes know how to draw themselves.
//
// Characters come from one font that has everything formulas use, DejaVu
// Math TeX Gyre. Brackets taller than a character, root signs, fraction
// bars and braces are drawn as lines and curves.

// mathBox is a laid-out piece of a formula.
type mathBox struct {
	w, h, d float64            // width, height above the baseline, depth below it
	draw    func(x, y float64) // draws it with its baseline starting at x, y; nil draws nothing
	class   mathClass
	char    bool // a single character: scripts sit at fixed heights beside it
}

// mathClass is what a piece of a formula is, which decides the space
// between it and its neighbors.
type mathClass int

const (
	mathOrd   mathClass = iota // a variable, a number, a fraction
	mathOp                     // a sum, an integral, a function name
	mathBin                    // + and its kind
	mathRel                    // = and its kind
	mathOpen                   // an opening bracket
	mathClose                  // a closing bracket
	mathPunct                  // a comma
	mathBlank                  // space, which the spacing looks through
)

// mathStyle is how a part of a formula is set: its size, whether it has the
// room of a formula on a line of its own, and its color.
type mathStyle struct {
	root    float64 // font size of the formula
	level   int     // 0 for the formula itself, 1 for scripts, 2 for scripts of scripts
	display bool    // big operators are large with their limits above and below
	color   theme.Color
	text    string // font family of words in the formula
}

func (s mathStyle) size() float64 {
	size := s.root
	switch s.level {
	case 0:
	case 1:
		size *= 0.72
	default:
		size *= 0.56
	}
	return math.Max(size, 4.5)
}

// script is the style of a subscript, a superscript or a limit.
func (s mathStyle) script() mathStyle {
	if s.level < 2 {
		s.level++
	}
	s.display = false
	return s
}

// mathKey names the math font among the renderer's families. Themes cannot
// define a family of this name.
const mathKey = "\x00math"

// mathFont is what the layout knows about the math font.
type mathFont struct {
	boxes  *glyphBoxes
	ascent float64 // from the top of a line of text to its baseline, per unit of size
	axis   float64 // height of the line fractions and brackets are centered on
	rule   float64 // thickness of fraction bars and drawn strokes
}

// mathFace loads the math font into the document the first time a formula
// needs it.
func (r *PdfRenderer) mathFace() *mathFont {
	if r.math != nil {
		return r.math
	}
	fm := &mathFont{ascent: typoAscent(fontMath, ttfAscent(fontMath)), axis: 0.27, rule: 0.07}
	if boxes, err := newGlyphBoxes(fontMath); err == nil {
		fm.boxes = boxes
		// The minus sign is on the axis, and as thick as a rule
		if lo, hi, ok := boxes.bounds('−'); ok {
			fm.axis, fm.rule = (lo+hi)/2, math.Min(hi-lo, 0.09)
		}
	}
	if err := r.pdf.AddTTFFontDataWithOption("Math", fontMath, r.glyphOption()); err == nil {
		r.families[mathKey] = &pdfFamily{name: "Math", styles: map[string]bool{"": true}, ascent: fm.ascent, baseline: fm.ascent}
	}
	r.math = fm
	return fm
}

// mathKeyFor identifies a typeset formula, so that laying a paragraph out
// more than once reads each of its formulas once.
type mathKeyFor struct {
	tex     string
	display bool
	size    float64
	color   theme.Color
}

// typeset lays a formula out. ok is false if it cannot be typeset; it is
// then shown as its source, and the conversion says so.
func (r *PdfRenderer) typeset(tex string, display bool, size float64, color theme.Color) (mathBox, bool) {
	key := mathKeyFor{tex, display, size, color}
	if cached, done := r.formulas[key]; done {
		return cached.box, cached.ok
	}
	var result typesetResult
	if root, err := mathml.Parse(tex, display); err != nil {
		r.mathProblems.note(tex, err)
	} else {
		r.mathFace()
		st := mathStyle{root: size, display: display, color: color, text: r.bodyFont()}
		result = typesetResult{box: r.mathNode(root, st), ok: true}
	}
	r.formulas[key] = result
	return result.box, result.ok
}

type typesetResult struct {
	box mathBox
	ok  bool
}

// ---- Characters -------------------------------------------------------------

// mathGlyphs is a box of characters of the math font at a size.
func (r *PdfRenderer) mathGlyphs(text string, st mathStyle, size float64) mathBox {
	fm := r.mathFace()
	r.setFont(mathKey, "", size)
	text = r.usable(text)
	width, _ := r.pdf.MeasureTextWidth(text)

	box := mathBox{w: width, char: len([]rune(text)) == 1}
	found := false
	if fm.boxes != nil {
		for _, c := range text {
			if lo, hi, ok := fm.boxes.bounds(c); ok {
				box.h, box.d = math.Max(box.h, hi*size), math.Max(box.d, -lo*size)
				found = true
			}
		}
	}
	if !found {
		box.h = 0.5 * size
	}
	color := st.color
	box.draw = func(x, y float64) {
		r.setFont(mathKey, "", size)
		r.textColor(color)
		r.pdf.SetX(x)
		r.pdf.SetY(y - fm.ascent*size)
		r.cell(text)
	}
	return box
}

// mathWords is a box of ordinary text, as \text{...} has, in the document's
// own font.
func (r *PdfRenderer) mathWords(text string, st mathStyle) mathBox {
	size := st.size()
	text = strings.ReplaceAll(text, " ", " ")
	r.setFont(st.text, "", size)
	text = r.usable(text)
	width, _ := r.pdf.MeasureTextWidth(text)
	family, color := st.text, st.color
	return mathBox{w: width, h: 0.7 * size, d: 0.22 * size, draw: func(x, y float64) {
		r.setFont(family, "", size)
		r.textColor(color)
		r.pdf.SetX(x)
		r.pdf.SetY(y - r.family(family).baseline*size)
		r.cell(text)
	}}
}

const (
	mathRelations = "=<>≤≥≠≈≡∼≃≅∝→←↔⇒⇐⇔↦∈∉∋⊂⊃⊆⊇≪≫⊥∥≔:∣"
	mathBinaries  = "+−±∓×÷⋅∘∗·∪∩∧∨⊕⊗∖*"
	mathBigOps    = "∑∏∐∫∬∭∮∯∰⋃⋂⋁⋀⨁⨂⨀⨄⨆"
	mathIntegrals = "∫∬∭∮∯∰"
	mathOpeners   = "([{⟨⌊⌈"
	mathClosers   = ")]}⟩⌋⌉"
	mathFences    = "()[]{}|‖⟨⟩⌊⌋⌈⌉"
)

// mathOperator lays out an operator: a sign, a bracket, a big operator.
func (r *PdfRenderer) mathOperator(n *mathml.Node, st mathStyle) mathBox {
	text := strings.Map(func(c rune) rune {
		switch {
		case c >= 0x2061 && c <= 0x2064:
			return -1 // operators that are not shown
		case c == '-':
			return '−'
		}
		return c
	}, n.Text)
	if text == "" {
		return mathBox{class: mathBlank}
	}

	size := st.size()
	big := strings.ContainsAny(text, mathBigOps) && len([]rune(text)) == 1
	if big && st.display && st.level == 0 {
		size *= 1.5
		if strings.Contains(mathIntegrals, text) {
			size *= 1.2
		}
	}
	box := r.mathGlyphs(text, st, size)
	switch {
	case big:
		// A big operator is centered on the axis
		axis := r.mathFace().axis * st.size()
		shift := (box.h-box.d)/2 - axis
		glyph := box.draw
		box.h, box.d = box.h-shift, box.d+shift
		box.draw = func(x, y float64) { glyph(x, y+shift) }
		box.class, box.char = mathOp, false
	case strings.ContainsAny(text, mathRelations):
		box.class = mathRel
	case strings.ContainsAny(text, mathBinaries):
		box.class = mathBin
	case text == "," || text == ";":
		box.class = mathPunct
	case strings.Contains(mathOpeners, text) || n.Attr["form"] == "prefix":
		box.class = mathOpen
	case strings.Contains(mathClosers, text) || n.Attr["form"] == "postfix":
		box.class = mathClose
	}
	return box
}

// ---- Rows -------------------------------------------------------------------

// mathSpace is the space between two neighbors, in units of the font size.
// Scripts are set tight: only the space around operators is kept.
func mathSpace(left, right mathClass, level int) float64 {
	const thin, medium, thick = 3.0 / 18, 4.0 / 18, 5.0 / 18
	space := 0.0
	switch {
	case left == mathPunct:
		space = thin
	case left == mathOp && (right == mathOrd || right == mathOp), right == mathOp && (left == mathOrd || left == mathClose):
		return thin
	case left == mathBin || right == mathBin:
		space = medium
	case left == mathRel && right != mathRel && right != mathPunct && right != mathClose,
		right == mathRel && left != mathRel && left != mathOpen:
		space = thick
	}
	if level > 0 {
		return 0
	}
	return space
}

// stretches reports whether an operator is a bracket that may grow to the
// height of what it encloses, as the brackets of \left( ... \right) do.
func stretches(n *mathml.Node) bool {
	return n.Tag == "mo" && (n.Is("fence") || n.Is("stretchy")) && n.Attr["stretchy"] != "false" &&
		n.Text != "" && strings.Contains(mathFences, n.Text)
}

// mathRow lays elements out one after the other.
func (r *PdfRenderer) mathRow(kids []*mathml.Node, st mathStyle) mathBox {
	if len(kids) == 1 {
		return r.mathNode(kids[0], st)
	}
	em := st.size()
	boxes := make([]mathBox, len(kids))
	for i, kid := range kids {
		if !stretches(kid) {
			boxes[i] = r.mathNode(kid, st)
		}
	}

	// Brackets are as tall as what is between them. They are matched up
	// here, the inner pairs first; one without a partner, like the brace of
	// \begin{cases}, goes with the rest of the row.
	extent := func(from, to int) (h, d float64) {
		for _, box := range boxes[from:to] {
			h, d = math.Max(h, box.h), math.Max(d, box.d)
		}
		return h, d
	}
	var open []int
	for i, kid := range kids {
		if !stretches(kid) {
			continue
		}
		closes := strings.Contains(mathClosers, kid.Text) || kid.Attr["form"] == "postfix"
		if strings.Contains("|‖", kid.Text) && kid.Attr["form"] == "" {
			// A bar closes the bar that is open, and opens one otherwise
			closes = len(open) > 0 && kids[open[len(open)-1]].Text == kid.Text
		}
		if !closes {
			open = append(open, i)
			continue
		}
		from := 0
		if len(open) > 0 {
			from = open[len(open)-1] + 1
			open = open[:len(open)-1]
		}
		h, d := extent(from, i)
		if from > 0 {
			boxes[from-1] = r.mathFence(kids[from-1], h, d, mathOpen, st)
		}
		boxes[i] = r.mathFence(kid, h, d, mathClose, st)
	}
	for len(open) > 0 {
		i := open[len(open)-1]
		open = open[:len(open)-1]
		// ... if it is a bracket put there to grow. A lone bar or brace
		// that merely could is left at its own size
		h, d := 0.0, 0.0
		if kids[i].Is("fence") {
			h, d = extent(i+1, len(boxes))
		}
		boxes[i] = r.mathFence(kids[i], h, d, mathOpen, st)
	}

	// A sign with nothing to its left to work on is a sign, not an
	// operation: the minus of -x has no space after it
	previous := mathOpen
	for i := range boxes {
		if boxes[i].class == mathBlank {
			continue
		}
		if boxes[i].class == mathBin {
			switch previous {
			case mathBin, mathOp, mathRel, mathOpen, mathPunct:
				boxes[i].class = mathOrd
			}
		}
		previous = boxes[i].class
	}

	row := mathBox{char: false}
	offsets := make([]float64, len(boxes))
	last := -1
	for i, box := range boxes {
		if box.class != mathBlank {
			if last >= 0 {
				left, right := boxes[last].class, box.class
				// ... and neither is one with nothing to its right
				if left == mathBin && (right == mathRel || right == mathClose || right == mathPunct) {
					left = mathOrd
				}
				row.w += mathSpace(left, right, st.level) * em
			}
			last = i
		}
		offsets[i] = row.w
		row.w += box.w
		row.h, row.d = math.Max(row.h, box.h), math.Max(row.d, box.d)
	}
	row.draw = func(x, y float64) {
		for i, box := range boxes {
			if box.draw != nil {
				box.draw(x+offsets[i], y)
			}
		}
	}
	return row
}

// ---- Elements ---------------------------------------------------------------

// mathNode lays out one element.
func (r *PdfRenderer) mathNode(n *mathml.Node, st mathStyle) mathBox {
	if n == nil {
		return mathBox{}
	}
	kid := func(i int) *mathml.Node {
		if i < len(n.Kids) {
			return n.Kids[i]
		}
		return nil
	}
	switch n.Tag {
	case "mi":
		return r.mathIdentifier(n, st)
	case "mn":
		return r.mathGlyphs(n.Text, st, st.size())
	case "mo":
		return r.mathOperator(n, st)
	case "mtext", "ms":
		return r.mathWords(n.Text, st)

	case "mspace":
		return mathBox{w: mathml.Ems(n.Attr["width"]) * st.size(), class: mathBlank}

	case "mstyle":
		if c, ok := mathColor(n.Attr["mathcolor"]); ok {
			st.color = c
		}
		switch n.Attr["displaystyle"] {
		case "true":
			st.display = true
		case "false":
			st.display = false
		}
		return r.mathRow(n.Kids, st)

	case "mphantom":
		box := r.mathRow(n.Kids, st)
		box.draw = nil
		return box

	case "mfrac":
		return r.mathFraction(kid(0), kid(1), n.Attr["linethickness"] != "0", st)
	case "msqrt":
		return r.mathRoot(r.mathRow(n.Kids, st), nil, st)
	case "mroot":
		return r.mathRoot(r.mathNode(kid(0), st), kid(1), st)

	case "msub":
		return r.mathScripts(kid(0), kid(1), nil, st)
	case "msup":
		return r.mathScripts(kid(0), nil, kid(1), st)
	case "msubsup":
		return r.mathScripts(kid(0), kid(1), kid(2), st)

	case "munder":
		return r.mathLimits(n, kid(0), kid(1), nil, st)
	case "mover":
		return r.mathLimits(n, kid(0), nil, kid(1), st)
	case "munderover":
		return r.mathLimits(n, kid(0), kid(1), kid(2), st)

	case "mtable":
		return r.mathTable(n, st)
	}
	// mrow, mpadded, menclose and whatever else only groups
	return r.mathRow(n.Kids, st)
}

// mathIdentifier lays out a name: a variable in italics, or a longer name
// such as "sin" upright.
func (r *PdfRenderer) mathIdentifier(n *mathml.Node, st mathStyle) mathBox {
	text := []rune(n.Text)
	if len(text) == 1 && n.Attr["mathvariant"] != "normal" {
		text[0] = mathml.ItalicLetter(text[0])
	}
	box := r.mathGlyphs(string(text), st, st.size())
	if len(text) > 1 {
		box.class = mathOp // a function name stands a little apart from its argument
	}
	return box
}

// mathFraction puts one thing over another, with a bar between unless it is
// a binomial coefficient.
func (r *PdfRenderer) mathFraction(over, under *mathml.Node, bar bool, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	part := st.script()
	gap := 0.1 * em
	if st.display && st.level == 0 {
		part = st
		part.display = false
		gap = 0.2 * em
	}
	num, den := r.mathNode(over, part), r.mathNode(under, part)

	axis, rule := fm.axis*em, fm.rule*em
	if !bar {
		rule = 0
	}
	pad := 0.1 * em
	width := math.Max(num.w, den.w) + 2*pad
	numUp := axis + rule/2 + gap + num.d    // baseline of the numerator above ours
	denDown := -axis + rule/2 + gap + den.h // baseline of the denominator below ours
	color := st.color
	return mathBox{w: width, h: numUp + num.h, d: denDown + den.d, draw: func(x, y float64) {
		if num.draw != nil {
			num.draw(x+(width-num.w)/2, y-numUp)
		}
		if den.draw != nil {
			den.draw(x+(width-den.w)/2, y+denDown)
		}
		if bar {
			r.mathStroke(color, rule)
			r.line(x+pad/2, y-axis, x+width-pad/2, y-axis)
		}
	}}
}

// mathRoot draws a root sign around a box, with the root's degree in its
// crook if it has one.
func (r *PdfRenderer) mathRoot(inner mathBox, degree *mathml.Node, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	rule := fm.rule * em
	clear := 0.14 * em
	top := math.Max(inner.h, 0.5*em) + clear + rule // of the bar, above the baseline
	bottom := inner.d + 0.04*em                     // of the sign, below it
	sign := math.Min(0.5*em+0.06*(top+bottom), em)  // width of the sign

	var index mathBox
	lead := 0.0 // room the degree needs left of the sign
	if degree != nil {
		small := st.script().script()
		index = r.mathNode(degree, small)
		lead = math.Max(0, index.w-0.45*sign)
	}
	hookY := 0.45*(top+bottom) - bottom // where the sign starts, above the baseline
	indexUp := hookY + 0.12*em + index.d
	color := st.color
	box := mathBox{w: lead + sign + inner.w + 0.1*em, h: math.Max(top, indexUp+index.h), d: bottom}
	box.draw = func(x, y float64) {
		x += lead
		r.mathStroke(color, rule*0.8)
		r.line(x+0.05*sign, y-hookY+0.04*em, x+0.2*sign, y-hookY-0.02*em)
		r.mathStroke(color, rule*1.5)
		r.line(x+0.2*sign, y-hookY-0.02*em, x+0.48*sign, y+bottom)
		r.mathStroke(color, rule*0.8)
		r.line(x+0.48*sign, y+bottom, x+sign, y-top+rule/2)
		r.mathStroke(color, rule)
		r.line(x+sign-rule*0.3, y-top+rule/2, x+sign+inner.w+0.08*em, y-top+rule/2)
		if inner.draw != nil {
			inner.draw(x+sign+0.03*em, y)
		}
		if index.draw != nil {
			index.draw(x+0.45*sign-index.w, y-indexUp)
		}
	}
	return box
}

// isBigOperator reports whether an element is a sum, an integral or a
// function name such as "lim", whose limits can go above and below it.
func isBigOperator(n *mathml.Node) bool {
	for n != nil && (n.Tag == "mrow" || n.Tag == "mpadded") && len(n.Kids) == 1 {
		n = n.Kids[0]
	}
	if n == nil {
		return false
	}
	return n.Is("movablelimits") || n.Is("largeop") || n.Tag == "mo" && strings.ContainsAny(n.Text, mathBigOps)
}

// mathScripts sets a subscript and a superscript beside a base.
func (r *PdfRenderer) mathScripts(base, sub, sup *mathml.Node, st mathStyle) mathBox {
	b := r.mathNode(base, st)
	return r.attachScripts(b, r.scriptBox(sub, st), r.scriptBox(sup, st), st)
}

func (r *PdfRenderer) scriptBox(n *mathml.Node, st mathStyle) mathBox {
	if n == nil {
		return mathBox{}
	}
	return r.mathNode(n, st.script())
}

func (r *PdfRenderer) attachScripts(b, sub, sup mathBox, st mathStyle) mathBox {
	em, small := st.size(), st.script().size()
	// Beside a character, scripts are at fixed heights; beside something
	// taller, at its top and bottom
	up, down := 0.42*em, 0.17*em
	if !b.char {
		up, down = math.Max(up, b.h-0.45*small), math.Max(down, b.d+0.12*small)
	}
	if sup.draw != nil && up-sup.d < 0.22*em {
		up = 0.22*em + sup.d // clear of the base's lower-case letters
	}
	if sub.draw != nil && sup.draw != nil {
		if overlap := 0.1*em - ((up - sup.d) - (sub.h - down)); overlap > 0 {
			down += overlap
		}
	}
	at := b.w + 0.04*em
	box := mathBox{w: at + math.Max(sub.w, sup.w) + 0.03*em, h: b.h, d: b.d, class: b.class}
	if sup.draw != nil {
		box.h = math.Max(box.h, up+sup.h)
	}
	if sub.draw != nil {
		box.d = math.Max(box.d, down+sub.d)
	}
	if box.class == mathBin || box.class == mathRel {
		box.class = mathOrd
	}
	box.draw = func(x, y float64) {
		if b.draw != nil {
			b.draw(x, y)
		}
		if sup.draw != nil {
			sup.draw(x+at, y-up)
		}
		if sub.draw != nil {
			sub.draw(x+at, y+down)
		}
	}
	return box
}

// mathLimits sets things under and over a base: the limits of a sum, an
// accent, a brace.
func (r *PdfRenderer) mathLimits(n, base, under, over *mathml.Node, st mathStyle) mathBox {
	big := isBigOperator(base)
	b := r.mathNode(base, st)
	if big {
		b.class = mathOp
		// On a line of text there is no room above and below
		if !st.display {
			return r.attachScripts(b, r.scriptBox(under, st), r.scriptBox(over, st), st)
		}
	}
	em := st.size()
	if over != nil {
		if mark := strings.TrimSpace(overText(over)); mark != "" && (n.Is("accent") || mark == "⏞") {
			b = r.mathAccent(b, mark, st)
			over = nil
		}
	}
	if under != nil {
		if mark := strings.TrimSpace(overText(under)); mark == "⏟" {
			b = r.mathBrace(b, false, st)
			under = nil
		}
	}
	if under == nil && over == nil {
		return b
	}

	gap := 0.12 * em
	lower, upper := r.scriptBox(under, st), r.scriptBox(over, st)
	width := math.Max(b.w, math.Max(lower.w, upper.w))
	box := mathBox{w: width, h: b.h, d: b.d, class: b.class}
	upperUp, lowerDown := b.h+gap+upper.d, b.d+gap+lower.h
	if upper.draw != nil {
		box.h = upperUp + upper.h
	}
	if lower.draw != nil {
		box.d = lowerDown + lower.d
	}
	box.draw = func(x, y float64) {
		if b.draw != nil {
			b.draw(x+(width-b.w)/2, y)
		}
		if upper.draw != nil {
			upper.draw(x+(width-upper.w)/2, y-upperUp)
		}
		if lower.draw != nil {
			lower.draw(x+(width-lower.w)/2, y+lowerDown)
		}
	}
	return box
}

// overText is the text of an element that is a single token, looking
// through rows that only wrap it.
func overText(n *mathml.Node) string {
	for (n.Tag == "mrow" || n.Tag == "mpadded") && len(n.Kids) == 1 {
		n = n.Kids[0]
	}
	if !n.IsToken() {
		return ""
	}
	return n.Text
}

// The marks of accents, by the character a formula has for them, as the
// spacing character the font draws.
var mathAccents = map[string]rune{
	"^": 'ˆ', "ˆ": 'ˆ', "~": '˜', "˜": '˜', "˙": '˙', "¨": '¨', "ˇ": 'ˇ', "˘": '˘', "´": '´', "`": '`',
}

// mathAccent puts an accent over a box: a bar, an arrow, a hat.
func (r *PdfRenderer) mathAccent(b mathBox, mark string, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	rule, color := fm.rule*em, st.color
	base := b.draw
	switch {
	case mark == "⏞":
		return r.mathBrace(b, true, st)

	case mark == "‾" || mark == "¯" || mark == "―" || mark == "_":
		up := b.h + 0.12*em
		b.h, b.char = up+rule, false
		b.draw = func(x, y float64) {
			base(x, y)
			r.mathStroke(color, rule)
			r.line(x+0.02*em, y-up, x+b.w-0.02*em, y-up)
		}

	case mark == "⃗" || mark == "→":
		up := b.h + 0.16*em
		// An arrow longer than a narrow letter makes room for itself
		length := math.Max(b.w, 0.5*em)
		inset := (length - b.w) / 2
		b.w, b.h, b.char = length, up+0.14*em, false
		b.draw = func(x, y float64) {
			base(x+inset, y)
			from := x + 0.03*em
			to := x + length
			r.mathStroke(color, rule*0.8)
			r.line(from, y-up, to, y-up)
			r.line(to-0.16*em, y-up-0.09*em, to, y-up)
			r.line(to-0.16*em, y-up+0.09*em, to, y-up)
		}

	default:
		glyph, known := mathAccents[mark]
		if !known {
			glyph = []rune(mark)[0]
		}
		accent := r.mathGlyphs(string(glyph), st, em)
		// The font draws the mark above lower-case letters; it is moved to
		// sit just over this base
		bottom := accent.h - 0.22*em
		if fm.boxes != nil {
			if lo, _, ok := fm.boxes.bounds(glyph); ok {
				bottom = lo * em
			}
		}
		lift := b.h + 0.05*em - bottom
		skew := 0.0
		if b.char {
			skew = 0.05 * em // letters lean
		}
		b.h, b.char = lift+accent.h, false
		b.draw = func(x, y float64) {
			base(x, y)
			accent.draw(x+(b.w-accent.w)/2+skew, y-lift)
		}
	}
	return b
}

// mathBrace draws a brace lying over or under a box.
func (r *PdfRenderer) mathBrace(b mathBox, above bool, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	rule, color := fm.rule*em*0.8, st.color
	height, gap := 0.3*em, 0.1*em
	base := b.draw
	up, down := b.h+gap, b.d+gap
	box := b
	box.char = false
	if above {
		box.h = up + height
	} else {
		box.d = down + height
	}
	box.draw = func(x, y float64) {
		if base != nil {
			base(x, y)
		}
		// From the ends towards the tip in the middle
		edge, tip := y+down, y+down+height
		if above {
			edge, tip = y-up, y-up-height
		}
		mid, bend := x+b.w/2, math.Min(height, b.w/4)
		half := (edge + tip) / 2
		r.mathStroke(color, rule)
		r.curve(x, edge, x, half, x+bend, half, x+bend, half)
		r.line(x+bend, half, mid-bend, half)
		r.curve(mid-bend, half, mid, half, mid, tip, mid, tip)
		r.curve(mid, tip, mid, half, mid+bend, half, mid+bend, half)
		r.line(mid+bend, half, x+b.w-bend, half)
		r.curve(x+b.w-bend, half, x+b.w, half, x+b.w, edge, x+b.w, edge)
	}
	return box
}

// mathFence lays out a bracket beside something h high and d deep. One that
// a character is tall enough for is that character; a taller one is drawn.
func (r *PdfRenderer) mathFence(n *mathml.Node, h, d float64, class mathClass, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	glyph := r.mathGlyphs(n.Text, st, em)
	glyph.class, glyph.char = class, false
	if h <= glyph.h+0.05*em && d <= glyph.d+0.05*em {
		return glyph
	}

	// Brackets are centered on the axis, and reach a little past the content
	axis := fm.axis * em
	half := math.Max(h-axis, d+axis) + 0.08*em
	top, bottom := axis+half, half-axis // above and below the baseline
	total := top + bottom
	rule, color := fm.rule*em*0.75, st.color
	width := math.Min(0.3*em+0.04*total, 0.55*em)
	char := n.Text
	// A little room on the outside, so that what follows does not touch it
	margin := 0.07 * em
	box := mathBox{w: width + margin, h: top, d: bottom, class: class}
	box.draw = func(x, y float64) {
		if class == mathOpen {
			x += margin
		}
		t, b := y-top, y+bottom
		l, rt := x+0.18*width, x+0.82*width
		r.mathStroke(color, rule)
		switch char {
		case "(":
			r.curve(rt, t, l-0.35*width, t+0.3*total, l-0.35*width, b-0.3*total, rt, b)
		case ")":
			r.curve(l, t, rt+0.35*width, t+0.3*total, rt+0.35*width, b-0.3*total, l, b)
		case "[":
			r.line(rt, t, l, t)
			r.line(l, t-rule/2, l, b+rule/2)
			r.line(l, b, rt, b)
		case "]":
			r.line(l, t, rt, t)
			r.line(rt, t-rule/2, rt, b+rule/2)
			r.line(rt, b, l, b)
		case "⌊":
			r.line(l, t, l, b+rule/2)
			r.line(l, b, rt, b)
		case "⌋":
			r.line(rt, t, rt, b+rule/2)
			r.line(rt, b, l, b)
		case "⌈":
			r.line(rt, t, l, t)
			r.line(l, t-rule/2, l, b)
		case "⌉":
			r.line(l, t, rt, t)
			r.line(rt, t-rule/2, rt, b)
		case "|":
			r.line(x+width/2, t, x+width/2, b)
		case "‖":
			r.line(x+0.35*width, t, x+0.35*width, b)
			r.line(x+0.65*width, t, x+0.65*width, b)
		case "⟨":
			r.line(rt, t, l, (t+b)/2)
			r.line(l, (t+b)/2, rt, b)
		case "⟩":
			r.line(l, t, rt, (t+b)/2)
			r.line(rt, (t+b)/2, l, b)
		case "{", "}":
			// A spine with a tip in the middle, pointing away from the content
			spine, tip, ends := x+width/2, l, rt
			if char == "}" {
				tip, ends = rt, l
			}
			mid, bend := (t+b)/2, math.Min(width/2, total/6)
			r.curve(ends, t, spine, t, spine, t+bend, spine, t+bend)
			r.line(spine, t+bend, spine, mid-bend)
			r.curve(spine, mid-bend, spine, mid, tip, mid, tip, mid)
			r.curve(tip, mid, spine, mid, spine, mid+bend, spine, mid+bend)
			r.line(spine, mid+bend, spine, b-bend)
			r.curve(spine, b-bend, spine, b, ends, b, ends, b)
		}
	}
	return box
}

// mathTable lays out rows and columns: a matrix, the lines of an aligned
// equation, the cases of a definition.
func (r *PdfRenderer) mathTable(table *mathml.Node, st mathStyle) mathBox {
	fm, em := r.mathFace(), st.size()
	cellStyle := st
	cellStyle.display = table.Is("displaystyle")

	type cell struct {
		box         mathBox
		align       string
		left, right float64 // space the cell asks for at its sides
	}
	var rows [][]cell
	columns := 0
	for _, tr := range table.Kids {
		if tr.Tag != "mtr" {
			continue
		}
		var cells []cell
		for _, td := range tr.Kids {
			if td.Tag != "mtd" {
				continue
			}
			c := cell{box: r.mathRow(td.Kids, cellStyle), align: mathml.CellAlign(td, table), left: 0.4, right: 0.4}
			// "a &= b" breaks the line before the sign, which keeps the
			// space it has when what is left of it is in the same row
			if lead := leadingSpace(td, st.level); lead > 0 && c.box.draw != nil {
				inner, shift := c.box.draw, lead*em
				c.box.w += shift
				c.box.draw = func(x, y float64) { inner(x+shift, y) }
			}
			if td.CSS("padding-left") != "" || td.CSS("padding-right") != "" {
				c.left, c.right = mathml.Ems(td.CSS("padding-left")), mathml.Ems(td.CSS("padding-right"))
			}
			cells = append(cells, c)
		}
		rows = append(rows, cells)
		columns = max(columns, len(cells))
	}
	if columns == 0 {
		return mathBox{}
	}

	widths := make([]float64, columns)
	lefts, rights := make([]float64, columns), make([]float64, columns)
	for _, cells := range rows {
		for c, cl := range cells {
			widths[c] = math.Max(widths[c], cl.box.w)
			lefts[c], rights[c] = math.Max(lefts[c], cl.left*em), math.Max(rights[c], cl.right*em)
		}
	}
	// Columns start after the space the cells ask for; the table's own
	// edges have none
	starts := make([]float64, columns)
	x := 0.0
	for c := range widths {
		if c > 0 {
			x += rights[c-1] + lefts[c]
		}
		starts[c] = x
		x += widths[c]
	}
	total := x

	// Every row is at least as tall as a line of text, and the table is
	// centered on the axis
	gap := 0.35 * em
	if cellStyle.display {
		gap = 0.55 * em
	}
	heights, depths := make([]float64, len(rows)), make([]float64, len(rows))
	height := 0.0
	for i, cells := range rows {
		heights[i], depths[i] = 0.72*em, 0.24*em
		for _, cl := range cells {
			heights[i], depths[i] = math.Max(heights[i], cl.box.h), math.Max(depths[i], cl.box.d)
		}
		height += heights[i] + depths[i]
		if i > 0 {
			height += gap
		}
	}
	top := height/2 + fm.axis*em // above the baseline

	box := mathBox{w: total, h: top, d: height - top}
	box.draw = func(x, y float64) {
		rowY := y - top
		for i, cells := range rows {
			rowY += heights[i]
			for c, cl := range cells {
				if cl.box.draw == nil {
					continue
				}
				at := x + starts[c]
				switch cl.align {
				case "right":
					at += widths[c] - cl.box.w
				case "center":
					at += (widths[c] - cl.box.w) / 2
				}
				cl.box.draw(at, rowY)
			}
			rowY += depths[i] + gap
		}
	}
	return box
}

// leadingSpace is the space a cell's content has before it because of the
// sign it starts with.
func leadingSpace(cell *mathml.Node, level int) float64 {
	if len(cell.Kids) == 0 {
		return 0
	}
	first := cell.Kids[0]
	for (first.Tag == "mrow" || first.Tag == "mpadded") && len(first.Kids) > 0 {
		first = first.Kids[0]
	}
	if first.Tag != "mo" {
		return 0
	}
	switch {
	case strings.ContainsAny(first.Text, mathRelations):
		return mathSpace(mathOrd, mathRel, level)
	case strings.ContainsAny(first.Text, mathBinaries+"-"):
		return mathSpace(mathOrd, mathBin, level)
	}
	return 0
}

// ---- Drawing ----------------------------------------------------------------

// mathStroke sets the pen that draws bars, root signs and brackets.
func (r *PdfRenderer) mathStroke(c theme.Color, width float64) {
	r.strokeColor(c)
	r.lineWidth(math.Max(width, 0.3))
}

// curve draws a Bézier curve from the first point to the last, pulled
// towards the two between.
func (r *PdfRenderer) curve(x0, y0, x1, y1, x2, y2, x3, y3 float64) {
	r.logf("curve x=%.1f y=%.1f to x=%.1f y=%.1f %s/%.1f", x0, y0, x3, y3, r.stroke, r.strokeWidth)
	r.pdf.Curve(x0, y0, x1, y1, x2, y2, x3, y3, "D")
}

// mathColors are the color names formulas use with \color.
var mathColors = map[string]string{
	"black": "000000", "white": "FFFFFF", "red": "D32F2F", "green": "2E7D32", "blue": "1565C0",
	"gray": "757575", "grey": "757575", "orange": "EF6C00", "purple": "6A1B9A", "brown": "5D4037",
	"teal": "00796B", "cyan": "0097A7", "magenta": "C2185B", "yellow": "F9A825", "violet": "6A1B9A",
}

func mathColor(name string) (theme.Color, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return theme.Color{}, false
	}
	if hex, ok := mathColors[name]; ok {
		return theme.Hex(hex), true
	}
	c, err := theme.ParseColor(name)
	return c, err == nil && strings.HasPrefix(name, "#")
}
