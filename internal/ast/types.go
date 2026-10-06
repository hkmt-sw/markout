package ast

// Document represents a complete markdown document
type Document struct {
	Elements []Element
}

// Element is the interface for all document elements
type Element interface {
	elementMarker()
}

// Heading represents a heading (H1-H6)
type Heading struct {
	Level int // 1-6
	Runs  []InlineRun
}

func (Heading) elementMarker() {}

// Paragraph represents a paragraph of text
type Paragraph struct {
	Runs  []InlineRun
	Align Alignment
}

func (Paragraph) elementMarker() {}

// Alignment represents text alignment
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
	AlignJustify
)

// InlineRun represents a run of inline text with formatting
type InlineRun struct {
	Text          string
	Bold          bool
	Italic        bool
	Code          bool
	Strikethrough bool
	Link          string // URL if this is a hyperlink, empty otherwise
	LinkTitle     string // Optional title for links
	FootnoteRef   string // Footnote reference ID if this is a footnote ref
	FootnoteIndex int    // Footnote display index (1, 2, 3...)
	Math          bool   // true if this is an inline math expression
	ColorChip     string // hex color value for color chip display (e.g. "#FF0000")
	Underline     bool   // <u>, <ins>, ^^text^^
	Highlight     bool   // ==text==, <mark>
	Superscript   bool   // ^text^, <sup>
	Subscript     bool   // ~text~, <sub>
	Inserted      bool   // inline diff / CriticMarkup addition
	Deleted       bool   // inline diff / CriticMarkup deletion
}

// Image represents an inline or block image
type Image struct {
	Alt   string // Alt text
	URL   string // Image URL or path
	Title string // Optional title
	Width int    // Optional display width hint in pixels (0 = unset), e.g. ![alt|640](url)
}

func (Image) elementMarker() {}

// FootnoteRef represents a footnote reference in text [^1]
type FootnoteRef struct {
	ID    string // Footnote identifier
	Index int    // Display index (1, 2, 3...)
}

// FootnoteDefinition represents a footnote definition
type FootnoteDefinition struct {
	ID       string    // Footnote identifier
	Index    int       // Display index
	Elements []Element // Content of the footnote
}

func (FootnoteDefinition) elementMarker() {}

// List represents an ordered or unordered list
type List struct {
	Ordered bool
	Items   []ListItem
}

func (List) elementMarker() {}

// ListItem represents a single item in a list
type ListItem struct {
	Runs     []InlineRun
	Level    int // nesting level, 0-based
	Children []ListItem
	IsTask   bool // true if this is a task list item (checkbox)
	Checked  bool // true if checkbox is checked [x], false if unchecked [ ]
}

// CodeBlock represents a fenced code block
type CodeBlock struct {
	Language string
	Code     string
}

func (CodeBlock) elementMarker() {}

// Table represents a table
type Table struct {
	Header     TableRow
	Rows       []TableRow
	Alignments []Alignment // column alignments
}

func (Table) elementMarker() {}

// TableRow represents a row in a table
type TableRow struct {
	Cells []TableCell
}

// TableCell represents a cell in a table
type TableCell struct {
	Runs  []InlineRun
	Align Alignment
}

// Blockquote represents a blockquote
type Blockquote struct {
	Elements []Element
}

func (Blockquote) elementMarker() {}

// HorizontalRule represents a horizontal rule (thematic break)
type HorizontalRule struct{}

func (HorizontalRule) elementMarker() {}

// AlertType represents the type of alert/callout
type AlertType int

const (
	AlertNote AlertType = iota
	AlertTip
	AlertImportant
	AlertCaution
	AlertWarning
)

// String returns the display name for an alert type
func (t AlertType) String() string {
	switch t {
	case AlertNote:
		return "NOTE"
	case AlertTip:
		return "TIP"
	case AlertImportant:
		return "IMPORTANT"
	case AlertCaution:
		return "CAUTION"
	case AlertWarning:
		return "WARNING"
	default:
		return "NOTE"
	}
}

// Alert represents a callout/alert block (e.g. > [!NOTE])
type Alert struct {
	Type     AlertType
	Title    string // Custom title or default from type
	Elements []Element
}

func (Alert) elementMarker() {}

// MermaidDiagram represents a mermaid diagram block
type MermaidDiagram struct {
	Source string
}

func (MermaidDiagram) elementMarker() {}

// MathBlock represents a block-level math expression ($$...$$)
type MathBlock struct {
	Expression string
}

func (MathBlock) elementMarker() {}

// DescriptionList represents a definition/description list
type DescriptionList struct {
	Items []DescriptionItem
}

func (DescriptionList) elementMarker() {}

// DescriptionItem represents a single term with its definitions
type DescriptionItem struct {
	Term        []InlineRun
	Definitions [][]InlineRun // Each definition is a list of runs
}

// TableOfContents represents a generated table of contents
type TableOfContents struct {
	Items []TOCItem
}

func (TableOfContents) elementMarker() {}

// TOCItem represents an entry in the table of contents
type TOCItem struct {
	Level int
	Title string
	ID    string
}

// FrontMatter represents YAML front matter metadata
type FrontMatter struct {
	Title  string
	Author string
	Date   string
	Raw    map[string]any
}

func (FrontMatter) elementMarker() {}

// NewDocument creates a new empty document
func NewDocument() *Document {
	return &Document{
		Elements: make([]Element, 0),
	}
}

// AppendElement adds an element to the document
func (d *Document) AppendElement(e Element) {
	d.Elements = append(d.Elements, e)
}

// NewInlineRun creates a plain text run
func NewInlineRun(text string) InlineRun {
	return InlineRun{Text: text}
}

// NewBoldRun creates a bold text run
func NewBoldRun(text string) InlineRun {
	return InlineRun{Text: text, Bold: true}
}

// NewItalicRun creates an italic text run
func NewItalicRun(text string) InlineRun {
	return InlineRun{Text: text, Italic: true}
}

// NewCodeRun creates an inline code run
func NewCodeRun(text string) InlineRun {
	return InlineRun{Text: text, Code: true}
}

// NewLinkRun creates a hyperlink run
func NewLinkRun(text, url string) InlineRun {
	return InlineRun{Text: text, Link: url}
}

// NewStrikethroughRun creates a strikethrough text run
func NewStrikethroughRun(text string) InlineRun {
	return InlineRun{Text: text, Strikethrough: true}
}

// NewImage creates an image element
func NewImage(alt, url, title string) Image {
	return Image{Alt: alt, URL: url, Title: title}
}

// NewParagraph creates a paragraph from runs
func NewParagraph(runs ...InlineRun) Paragraph {
	return Paragraph{Runs: runs, Align: AlignLeft}
}

// NewHeading creates a heading from runs
func NewHeading(level int, runs ...InlineRun) Heading {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return Heading{Level: level, Runs: runs}
}

// NewCodeBlock creates a code block
func NewCodeBlock(language, code string) CodeBlock {
	return CodeBlock{Language: language, Code: code}
}

// NewList creates a list
func NewList(ordered bool, items ...ListItem) List {
	return List{Ordered: ordered, Items: items}
}

// NewListItem creates a list item
func NewListItem(level int, runs ...InlineRun) ListItem {
	return ListItem{Level: level, Runs: runs}
}

// NewTable creates a table
func NewTable(header TableRow, rows []TableRow, alignments []Alignment) Table {
	return Table{Header: header, Rows: rows, Alignments: alignments}
}

// NewTableRow creates a table row
func NewTableRow(cells ...TableCell) TableRow {
	return TableRow{Cells: cells}
}

// NewTableCell creates a table cell
func NewTableCell(runs ...InlineRun) TableCell {
	return TableCell{Runs: runs, Align: AlignLeft}
}

// NewBlockquote creates a blockquote
func NewBlockquote(elements ...Element) Blockquote {
	return Blockquote{Elements: elements}
}
