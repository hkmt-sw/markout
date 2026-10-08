// Package mathml reads LaTeX math into a tree of MathML elements, which is
// what the renderers typeset: PDF by laying the tree out itself, DOCX by
// turning it into a Word equation.
package mathml

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Node is a MathML element: mi, mo, mfrac, mrow and so on.
type Node struct {
	Tag  string            // without namespace: "mfrac"
	Text string            // of a token element (mi, mn, mo, mtext)
	Attr map[string]string // attributes; "style" holds inline CSS
	Kids []*Node
}

// MaxLength is the longest formula that is typeset. Longer ones are shown as
// their source, like formulas that cannot be read.
const MaxLength = 20000

// Parse reads a LaTeX formula, without its delimiters. display says whether
// it stands on its own (as between $$) or in a line of text. The root of
// the result is an mrow. An error means the formula cannot be typeset: it
// is too long, not well-formed, or uses a command that is not known.
func Parse(tex string, display bool) (root *Node, err error) {
	if len(tex) > MaxLength {
		return nil, fmt.Errorf("the formula is longer than %d characters", MaxLength)
	}
	// The translator is given text from documents of any origin
	defer func() {
		if p := recover(); p != nil {
			root, err = nil, fmt.Errorf("the formula could not be read")
		}
	}()

	source := prepare(tex)
	if display {
		source = align(source)
	}
	mml, err := translate(source, display)
	if err != nil {
		// The translator failing on a formula says nothing a reader can use
		if strings.HasPrefix(err.Error(), "TreeBlood") {
			return nil, fmt.Errorf("the formula could not be read")
		}
		// The message goes on to point at the place in HTML
		msg, _, _ := strings.Cut(err.Error(), "<")
		return nil, fmt.Errorf("%s", strings.TrimSpace(msg))
	}
	math, err := decode(mml)
	if err != nil {
		return nil, err
	}
	if unknown := find(math, "merror"); unknown != nil {
		return nil, fmt.Errorf(`\%s is not a command markout knows`, strings.TrimSpace(text(unknown)))
	}

	// <math><semantics><mrow>…</mrow><annotation>…</annotation></semantics></math>
	root = math
	for len(root.Kids) > 0 && (root.Tag == "math" || root.Tag == "semantics") {
		root = root.Kids[0]
	}
	if root.Tag != "mrow" {
		root = &Node{Tag: "mrow", Kids: []*Node{root}}
	}
	return root, nil
}

var (
	operatorName = regexp.MustCompile(`\\operatorname\*?\s*\{`)
	boxed        = regexp.MustCompile(`\\(boxed|fbox)\s*\{`)
	// Environments that number their formulas, or have a starred form that
	// does not; numbering is LaTeX's business, so the plain form is read
	starred = regexp.MustCompile(`\\(begin|end)\{(equation|align|alignat|flalign|gather|multline|eqnarray)\*\}`)
	// Lines one under the other, centered: what a one-column matrix is
	gathered = regexp.MustCompile(`\\(begin|end)\{(?:gather|gathered|multline)\}`)
	eqnarray = regexp.MustCompile(`\\(begin|end)\{(?:eqnarray|flalign)\}`)
	labels   = regexp.MustCompile(`\\(label|tag|nonumber|notag)\b\*?(\{[^{}]*\})?`)
)

// prepare rewrites a few common commands the translator does not know into
// ones it does, and drops those that only matter to LaTeX's numbering.
func prepare(tex string) string {
	tex = operatorName.ReplaceAllString(tex, `\mathrm{`)
	tex = boxed.ReplaceAllString(tex, `{`)
	tex = starred.ReplaceAllString(tex, `\$1{$2}`)
	tex = gathered.ReplaceAllString(tex, `\$1{matrix}`)
	tex = eqnarray.ReplaceAllString(tex, `\$1{align}`)
	return labels.ReplaceAllString(tex, "")
}

// align wraps a formula of several lines that names no environment in
// "aligned": lines broken with \\ and lined up at & are how such a formula
// is written between $$ and in MyST's math directive, where LaTeX would
// need the environment spelled out.
func align(tex string) string {
	if !strings.Contains(tex, `\\`) || strings.Contains(tex, `\begin{`) {
		return tex
	}
	return `\begin{aligned}` + tex + `\end{aligned}`
}

// decode reads MathML text into nodes, leaving out the annotation that
// repeats the source.
func decode(mml string) (*Node, error) {
	dec := xml.NewDecoder(strings.NewReader(mml))
	dec.Strict = false
	dec.Entity = entities
	var stack []*Node
	var root *Node
	skip := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the formula could not be read")
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if skip > 0 || el.Name.Local == "annotation" {
				skip++
				continue
			}
			n := &Node{Tag: el.Name.Local, Attr: map[string]string{}}
			for _, a := range el.Attr {
				n.Attr[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Kids = append(parent.Kids, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if skip == 0 && len(stack) > 0 {
				if n := stack[len(stack)-1]; n.IsToken() || n.Tag == "merror" {
					n.Text += string(el)
				}
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("the formula is empty")
	}
	return root, nil
}

// entities are the named characters the translator writes that XML does not
// define.
var entities = func() map[string]string {
	m := map[string]string{
		"nbsp": "\u00a0", "UnderBrace": "\u23df", "OverBrace": "\u23de",
		"UnderBar": "_", "OverBar": "\u203e", "ApplyFunction": "\u2061",
		"InvisibleTimes": "\u2062", "InvisibleComma": "\u2063",
	}
	for name, value := range xml.HTMLEntity {
		if _, ok := m[name]; !ok {
			m[name] = value
		}
	}
	return m
}()

// IsToken reports whether the element holds text and not other elements.
func (n *Node) IsToken() bool {
	switch n.Tag {
	case "mi", "mn", "mo", "mtext", "ms":
		return true
	}
	return false
}

// Is reports whether an attribute is set to "true". The translator spells
// stretchy "strechy" in places, so both are looked at.
func (n *Node) Is(attr string) bool {
	if n.Attr[attr] == "true" {
		return true
	}
	return attr == "stretchy" && n.Attr["strechy"] == "true"
}

// CSS returns a property of the element's inline style, such as text-align.
func (n *Node) CSS(property string) string {
	for _, decl := range strings.Split(n.Attr["style"], ";") {
		name, value, ok := strings.Cut(decl, ":")
		if ok && strings.TrimSpace(name) == property {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func find(n *Node, tag string) *Node {
	if n.Tag == tag {
		return n
	}
	for _, kid := range n.Kids {
		if found := find(kid, tag); found != nil {
			return found
		}
	}
	return nil
}

func text(n *Node) string {
	var b strings.Builder
	b.WriteString(n.Text)
	for _, kid := range n.Kids {
		b.WriteString(text(kid))
	}
	return b.String()
}

// Variant is how a letter of a formula is styled.
type Variant int

const (
	Plain Variant = iota
	Bold
	Italic
	BoldItalic
	Script       // calligraphic, as \mathcal
	Fraktur      // as \mathfrak
	DoubleStruck // blackboard bold, as \mathbb
	SansSerif
	Monospace
)

// Styled letters are characters of their own in Unicode, in runs of 26 (or
// 10 for digits) that start here.
var styledRuns = []struct {
	upper, lower, digit rune
	variant             Variant
}{
	{0x1D400, 0x1D41A, 0x1D7CE, Bold},
	{0x1D434, 0x1D44E, 0, Italic},
	{0x1D468, 0x1D482, 0, BoldItalic},
	{0x1D49C, 0x1D4B6, 0, Script},
	{0x1D4D0, 0x1D4EA, 0, Script}, // bold script
	{0x1D504, 0x1D51E, 0, Fraktur},
	{0x1D538, 0x1D552, 0x1D7D8, DoubleStruck},
	{0x1D56C, 0x1D586, 0, Fraktur}, // bold fraktur
	{0x1D5A0, 0x1D5BA, 0x1D7E2, SansSerif},
	{0x1D5D4, 0x1D5EE, 0x1D7EC, SansSerif}, // bold
	{0x1D608, 0x1D622, 0, SansSerif},       // italic
	{0x1D63C, 0x1D656, 0, SansSerif},       // bold italic
	{0x1D670, 0x1D68A, 0x1D7F6, Monospace},
}

// A few styled letters were in Unicode before the runs, and are still at
// their old places.
var styledSingles = map[rune]struct {
	base    rune
	variant Variant
}{
	'ℎ': {'h', Italic},
	'ℬ': {'B', Script}, 'ℰ': {'E', Script}, 'ℱ': {'F', Script}, 'ℋ': {'H', Script}, 'ℐ': {'I', Script},
	'ℒ': {'L', Script}, 'ℳ': {'M', Script}, 'ℛ': {'R', Script}, 'ℯ': {'e', Script}, 'ℊ': {'g', Script}, 'ℴ': {'o', Script},
	'ℭ': {'C', Fraktur}, 'ℌ': {'H', Fraktur}, 'ℑ': {'I', Fraktur}, 'ℜ': {'R', Fraktur}, 'ℨ': {'Z', Fraktur},
	'ℂ': {'C', DoubleStruck}, 'ℍ': {'H', DoubleStruck}, 'ℕ': {'N', DoubleStruck}, 'ℙ': {'P', DoubleStruck},
	'ℚ': {'Q', DoubleStruck}, 'ℝ': {'R', DoubleStruck}, 'ℤ': {'Z', DoubleStruck},
}

// Unstyle returns the plain letter or digit a styled one stands for, and its
// style. A character that is not a styled letter comes back as it is.
func Unstyle(c rune) (rune, Variant) {
	if s, ok := styledSingles[c]; ok {
		return s.base, s.variant
	}
	if c < 0x1D400 || c > 0x1D7FF {
		return c, Plain
	}
	if c >= 0x1D6FC && c <= 0x1D714 {
		return 'α' + (c - 0x1D6FC), Italic // lower-case Greek, as ItalicLetter makes it
	}
	for _, run := range styledRuns {
		switch {
		case c >= run.upper && c < run.upper+26:
			return 'A' + (c - run.upper), run.variant
		case c >= run.lower && c < run.lower+26:
			return 'a' + (c - run.lower), run.variant
		case run.digit != 0 && c >= run.digit && c < run.digit+10:
			return '0' + (c - run.digit), run.variant
		}
	}
	return c, Plain
}

// ItalicLetter returns the italic form of a letter, which is how a formula
// shows a variable: Latin letters and lower-case Greek. Other characters
// come back as they are.
func ItalicLetter(c rune) rune {
	switch {
	case c == 'h':
		return 'ℎ' // the one italic letter that is not in the run
	case c >= 'A' && c <= 'Z':
		return 0x1D434 + (c - 'A')
	case c >= 'a' && c <= 'z':
		return 0x1D44E + (c - 'a')
	case c >= 'α' && c <= 'ω':
		return 0x1D6FC + (c - 'α')
	}
	return c
}
