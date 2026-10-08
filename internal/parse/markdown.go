package parse

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	mathjax "github.com/litao91/goldmark-mathjax"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	emojiast "github.com/yuin/goldmark-emoji/ast"
	gmast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.abhg.dev/goldmark/frontmatter"
	"go.abhg.dev/goldmark/toc"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
)

// color chip pattern: hex color code like #FF0000, #abc, #12345678
var colorChipPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{3,8}$`)

// alert pattern: [!type] at the start of a blockquote, with Obsidian's
// optional fold marker
var alertPattern = regexp.MustCompile(`^\[!([A-Za-z][\w:-]*)\]([+-]?)[ \t]*`)

// Parser holds the goldmark parser instance
type Parser struct {
	md       goldmark.Markdown
	features flavor.Features
	baseDir  string
}

// NewParser creates a markdown parser for the default flavor
func NewParser() *Parser {
	return NewParserFor(flavor.Default(), "")
}

// NewParserFor creates a markdown parser that interprets the given flavor.
// baseDir resolves files referenced by the document (e.g. GitLab includes).
func NewParserFor(fl flavor.Flavor, baseDir string) *Parser {
	f := fl.Features
	var exts []goldmark.Extender
	add := func(on bool, e goldmark.Extender) {
		if on {
			exts = append(exts, e)
		}
	}
	add(f.Tables, extension.Table)
	add(f.TaskLists, extension.TaskList)
	add(f.Strikethrough, extension.Strikethrough)
	add(f.Linkify, extension.Linkify)
	add(f.Footnotes, extension.Footnote)
	add(f.DefinitionLists, extension.DefinitionList)
	add(f.Typographer, extension.Typographer)
	add(f.MathDollar, mathjax.MathJax)
	add(f.FrontMatter, &frontmatter.Extender{})
	add(f.Emoji, emoji.Emoji)

	parserOpts := []parser.Option{parser.WithAutoHeadingID()}
	if f.HeadingAttributes {
		parserOpts = append(parserOpts, parser.WithAttribute())
	}

	md := goldmark.New(
		goldmark.WithExtensions(exts...),
		goldmark.WithParserOptions(parserOpts...),
	)
	return &Parser{md: md, features: f, baseDir: baseDir}
}

// Parse converts markdown bytes to our internal AST
func (p *Parser) Parse(source []byte) (*ast.Document, error) {
	pre := newPreprocessor(p.features, p.baseDir)
	source = pre.run(source)

	ctx := parser.NewContext()
	reader := text.NewReader(source)
	gmDoc := p.md.Parser().Parse(reader, parser.WithContext(ctx))

	// Extract TOC tree from headings
	tocTree, _ := toc.Inspect(gmDoc, source)

	doc := ast.NewDocument()
	doc.Warnings = pre.left.warnings()

	// Extract and prepend front matter
	if pre.frontMatter != nil {
		doc.AppendElement(*pre.frontMatter)
	} else if fmData := frontmatter.Get(ctx); fmData != nil {
		if fm := extractFrontMatter(fmData); fm != nil {
			doc.AppendElement(*fm)
		}
	}

	walker := &astWalker{
		f:         p.features,
		source:    source,
		doc:       doc,
		footnotes: make(map[string]*ast.FootnoteDefinition),
		tocTree:   tocTree,
	}

	err := gmast.Walk(gmDoc, walker.walk)
	if err != nil {
		return nil, err
	}

	// Append collected footnote definitions at the end
	walker.appendFootnotes()

	return doc, nil
}

// Parse is a convenience function that creates a parser and parses the source
func Parse(source []byte) (*ast.Document, error) {
	p := NewParser()
	return p.Parse(source)
}

// ParseFlavor parses the source the way the given flavor reads it
func ParseFlavor(source []byte, fl flavor.Flavor, baseDir string) (*ast.Document, error) {
	return NewParserFor(fl, baseDir).Parse(source)
}

// extractFrontMatter decodes front matter data into our AST type
func extractFrontMatter(data *frontmatter.Data) *ast.FrontMatter {
	var raw map[string]any
	if err := data.Decode(&raw); err != nil {
		return nil
	}
	return frontMatterFromMap(raw)
}

// frontMatterFromMap builds the front matter element from decoded metadata
func frontMatterFromMap(raw map[string]any) *ast.FrontMatter {
	if len(raw) == 0 {
		return nil
	}
	fm := &ast.FrontMatter{Raw: raw}
	if title, ok := raw["title"].(string); ok {
		fm.Title = title
	}
	if author, ok := raw["author"].(string); ok {
		fm.Author = author
	}
	// An unquoted date is decoded as a time; show it the way it was written
	switch date := raw["date"].(type) {
	case string:
		fm.Date = date
	case time.Time:
		if h, m, sec := date.Clock(); h == 0 && m == 0 && sec == 0 {
			fm.Date = date.Format("2006-01-02")
		} else {
			fm.Date = date.Format("2006-01-02 15:04")
		}
	case nil:
	default:
		fm.Date = fmt.Sprintf("%v", date)
	}
	return fm
}

// flattenTOCItems converts the toc tree into a flat list with levels
func flattenTOCItems(items toc.Items, level int) []ast.TOCItem {
	var result []ast.TOCItem
	for _, item := range items {
		if item.Title != nil {
			result = append(result, ast.TOCItem{
				Level: level,
				Title: string(item.Title),
				ID:    string(item.ID),
			})
		}
		if len(item.Items) > 0 {
			result = append(result, flattenTOCItems(item.Items, level+1)...)
		}
	}
	return result
}

// astWalker walks the goldmark AST and builds our internal AST
type astWalker struct {
	f               flavor.Features // Active syntax extensions
	html            htmlState       // Open inline HTML tags in the current block
	source          []byte
	doc             *ast.Document
	footnoteCounter int                                // Counter for footnote indices
	footnotes       map[string]*ast.FootnoteDefinition // Collected footnote definitions
	tocTree         *toc.TOC                           // Pre-extracted TOC tree
}

func (w *astWalker) walk(node gmast.Node, entering bool) (gmast.WalkStatus, error) {
	// Only process on entering, except for some special cases
	if !entering {
		return gmast.WalkContinue, nil
	}

	switch n := node.(type) {
	case *gmast.Document:
		// Process children
		return gmast.WalkContinue, nil

	case *gmast.Heading:
		heading := w.convertHeading(n)
		w.doc.AppendElement(heading)
		return gmast.WalkSkipChildren, nil

	case *gmast.Paragraph:
		// Skip paragraphs inside list items, blockquotes, or definition descriptions
		if isInsideListItem(n) || isInsideBlockquote(n) {
			return gmast.WalkSkipChildren, nil
		}
		// Check for a table of contents marker
		if w.isTOCMarker(n) {
			w.doc.AppendElement(w.generateTOC())
			return gmast.WalkSkipChildren, nil
		}
		// Images live inside paragraphs in the goldmark AST. Emit them as
		// block-level Image elements (interleaved with any surrounding text)
		// so the renderers can embed them instead of dropping them.
		if w.paragraphHasImage(n) {
			w.appendParagraphWithImages(n)
			return gmast.WalkSkipChildren, nil
		}
		para := w.convertParagraph(n)
		w.doc.AppendElement(para)
		return gmast.WalkSkipChildren, nil

	case *gmast.List:
		// Only process top-level lists
		if isInsideListItem(n) {
			return gmast.WalkSkipChildren, nil
		}
		list := w.convertList(n, 0)
		w.doc.AppendElement(list)
		return gmast.WalkSkipChildren, nil

	case *gmast.FencedCodeBlock:
		elem := w.convertFencedCodeBlock(n)
		w.doc.AppendElement(elem)
		return gmast.WalkSkipChildren, nil

	case *gmast.CodeBlock:
		codeBlock := w.convertCodeBlock(n)
		w.doc.AppendElement(codeBlock)
		return gmast.WalkSkipChildren, nil

	case *gmast.Blockquote:
		// Only process top-level blockquotes
		if isInsideBlockquote(n) {
			return gmast.WalkSkipChildren, nil
		}
		elem := w.convertBlockquote(n)
		w.doc.AppendElement(elem)
		return gmast.WalkSkipChildren, nil

	case *gmast.ThematicBreak:
		w.doc.AppendElement(ast.HorizontalRule{})
		return gmast.WalkSkipChildren, nil

	case *gmast.HTMLBlock:
		// HTML nested in lists and quotes has no place in their run lists
		if !isInsideListItem(n) && !isInsideBlockquote(n) {
			for _, elem := range w.convertHTMLBlock(n) {
				w.doc.AppendElement(elem)
			}
		}
		return gmast.WalkSkipChildren, nil

	case *extTable:
		table := w.convertTable(n)
		w.doc.AppendElement(table)
		return gmast.WalkSkipChildren, nil

	case *gmast.Image:
		image := w.convertImage(n)
		w.doc.AppendElement(image)
		return gmast.WalkSkipChildren, nil

	case *extFootnoteList:
		// Process footnote list - collect all footnote definitions
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if fn, ok := child.(*extFootnote); ok {
				w.convertFootnoteDefinition(fn)
			}
		}
		return gmast.WalkSkipChildren, nil

	case *extMathBlock:
		mathBlock := w.convertMathBlock(n)
		w.doc.AppendElement(mathBlock)
		return gmast.WalkSkipChildren, nil

	case *extDefinitionList:
		dl := w.convertDefinitionList(n)
		w.doc.AppendElement(dl)
		return gmast.WalkSkipChildren, nil
	}

	return gmast.WalkContinue, nil
}

func (w *astWalker) convertHeading(n *gmast.Heading) ast.Heading {
	runs := w.extractInlineRuns(n)
	heading := ast.NewHeading(n.Level, runs...)
	if id, ok := n.AttributeString("id"); ok {
		if b, ok := id.([]byte); ok {
			heading.ID = string(b)
		}
	}
	return heading
}

func (w *astWalker) convertParagraph(n *gmast.Paragraph) ast.Paragraph {
	runs := w.extractInlineRuns(n)
	return ast.NewParagraph(runs...)
}

func (w *astWalker) convertList(n *gmast.List, level int) ast.List {
	ordered := n.IsOrdered()
	var items []ast.ListItem

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if listItem, ok := child.(*gmast.ListItem); ok {
			item := w.convertListItem(listItem, level)
			items = append(items, item)
		}
	}

	return ast.NewList(ordered, items...)
}

func (w *astWalker) convertListItem(n *gmast.ListItem, level int) ast.ListItem {
	var runs []ast.InlineRun
	var blocks []ast.Element
	var children []ast.ListItem
	var childrenOrdered bool
	var isTask bool
	var checked bool
	first := true // the item's own text has not been seen yet

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *gmast.TextBlock, *gmast.Paragraph:
			// Check for TaskCheckBox at the beginning
			taskChecked, hasTask := w.extractTaskCheckbox(c)
			if hasTask {
				isTask = true
				checked = taskChecked
			}
			// The first paragraph is the item's text. Further paragraphs,
			// and images, which are blocks of their own, follow under it.
			var elems []ast.Element
			if paragraphHasImageNode(c) {
				elems = w.splitAtImages(c)
			} else if r := w.extractInlineRuns(c); len(r) > 0 {
				elems = []ast.Element{ast.NewParagraph(r...)}
			}
			if first && len(blocks) == 0 && len(elems) > 0 {
				if p, ok := elems[0].(ast.Paragraph); ok {
					runs, elems = p.Runs, elems[1:]
				}
			}
			first = false
			blocks = append(blocks, elems...)
		case *gmast.List:
			// Nested list
			nestedList := w.convertList(c, level+1)
			if len(children) == 0 {
				childrenOrdered = nestedList.Ordered
			}
			for _, item := range nestedList.Items {
				children = append(children, item)
			}
		default:
			blocks = append(blocks, w.convertNestedBlock(child)...)
		}
	}

	item := ast.NewListItem(level, runs...)
	item.Blocks = blocks
	item.Children = children
	item.ChildrenOrdered = childrenOrdered
	item.IsTask = isTask
	item.Checked = checked
	return item
}

// convertNestedBlock converts a block found inside a list item, where the
// document walk does not go.
func (w *astWalker) convertNestedBlock(node gmast.Node) []ast.Element {
	switch c := node.(type) {
	case *gmast.FencedCodeBlock:
		return []ast.Element{w.convertFencedCodeBlock(c)}
	case *gmast.CodeBlock:
		return []ast.Element{w.convertCodeBlock(c)}
	case *gmast.Blockquote:
		return []ast.Element{w.convertBlockquote(c)}
	case *extTable:
		return []ast.Element{w.convertTable(c)}
	case *gmast.ThematicBreak:
		return []ast.Element{ast.HorizontalRule{}}
	case *gmast.Heading:
		return []ast.Element{w.convertHeading(c)}
	case *gmast.HTMLBlock:
		return w.convertHTMLBlock(c)
	case *extMathBlock:
		return []ast.Element{w.convertMathBlock(c)}
	case *extDefinitionList:
		return []ast.Element{w.convertDefinitionList(c)}
	}
	return nil
}

// extractTaskCheckbox checks if a node starts with a TaskCheckBox and returns its state
func (w *astWalker) extractTaskCheckbox(node gmast.Node) (checked bool, isTask bool) {
	firstChild := node.FirstChild()
	if firstChild == nil {
		return false, false
	}

	if checkbox, ok := firstChild.(*extTaskCheckBox); ok {
		return checkbox.IsChecked, true
	}

	return false, false
}

// convertFencedCodeBlock converts a fenced code block, detecting mermaid and math blocks
func (w *astWalker) convertFencedCodeBlock(n *gmast.FencedCodeBlock) ast.Element {
	language := string(n.Language(w.source))
	var code bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		code.Write(line.Value(w.source))
	}
	codeStr := code.String()

	switch {
	case language == internalMermaidLang, language == "mermaid" && w.f.MermaidFence:
		return ast.MermaidDiagram{Source: codeStr}

	case language == internalMathLang,
		language == "math" && w.f.MathFence,
		language == "latex" && w.f.MathLatexFence:
		return ast.MathBlock{Expression: strings.TrimSpace(codeStr)}

	case language == "json:table" && w.f.JSONTables:
		if table, ok := jsonTable(codeStr); ok {
			return table
		}
	}

	return ast.NewCodeBlock(language, codeStr)
}

func (w *astWalker) convertCodeBlock(n *gmast.CodeBlock) ast.CodeBlock {
	var code bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		code.Write(line.Value(w.source))
	}
	return ast.NewCodeBlock("", code.String())
}

// imageSizeHintPattern matches a width hint appended to the alt text: the
// Obsidian form "Diagram|640", or the "Diagram¦640" / "Diagram¦50%" the
// preprocessor writes for the other flavors' size syntaxes. Only the width is
// captured.
var imageSizeHintPattern = regexp.MustCompile(`^(.*)[|¦](\d+)(%?)(?:x\d+)?$`)

// percentWidthBasePx is the content width a percentage image width refers to.
const percentWidthBasePx = 600

func (w *astWalker) convertImage(n *gmast.Image) ast.Image {
	alt := extractImageAlt(n, w.source)
	url := string(n.Destination)
	title := string(n.Title)

	width := 0
	if m := imageSizeHintPattern.FindStringSubmatch(alt); m != nil {
		// strconv is intentionally avoided elsewhere in this package; parse manually.
		n := 0
		for _, c := range m[2] {
			n = n*10 + int(c-'0')
		}
		if m[3] == "%" {
			n = n * percentWidthBasePx / 100
		}
		width = n
		alt = strings.TrimRight(m[1], " ")
	}

	img := ast.NewImage(alt, url, title)
	img.Width = width
	return img
}

// paragraphHasImage reports whether a paragraph contains an image anywhere in
// its inline descendants (e.g. a bare image, or a linked image [![](img)](url)).
func paragraphHasImageNode(node gmast.Node) bool {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if _, ok := child.(*gmast.Image); ok {
			return true
		}
		if paragraphHasImageNode(child) {
			return true
		}
	}
	return false
}

func (w *astWalker) paragraphHasImage(n *gmast.Paragraph) bool {
	return paragraphHasImageNode(n)
}

// firstImageDescendant returns the first image found within node's subtree, or
// nil. Used to unwrap linked/emphasized images such as [![alt](img)](url).
func firstImageDescendant(node gmast.Node) *gmast.Image {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if img, ok := child.(*gmast.Image); ok {
			return img
		}
		if img := firstImageDescendant(child); img != nil {
			return img
		}
	}
	return nil
}

// appendParagraphWithImages splits a paragraph that contains one or more images
// into a sequence of Paragraph and Image block elements, preserving order. Text
// runs that surround the images are grouped into their own paragraphs.
func (w *astWalker) appendParagraphWithImages(n *gmast.Paragraph) {
	for _, elem := range w.splitAtImages(n) {
		w.doc.AppendElement(elem)
	}
}

// splitAtImages converts a paragraph or text block that has images in it into
// the paragraphs of text around them and the images between.
func (w *astWalker) splitAtImages(n gmast.Node) []ast.Element {
	var out []ast.Element
	var pending []ast.InlineRun
	flush := func() {
		if len(pending) > 0 {
			out = append(out, ast.NewParagraph(pending...))
			pending = nil
		}
	}

	w.html = htmlState{}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if _, ok := child.(*extTaskCheckBox); ok {
			continue
		}
		if img, ok := child.(*gmast.Image); ok {
			flush()
			out = append(out, w.convertImage(img))
			continue
		}
		// A link (or other wrapper) whose subtree is essentially just an image,
		// e.g. [![alt](img)](url): emit the image as a block element.
		if img := firstImageDescendant(child); img != nil {
			flush()
			out = append(out, w.convertImage(img))
			continue
		}
		pending = append(pending, w.extractInlineRunsFromNode(child, false, false, false, false, "")...)
	}
	flush()
	return out
}

// extractImageAlt extracts alt text from image children
func extractImageAlt(n *gmast.Image, source []byte) string {
	var text string
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*gmast.Text); ok {
			text += string(t.Segment.Value(source))
		}
	}
	return text
}

func (w *astWalker) convertFootnoteDefinition(n *extFootnote) {
	w.footnoteCounter++
	id := string(n.Ref)

	// A footnote is shown as paragraphs of text, so whatever else it holds
	// (a list, a quote, code) is reduced to that.
	var elements []ast.Element
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		elements = append(elements, w.flattenBlock(child)...)
	}

	def := &ast.FootnoteDefinition{
		ID:       id,
		Index:    n.Index,
		Elements: elements,
	}
	w.footnotes[id] = def
}

func (w *astWalker) appendFootnotes() {
	if len(w.footnotes) == 0 {
		return
	}

	// Sort footnotes by index and append to document
	ordered := make([]*ast.FootnoteDefinition, 0, len(w.footnotes))
	for _, fn := range w.footnotes {
		ordered = append(ordered, fn)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
	for _, fn := range ordered {
		w.doc.AppendElement(*fn)
	}
}

// convertBlockquote converts a blockquote, detecting alert/callout patterns
func (w *astWalker) convertBlockquote(n *gmast.Blockquote) ast.Element {
	// Check for alert pattern [!TYPE] in first paragraph
	if alert := w.tryConvertAlert(n); alert != nil {
		return *alert
	}

	var elements []ast.Element

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *gmast.Paragraph:
			para := w.convertParagraph(c)
			elements = append(elements, para)
		case *gmast.Blockquote:
			// Nested blockquote
			nested := w.convertBlockquote(c)
			elements = append(elements, nested)
		case *gmast.List:
			list := w.convertList(c, 0)
			elements = append(elements, list)
		default:
			elements = append(elements, w.flattenBlock(child)...)
		}
	}

	return ast.NewBlockquote(elements...)
}

// tryConvertAlert checks if a blockquote is an alert/callout
// Uses raw source lines because goldmark breaks up [!NOTE] into separate tokens
func (w *astWalker) tryConvertAlert(n *gmast.Blockquote) *ast.Alert {
	firstChild := n.FirstChild()
	if firstChild == nil {
		return nil
	}
	para, ok := firstChild.(*gmast.Paragraph)
	if !ok {
		return nil
	}

	lines := para.Lines()
	if lines.Len() == 0 {
		return nil
	}
	firstLine := lines.At(0)
	head := strings.TrimRight(string(firstLine.Value(w.source)), "\r\n")

	match := alertPattern.FindStringSubmatch(head)
	if match == nil {
		return nil
	}
	name, fold := match[1], match[2]
	rest := strings.TrimSpace(head[len(match[0]):])

	// Callouts written by the preprocessor are always honored; otherwise the
	// flavor decides which types and decorations make a blockquote an alert.
	titled := true
	alertType, known := alertTypeFor(name)
	switch {
	case strings.HasPrefix(name, internalAlert):
		name = strings.TrimPrefix(name, internalAlert)
		alertType, _ = alertTypeFor(name)
	case w.f.Callouts:
		titled = true
	case w.f.Alerts && known && isGitHubAlert(name) && fold == "":
		titled = w.f.AlertTitles
	default:
		return nil
	}

	title := defaultAlertTitle(name)
	var elements []ast.Element
	var lead []ast.InlineRun
	if rest != "" {
		if titled {
			title = rest
		} else {
			lead = append(lead, ast.InlineRun{Text: rest + " "})
		}
	}

	// The rest of the first paragraph, after the marker line, is content.
	w.html = htmlState{}
	inContent := false
	for child := para.FirstChild(); child != nil; child = child.NextSibling() {
		if !inContent {
			if nodeStart(child) < firstLine.Stop {
				continue
			}
			inContent = true
		}
		lead = append(lead, w.extractInlineRunsFromNode(child, false, false, false, false, "")...)
	}
	if len(lead) > 0 {
		elements = append(elements, ast.NewParagraph(lead...))
	}

	// Add remaining blockquote children (after the first paragraph)
	for child := firstChild.NextSibling(); child != nil; child = child.NextSibling() {
		elements = append(elements, w.flattenBlock(child)...)
	}

	return &ast.Alert{
		Type:     alertType,
		Title:    title,
		Elements: elements,
	}
}

// convertMathBlock converts a goldmark-mathjax MathBlock to our AST
func (w *astWalker) convertMathBlock(n *extMathBlock) ast.MathBlock {
	var expr bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		expr.Write(line.Value(w.source))
	}
	return ast.MathBlock{Expression: strings.TrimSpace(expr.String())}
}

// convertDefinitionList converts a goldmark DefinitionList to our AST
func (w *astWalker) convertDefinitionList(n *extDefinitionList) ast.DescriptionList {
	var items []ast.DescriptionItem
	var currentItem *ast.DescriptionItem

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *extDefinitionTerm:
			if currentItem != nil {
				items = append(items, *currentItem)
			}
			runs := w.extractInlineRuns(c)
			currentItem = &ast.DescriptionItem{
				Term: runs,
			}
		case *extDefinitionDescription:
			if currentItem != nil {
				var defRuns []ast.InlineRun
				for descChild := c.FirstChild(); descChild != nil; descChild = descChild.NextSibling() {
					switch descChild.(type) {
					case *gmast.Paragraph, *gmast.TextBlock:
						defRuns = append(defRuns, w.extractInlineRuns(descChild)...)
					}
				}
				currentItem.Definitions = append(currentItem.Definitions, defRuns)
			}
		}
	}
	if currentItem != nil {
		items = append(items, *currentItem)
	}

	return ast.DescriptionList{Items: items}
}

// isTOCMarker checks if a paragraph is the table of contents marker the
// preprocessor substitutes for the flavor's own ([[_TOC_]], [TOC], ...)
func (w *astWalker) isTOCMarker(n *gmast.Paragraph) bool {
	lines := n.Lines()
	if lines.Len() != 1 {
		return false
	}
	line := lines.At(0)
	return strings.TrimSpace(string(line.Value(w.source))) == tocSentinel
}

// generateTOC creates a TableOfContents from the pre-extracted heading tree
func (w *astWalker) generateTOC() ast.TableOfContents {
	var items []ast.TOCItem
	if w.tocTree != nil {
		items = flattenTOCItems(w.tocTree.Items, 1)
	}
	return ast.TableOfContents{Items: items}
}

func (w *astWalker) convertTable(n *extTable) ast.Table {
	var header ast.TableRow
	var rows []ast.TableRow
	var alignments []ast.Alignment

	// Extract alignments from table
	if n.Alignments != nil {
		for _, align := range n.Alignments {
			alignments = append(alignments, convertTableAlignment(align))
		}
	}

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *extTableHeader:
			// TableHeader directly contains TableCell nodes (not TableRow)
			header = w.convertTableHeader(c, alignments)
		case *extTableRow:
			rows = append(rows, w.convertTableRow(c, alignments))
		}
	}

	return ast.NewTable(header, rows, alignments)
}

func (w *astWalker) convertTableHeader(n *extTableHeader, alignments []ast.Alignment) ast.TableRow {
	var cells []ast.TableCell

	i := 0
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if cell, ok := child.(*extTableCell); ok {
			runs := w.extractInlineRuns(cell)
			tableCell := ast.NewTableCell(runs...)
			if i < len(alignments) {
				tableCell.Align = alignments[i]
			}
			cells = append(cells, tableCell)
			i++
		}
	}

	return ast.NewTableRow(cells...)
}

func (w *astWalker) convertTableRow(n *extTableRow, alignments []ast.Alignment) ast.TableRow {
	var cells []ast.TableCell

	i := 0
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if cell, ok := child.(*extTableCell); ok {
			runs := w.extractInlineRuns(cell)
			tableCell := ast.NewTableCell(runs...)
			if i < len(alignments) {
				tableCell.Align = alignments[i]
			}
			cells = append(cells, tableCell)
			i++
		}
	}

	return ast.NewTableRow(cells...)
}

// extractInlineRuns extracts all inline runs from a node
func (w *astWalker) extractInlineRuns(node gmast.Node) []ast.InlineRun {
	var runs []ast.InlineRun
	w.html = htmlState{}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		// Skip TaskCheckBox nodes - they're handled separately
		if _, ok := child.(*extTaskCheckBox); ok {
			continue
		}
		childRuns := w.extractInlineRunsFromNode(child, false, false, false, false, "")
		runs = append(runs, childRuns...)
	}

	return runs
}

func (w *astWalker) extractInlineRunsFromNode(node gmast.Node, bold, italic, code, strikethrough bool, link string) []ast.InlineRun {
	var runs []ast.InlineRun

	switch n := node.(type) {
	case *gmast.Text:
		value := n.Segment.Value(w.source)
		if !n.IsRaw() {
			// Backslash escapes and character references are kept as written
			// in the syntax tree; resolve them the way a renderer does.
			value = util.UnescapePunctuations(value)
			value = util.ResolveNumericReferences(value)
			value = util.ResolveEntityNames(value)
		}
		text := string(value)
		if n.SoftLineBreak() {
			text += " "
		}
		run := ast.InlineRun{
			Text:          text,
			Bold:          bold,
			Italic:        italic,
			Code:          code,
			Strikethrough: strikethrough,
			Link:          link,
		}
		w.html.apply(&run)
		runs = append(runs, run)

	case *gmast.String:
		// The typographer extension writes its replacements as HTML entities
		run := ast.InlineRun{
			Text:          html.UnescapeString(string(n.Value)),
			Bold:          bold,
			Italic:        italic,
			Code:          code,
			Strikethrough: strikethrough,
			Link:          link,
		}
		w.html.apply(&run)
		runs = append(runs, run)

	case *gmast.RawHTML:
		var raw bytes.Buffer
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			raw.Write(seg.Value(w.source))
		}
		if text, ok := w.html.tag(raw.String(), w.f.HTML); ok {
			run := ast.InlineRun{
				Text:          text,
				Bold:          bold,
				Italic:        italic,
				Code:          code,
				Strikethrough: strikethrough,
				Link:          link,
			}
			w.html.apply(&run)
			runs = append(runs, run)
		}

	case *emojiast.Emoji:
		run := ast.InlineRun{
			Text:          string(n.Value.Unicode),
			Bold:          bold,
			Italic:        italic,
			Strikethrough: strikethrough,
			Link:          link,
		}
		w.html.apply(&run)
		runs = append(runs, run)

	case *gmast.Emphasis:
		newBold := bold
		newItalic := italic
		if n.Level == 1 {
			newItalic = true
		} else if n.Level >= 2 {
			newBold = true
		}
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			childRuns := w.extractInlineRunsFromNode(child, newBold, newItalic, code, strikethrough, link)
			runs = append(runs, childRuns...)
		}

	case *extStrikethrough:
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			childRuns := w.extractInlineRunsFromNode(child, bold, italic, code, true, link)
			runs = append(runs, childRuns...)
		}

	case *gmast.CodeSpan:
		var textBuf bytes.Buffer
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*gmast.Text); ok {
				textBuf.Write(t.Segment.Value(w.source))
			}
		}
		textStr := textBuf.String()
		run := ast.InlineRun{
			Text:          textStr,
			Bold:          bold,
			Italic:        italic,
			Code:          true,
			Strikethrough: strikethrough,
			Link:          link,
		}
		// Check for color chip pattern (#RGB, #RRGGBB, rgb(...), hsl(...))
		if w.f.ColorChips {
			run.ColorChip = colorChip(textStr)
		}
		w.html.apply(&run)
		runs = append(runs, run)

	case *extInlineMath:
		var textBuf bytes.Buffer
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*gmast.Text); ok {
				textBuf.Write(t.Segment.Value(w.source))
			}
		}
		run := ast.InlineRun{
			Text:   textBuf.String(),
			Math:   true,
			Bold:   bold,
			Italic: italic,
		}
		runs = append(runs, run)

	case *gmast.Link:
		linkURL := string(n.Destination)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			childRuns := w.extractInlineRunsFromNode(child, bold, italic, code, strikethrough, linkURL)
			runs = append(runs, childRuns...)
		}

	case *gmast.AutoLink:
		url := string(n.URL(w.source))
		run := ast.InlineRun{
			Text:          url,
			Strikethrough: strikethrough,
			Link:          url,
		}
		w.html.apply(&run)
		runs = append(runs, run)

	case *extFootnoteLink:
		// Footnote reference like [^1]
		run := ast.InlineRun{
			Text:          "",
			FootnoteIndex: n.Index,
		}
		runs = append(runs, run)

	case *extFootnoteBacklink:
		// Skip backlinks - they're auto-generated by goldmark
		// We'll add our own in rendering

	default:
		// For any other node type, try to process children
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			childRuns := w.extractInlineRunsFromNode(child, bold, italic, code, strikethrough, link)
			runs = append(runs, childRuns...)
		}
	}

	return runs
}

// Helper functions

func isInsideListItem(node gmast.Node) bool {
	parent := node.Parent()
	for parent != nil {
		if _, ok := parent.(*gmast.ListItem); ok {
			return true
		}
		parent = parent.Parent()
	}
	return false
}

func isInsideBlockquote(node gmast.Node) bool {
	parent := node.Parent()
	for parent != nil {
		if _, ok := parent.(*gmast.Blockquote); ok {
			return true
		}
		parent = parent.Parent()
	}
	return false
}

func convertTableAlignment(align extTableAlignment) ast.Alignment {
	switch align {
	case extAlignLeft:
		return ast.AlignLeft
	case extAlignCenter:
		return ast.AlignCenter
	case extAlignRight:
		return ast.AlignRight
	default:
		return ast.AlignLeft
	}
}

// Table extension types (goldmark extension/ast)
type extTable = extast.Table
type extTableHeader = extast.TableHeader
type extTableRow = extast.TableRow
type extTableCell = extast.TableCell
type extTableAlignment = extast.Alignment

// Task list extension type
type extTaskCheckBox = extast.TaskCheckBox

// Strikethrough extension type
type extStrikethrough = extast.Strikethrough

// Footnote extension types
type extFootnote = extast.Footnote
type extFootnoteLink = extast.FootnoteLink
type extFootnoteList = extast.FootnoteList
type extFootnoteBacklink = extast.FootnoteBacklink

// Math extension types (goldmark-mathjax)
type extMathBlock = mathjax.MathBlock
type extInlineMath = mathjax.InlineMath

// Definition list extension types (goldmark built-in)
type extDefinitionList = extast.DefinitionList
type extDefinitionTerm = extast.DefinitionTerm
type extDefinitionDescription = extast.DefinitionDescription

const (
	extAlignLeft   = extast.AlignLeft
	extAlignCenter = extast.AlignCenter
	extAlignRight  = extast.AlignRight
)
