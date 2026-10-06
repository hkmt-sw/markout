// Package theme describes how a converted document looks: page, fonts, sizes,
// spacing and colors. The renderers read every such value from a Theme.
//
// Lengths are in points (1/72 inch).
package theme

import "fmt"

// Color is an RGB color.
type Color struct {
	R, G, B uint8
}

// Common colors.
var (
	Black = Color{}
	White = Color{R: 255, G: 255, B: 255}
)

// Hex parses "RRGGBB" or "#RRGGBB". It panics on anything else and is meant
// for colors written in the source; user input goes through ParseColor.
func Hex(s string) Color {
	c, err := ParseColor(s)
	if err != nil {
		panic(err)
	}
	return c
}

// ParseColor reads "#RRGGBB", "RRGGBB", "#RGB" or "RGB".
func ParseColor(s string) (Color, error) {
	h := s
	if len(h) > 0 && h[0] == '#' {
		h = h[1:]
	}
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var c Color
	if len(h) != 6 {
		return c, fmt.Errorf("color %q is not of the form #RRGGBB", s)
	}
	if _, err := fmt.Sscanf(h, "%02x%02x%02x", &c.R, &c.G, &c.B); err != nil {
		return c, fmt.Errorf("color %q is not of the form #RRGGBB", s)
	}
	return c, nil
}

// String returns the color as "#RRGGBB".
func (c Color) String() string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// Hex returns the color as "RRGGBB", the form DOCX uses.
func (c Color) Hex() string {
	return fmt.Sprintf("%02X%02X%02X", c.R, c.G, c.B)
}

// Theme is the complete description of a document's look.
type Theme struct {
	Name        string
	Description string

	Page     Page
	Fonts    Fonts
	Text     Text
	Heading  [6]Heading // H1 to H6
	Link     Color
	Code     Code
	List     List
	Table    Table
	Quote    Quote
	Rule     Rule
	Alert    Alert
	Box      Box
	Diagram  Diagram
	Footnote Footnote
	Caption  Caption

	Math      Color // formulas
	Highlight Color // background of ==marked== text
	Inserted  Color // additions in inline diffs
	Deleted   Color // removals in inline diffs
}

// Page is the paper and its margins.
type Page struct {
	Width, Height                                    float64
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
}

// ContentWidth is the width between the left and right margins.
func (p Page) ContentWidth() float64 {
	return p.Width - p.MarginLeft - p.MarginRight
}

// Fonts names the typefaces. In PDF they are families the renderer has
// loaded ("sans", "mono"); in DOCX they are font names the reader's word
// processor looks up.
type Fonts struct {
	Body    string
	Heading string
	Code    string
}

// Text is the body text.
type Text struct {
	Size             float64
	LineHeight       float64
	ParagraphSpacing float64 // space after a paragraph
	Color            Color
	Muted            Color // captions, labels
	Faint            Color // struck-through text
}

// Heading is one heading level.
type Heading struct {
	Size        float64
	LineHeight  float64
	SpaceBefore float64
	SpaceAfter  float64
	Color       Color
}

// Code covers inline code and code blocks.
type Code struct {
	Color Color // inline code

	BlockSize       float64
	BlockLineHeight float64
	Padding         float64
	SpaceAfter      float64
	BlockColor      Color
	Background      Color
	Border          Color
}

// List is bulleted, numbered and task lists.
type List struct {
	Indent     float64 // per nesting level
	SpaceAfter float64
}

// Table is a table and its cells.
type Table struct {
	CellPadding float64
	LineHeight  float64
	SpaceAfter  float64

	Border           Color
	HeaderBackground Color
	RowBackground    Color
	StripeBackground Color // every other row
}

// Quote is a block quote.
type Quote struct {
	Indent     float64
	BarWidth   float64
	SpaceAfter float64 // after each quoted paragraph
	Bar        Color
	Italic     bool
}

// Rule is a horizontal rule.
type Rule struct {
	Width float64
	Space float64 // above and below
	Color Color
}

// AlertColors are the colors of one kind of callout.
type AlertColors struct {
	Background Color
	Border     Color // the bar on the left, and the title
}

// Alert is a callout box (note, tip, warning, ...).
type Alert struct {
	Padding    float64
	BarWidth   float64
	SpaceAfter float64

	Note, Tip, Important, Caution, Warning AlertColors
}

// Box is the panel that shows a document's metadata (front matter).
type Box struct {
	Background Color
	Border     Color
}

// Diagram is the placeholder panel for diagram source.
type Diagram struct {
	Background Color
	Border     Color
	Text       Color
}

// Footnote is the footnote section at the end.
type Footnote struct {
	Size       float64
	LineHeight float64
	Rule       Color // the line above the section
}

// Caption is the text under an image.
type Caption struct {
	Size float64
}

// Set is a theme as each output format uses it. The two are separate because
// the formats do not share units of layout: PDF is drawn by markout, DOCX is
// laid out by the word processor that opens it.
type Set struct {
	PDF  Theme
	DOCX Theme
}
