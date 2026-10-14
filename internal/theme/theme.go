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

// Theme is the complete description of a document's look. The key tags name
// the settings in a theme file.
type Theme struct {
	Name        string `key:"-"`
	Description string `key:"-"`

	Page     Page       `key:"page"`
	Fonts    Fonts      `key:"fonts"`
	Text     Text       `key:"text"`
	Heading  [6]Heading `key:"heading"` // H1 to H6, as heading.h1 ... heading.h6
	Link     Link       `key:"link"`
	Code     Code       `key:"code"`
	List     List       `key:"list"`
	Table    Table      `key:"table"`
	Quote    Quote      `key:"quote"`
	Rule     Rule       `key:"rule"`
	Alert    Alert      `key:"alert"`
	Box      Box        `key:"box"`
	Diagram  Diagram    `key:"diagram"`
	Footnote Footnote   `key:"footnote"`
	Caption  Caption    `key:"caption"`
	Colors   Colors     `key:"colors"`
	Header   Running    `key:"header"`
	Footer   Running    `key:"footer"`
	Control  Control    `key:"control"`
	Cover    Cover      `key:"cover"`
}

// Page is the paper and its margins.
type Page struct {
	Width        float64 `key:"width" doc:"paper width; usually set through size"`
	Height       float64 `key:"height" doc:"paper height; usually set through size"`
	MarginTop    float64 `key:"margin-top"`
	MarginRight  float64 `key:"margin-right"`
	MarginBottom float64 `key:"margin-bottom"`
	MarginLeft   float64 `key:"margin-left"`
}

// ContentWidth is the width between the left and right margins.
func (p Page) ContentWidth() float64 {
	return p.Width - p.MarginLeft - p.MarginRight
}

// Fonts names the typefaces. In PDF they are families the renderer can load:
// the built-in "sans", "serif" and "mono", or one defined under Families. In
// DOCX they are font names the reader's word processor looks up.
type Fonts struct {
	Body    string `key:"body" doc:"sans, serif, mono, or a family defined under [fonts.family.NAME]"`
	Heading string `key:"heading"`
	Code    string `key:"code"`

	// Families are fonts loaded from TTF files, by the name the settings
	// above refer to them.
	Families map[string]FontFamily `key:"-"`
}

// FontFamily is a typeface loaded from TTF files. Only Regular is required;
// a missing style falls back to it.
type FontFamily struct {
	Regular    string // paths to TTF files
	Bold       string
	Italic     string
	BoldItalic string
	Name       string // what word processors call the font, for DOCX
}

// Text is the body text.
type Text struct {
	Size             float64 `key:"size"`
	LineHeight       float64 `key:"line-height"`
	ParagraphSpacing float64 `key:"paragraph-spacing" doc:"space after a paragraph"`
	Align            string  `key:"align" doc:"left, or justify for text flush on both sides"`
	Hyphenate        string  `key:"hyphenate" doc:"auto (where text is justified), always or never"`
	Color            Color   `key:"color"`
	Muted            Color   `key:"muted" doc:"captions and labels"`
	Faint            Color   `key:"faint" doc:"struck-through text"`
}

// Values of Text.Align.
const (
	AlignLeft    = "left"
	AlignJustify = "justify"
)

// Values of Text.Hyphenate.
const (
	HyphenateAuto   = "auto"
	HyphenateAlways = "always"
	HyphenateNever  = "never"
)

// Hyphenated reports whether the words of running text (paragraphs, list
// items, quotes, callouts) are broken at the ends of lines.
func (t Text) Hyphenated() bool {
	return t.Hyphenate == HyphenateAlways || t.Hyphenate == HyphenateAuto && t.Justified()
}

// Justified reports whether body text is set flush on both sides.
func (t Text) Justified() bool {
	return t.Align == AlignJustify
}

// Heading is one heading level.
type Heading struct {
	Size        float64 `key:"size"`
	LineHeight  float64 `key:"line-height"`
	SpaceBefore float64 `key:"space-before"`
	SpaceAfter  float64 `key:"space-after"`
	Color       Color   `key:"color"`
}

// Link is a hyperlink.
type Link struct {
	Color Color `key:"color"`
}

// Code covers inline code and code blocks.
type Code struct {
	Color Color `key:"color" doc:"inline code"`

	BlockSize       float64 `key:"block-size" doc:"font size in code blocks"`
	BlockLineHeight float64 `key:"block-line-height"`
	Padding         float64 `key:"padding"`
	SpaceAfter      float64 `key:"space-after"`
	BlockColor      Color   `key:"block-color" doc:"text in code blocks"`
	Background      Color   `key:"background"`
	Border          Color   `key:"border"`

	Syntax Syntax `key:"syntax"`
}

// Syntax colors the parts of the code in a code block that names its
// language. Everything else in the block keeps the block's text color.
type Syntax struct {
	Highlight bool  `key:"highlight" doc:"false leaves code in one color"`
	Keyword   Color `key:"keyword" doc:"also tag names in markup"`
	String    Color `key:"string"`
	Comment   Color `key:"comment"`
	Number    Color `key:"number" doc:"and other literal values"`
	Function  Color `key:"function" doc:"function names, and attribute names in markup"`
	Type      Color `key:"type" doc:"type names and built-in names"`
}

// List is bulleted, numbered and task lists.
type List struct {
	Indent     float64 `key:"indent" doc:"per nesting level"`
	SpaceAfter float64 `key:"space-after"`
}

// Table is a table and its cells.
type Table struct {
	CellPadding float64 `key:"cell-padding"`
	LineHeight  float64 `key:"line-height"`
	SpaceAfter  float64 `key:"space-after"`

	Border           Color `key:"border"`
	HeaderBackground Color `key:"header-background"`
	RowBackground    Color `key:"row-background"`
	StripeBackground Color `key:"stripe-background" doc:"every other row"`
}

// Quote is a block quote.
type Quote struct {
	Indent     float64 `key:"indent"`
	BarWidth   float64 `key:"bar-width"`
	SpaceAfter float64 `key:"space-after" doc:"after each quoted paragraph"`
	Bar        Color   `key:"bar" doc:"the line on the left"`
	Italic     bool    `key:"italic"`
}

// Rule is a horizontal rule.
type Rule struct {
	Width float64 `key:"width"`
	Space float64 `key:"space" doc:"above and below"`
	Color Color   `key:"color"`
}

// AlertColors are the colors of one kind of callout.
type AlertColors struct {
	Background Color `key:"background"`
	Border     Color `key:"border" doc:"the bar on the left, and the title"`
}

// Alert is a callout box (note, tip, warning, ...).
type Alert struct {
	Padding    float64 `key:"padding"`
	BarWidth   float64 `key:"bar-width"`
	SpaceAfter float64 `key:"space-after"`

	Note      AlertColors `key:"note"`
	Tip       AlertColors `key:"tip"`
	Important AlertColors `key:"important"`
	Caution   AlertColors `key:"caution"`
	Warning   AlertColors `key:"warning"`
}

// Box is the panel that shows a document's metadata (front matter).
type Box struct {
	Background Color `key:"background"`
	Border     Color `key:"border"`
}

// Diagram is the placeholder panel for diagram source.
type Diagram struct {
	Background Color `key:"background"`
	Border     Color `key:"border"`
	Text       Color `key:"text"`
}

// Footnote is the footnote section at the end.
type Footnote struct {
	Size       float64 `key:"size"`
	LineHeight float64 `key:"line-height"`
	Rule       Color   `key:"rule" doc:"the line above the section"`
}

// Caption is the text under an image.
type Caption struct {
	Size float64 `key:"size"`
}

// Running is a header or footer: a line of text repeated on every page, in
// the top or bottom margin. Each of the three positions may hold text with
// the placeholders {title}, {author}, {date}, {version} and {classification}
// (from the document's front matter; the title falls back to the first
// heading), {page} and {pages}. With all three empty there is no header or
// footer.
type Running struct {
	Left   string  `key:"left" doc:"text with {title} {author} {date} {version} {classification} {page} {pages}"`
	Center string  `key:"center"`
	Right  string  `key:"right"`
	Size   float64 `key:"size"`
	Color  Color   `key:"color"`
	Rule   bool    `key:"rule" doc:"a line between it and the page's text"`
}

// Control is the document control block, the table the front matter is
// shown in.
type Control struct {
	Labels ControlLabels `key:"labels"`
}

// ControlLabels name the fields of the control block. An empty label is
// given in the language of the document.
type ControlLabels struct {
	Title          string `key:"title" doc:"empty: in the language of the document"`
	Version        string `key:"version"`
	Status         string `key:"status"`
	Author         string `key:"author"`
	Owner          string `key:"owner"`
	ApprovedBy     string `key:"approved-by"`
	Date           string `key:"date"`
	ReviewDate     string `key:"review-date"`
	Classification string `key:"classification"`
}

// Cover is the cover page: the title and subtitle of a document and its
// control block on a page of their own, ahead of the text.
type Cover struct {
	Enabled       bool    `key:"enabled" doc:"every document with front matter gets a cover page"`
	TitleSize     float64 `key:"title-size"`
	TitleColor    Color   `key:"title-color"`
	SubtitleSize  float64 `key:"subtitle-size"`
	SubtitleColor Color   `key:"subtitle-color"`
	Top           float64 `key:"top" doc:"how far below the top margin the title starts"`
}

// Empty reports whether there is nothing to show.
func (r Running) Empty() bool {
	return r.Left == "" && r.Center == "" && r.Right == ""
}

// Colors are the accent colors of inline elements.
type Colors struct {
	Math      Color `key:"math" doc:"the source of a formula that could not be typeset"`
	Highlight Color `key:"highlight" doc:"background of ==marked== text"`
	Inserted  Color `key:"inserted" doc:"additions in inline diffs"`
	Deleted   Color `key:"deleted" doc:"removals in inline diffs"`
}

// Set is a theme as each output format uses it. The two are separate because
// the formats do not share units of layout: PDF is drawn by markout, DOCX is
// laid out by the word processor that opens it.
type Set struct {
	PDF  Theme
	DOCX Theme
}
