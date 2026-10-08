package render

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"strings"

	docx "github.com/mmonterroca/docxgo/v2"
	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// DocxRenderer renders AST to DOCX format
type DocxRenderer struct {
	doc          domain.Document
	listCounters map[int]int
	lastListItem domain.Paragraph         // the list item added last, which carries the space after its list
	footnotes    []ast.FootnoteDefinition // Collected footnotes
	opts         Options                  // base directory and image loading policy
	t            theme.Theme              // how everything looks

	// inset is how far, in points, blocks are moved in from the left margin:
	// the blocks of a list item sit under its text. tableInsets has it, in
	// twips, for every table of the body.
	inset       float64
	tableInsets []int
}

// NewDocxRenderer creates a new DOCX renderer
func NewDocxRenderer() *DocxRenderer {
	return &DocxRenderer{
		listCounters: make(map[int]int),
	}
}

// RenderToFile renders AST document to a DOCX file
func (r *DocxRenderer) RenderToFile(astDoc *ast.Document, filename string) error {
	r.t = r.opts.theme().DOCX
	r.doc = docx.NewDocument()
	r.footnotes = nil // Reset footnotes
	r.inset, r.tableInsets = 0, nil
	r.setupDocument()

	for _, elem := range astDoc.Elements {
		if err := r.renderElement(elem); err != nil {
			return err
		}
	}

	// Render collected footnotes at the end
	if len(r.footnotes) > 0 {
		if err := r.renderFootnoteSection(); err != nil {
			return err
		}
	}

	if err := r.addRunning(infoOf(astDoc)); err != nil {
		return err
	}

	// Save to temp file first
	tempFile := filename + ".tmp"
	if err := r.doc.SaveAs(tempFile); err != nil {
		return err
	}

	// Post-process to add table row properties (cantSplit, tblHeader) and
	// table indents
	if err := postProcessDocx(tempFile, filename, r.tableInsets); err != nil {
		os.Remove(tempFile)
		return err
	}

	os.Remove(tempFile)
	return nil
}

func (r *DocxRenderer) setupDocument() {
	// Set page size and margins via default section
	section, err := r.doc.DefaultSection()
	if err != nil {
		return // Ignore error for setup
	}
	// A4, to the twip, unless the theme asks for another size
	size := domain.PageSizeA4
	if page := r.t.Page; page.Width > page.Height || !near(page.Width, 595.28) || !near(page.Height, 841.89) {
		size = domain.PageSize{Width: twips(page.Width), Height: twips(page.Height)}
		if page.Width > page.Height {
			section.SetOrientation(domain.OrientationLandscape)
		}
	}
	section.SetPageSize(size)
	margins := domain.Margins{
		Top:    twips(r.t.Page.MarginTop),
		Bottom: twips(r.t.Page.MarginBottom),
		Left:   twips(r.t.Page.MarginLeft),
		Right:  twips(r.t.Page.MarginRight),
	}
	// A header or footer sits in the middle of its margin, as in PDF
	if h := r.t.Header; !h.Empty() {
		margins.Header = twips((r.t.Page.MarginTop - h.Size) / 2)
	}
	if f := r.t.Footer; !f.Empty() {
		margins.Footer = twips((r.t.Page.MarginBottom - f.Size) / 2)
	}
	section.SetMargins(margins)
}

func (r *DocxRenderer) renderElement(elem ast.Element) error {
	switch e := elem.(type) {
	case ast.Heading:
		return r.renderHeading(e)
	case ast.Paragraph:
		return r.renderParagraph(e)
	case ast.List:
		return r.renderList(e)
	case ast.CodeBlock:
		return r.renderCodeBlock(e)
	case ast.Table:
		return r.renderTable(e)
	case ast.Blockquote:
		return r.renderBlockquote(e)
	case ast.HorizontalRule:
		return r.renderHorizontalRule()
	case ast.Image:
		return r.renderImage(e)
	case ast.FootnoteDefinition:
		// Collect footnotes to render at the end
		r.footnotes = append(r.footnotes, e)
	case ast.Alert:
		return r.renderAlert(e)
	case ast.MermaidDiagram:
		return r.renderMermaidDiagram(e)
	case ast.MathBlock:
		return r.renderMathBlock(e)
	case ast.DescriptionList:
		return r.renderDescriptionList(e)
	case ast.TableOfContents:
		return r.renderTableOfContents(e)
	case ast.FrontMatter:
		return r.renderFrontMatter(e)
	}
	return nil
}

func (r *DocxRenderer) renderHeading(h ast.Heading) error {
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}

	// Font size in half-points
	level := h.Level
	if level < 1 {
		level = 1
	}
	if level > len(r.t.Heading) {
		level = len(r.t.Heading)
	}
	hd := r.t.Heading[level-1]

	r.space(para, hd.LineHeight, hd.SpaceBefore, hd.SpaceAfter)
	r.inside(para)

	// Black is what a word processor uses when no color is given
	base := runBase{size: hd.Size, font: r.t.Fonts.Heading, color: hd.Color, noColor: hd.Color == theme.Black, bold: true}
	for _, astRun := range h.Runs {
		if err := r.addRun(para, astRun, base); err != nil {
			return err
		}
	}

	return nil
}

func (r *DocxRenderer) renderParagraph(p ast.Paragraph) error {
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}

	// Set alignment
	switch p.Align {
	case ast.AlignCenter:
		para.SetAlignment(domain.AlignmentCenter)
	case ast.AlignRight:
		para.SetAlignment(domain.AlignmentRight)
	case ast.AlignJustify:
		para.SetAlignment(domain.AlignmentJustify)
	}
	r.space(para, r.t.Text.LineHeight, 0, r.t.Text.ParagraphSpacing)
	r.inside(para)

	for _, astRun := range p.Runs {
		if err := r.addRunToParagraph(para, astRun); err != nil {
			return err
		}
	}

	return nil
}

func (r *DocxRenderer) addRunToParagraph(para domain.Paragraph, astRun ast.InlineRun) error {
	return r.addRun(para, astRun, r.bodyBase())
}

// addRun adds an inline run to a paragraph whose text has the given look.
func (r *DocxRenderer) addRun(para domain.Paragraph, astRun ast.InlineRun, base runBase) error {
	run, err := para.AddRun()
	if err != nil {
		return err
	}

	// Handle footnote reference
	if astRun.FootnoteIndex > 0 {
		run.SetText("[" + itoa(astRun.FootnoteIndex) + "]")
		run.SetSize(halfPoints(r.t.Footnote.Size))
		run.SetFont(domain.Font{Name: base.font})
		run.SetColor(docxColor(r.t.Link.Color))
		return nil
	}

	// Handle hyperlinks
	if astRun.Link != "" {
		run.SetColor(docxColor(r.t.Link.Color)) // Electric blue for links
		run.SetUnderline(domain.UnderlineSingle)
		run.SetSize(halfPoints(base.size))
		run.SetFont(domain.Font{Name: base.font})
		if base.bold {
			run.SetBold(true)
		}
		if base.italic {
			run.SetItalic(true)
		}
		// Add clickable hyperlink field
		linkField := docx.NewHyperlinkField(astRun.Link, astRun.Text)
		run.AddField(linkField)
		return nil
	}

	// Handle inline math
	if astRun.Math {
		run.SetText(astRun.Text)
		run.SetSize(halfPoints(base.size))
		run.SetItalic(true)
		run.SetFont(domain.Font{Name: base.font})
		run.SetColor(docxColor(r.t.Colors.Math))
		return nil
	}

	// Handle color chip: add colored swatch before the hex text
	if astRun.ColorChip != "" {
		// Add a colored square indicator before the hex code
		chipRun, err := para.AddRun()
		if err != nil {
			return err
		}
		chipRun.SetText("\u25A0 ") // Filled square
		chipRun.SetSize(halfPoints(base.size))
		chipRun.SetFont(domain.Font{Name: base.font})
		chipRun.SetColor(hexToColor(strings.TrimPrefix(astRun.ColorChip, "#")))

		// Then render the hex text as code
		run.SetText(astRun.Text)
		run.SetSize(halfPoints(base.size))
		run.SetFont(domain.Font{Name: r.t.Fonts.Code})
		run.SetColor(docxColor(r.t.Code.Color))
		return nil
	}

	// Superscripts and subscripts use the Unicode script characters where
	// they exist, and smaller type otherwise.
	text, size := astRun.Text, halfPoints(base.size)
	if astRun.Superscript || astRun.Subscript {
		if script, ok := scriptText(text, astRun.Superscript); ok {
			text = script
		} else {
			size = halfPoints(base.size * 0.64)
		}
	}
	run.SetText(text)
	run.SetSize(size)

	if astRun.Code {
		run.SetFont(domain.Font{Name: r.t.Fonts.Code})
		run.SetColor(docxColor(r.t.Code.Color)) // Syntax red for inline code
	} else {
		run.SetFont(domain.Font{Name: base.font})
		if !base.noColor {
			run.SetColor(docxColor(base.color))
		}
	}

	if astRun.Bold || base.bold {
		run.SetBold(true)
	}
	if astRun.Italic || base.italic {
		run.SetItalic(true)
	}
	if astRun.Strikethrough {
		run.SetStrike(true)
		run.SetColor(docxColor(r.t.Text.Faint)) // Muted for strikethrough
	}
	if astRun.Underline {
		run.SetUnderline(domain.UnderlineSingle)
	}
	if astRun.Highlight {
		run.SetHighlight(domain.HighlightYellow)
	}
	if astRun.Inserted {
		run.SetUnderline(domain.UnderlineSingle)
		run.SetColor(docxColor(r.t.Colors.Inserted))
	}
	if astRun.Deleted {
		run.SetStrike(true)
		run.SetColor(docxColor(r.t.Colors.Deleted))
	}

	return nil
}

var (
	superscriptChars = map[rune]rune{
		'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
		'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ',
	}
	subscriptChars = map[rune]rune{
		'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
		'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	}
)

// scriptText maps text to Unicode superscript or subscript characters. ok is
// false when a character has no such form.
func scriptText(text string, super bool) (string, bool) {
	table := subscriptChars
	if super {
		table = superscriptChars
	}
	out := make([]rune, 0, len(text))
	for _, c := range text {
		m, found := table[c]
		if !found {
			return "", false
		}
		out = append(out, m)
	}
	return string(out), true
}

func (r *DocxRenderer) renderList(l ast.List) error {
	r.listCounters = make(map[int]int)
	r.lastListItem = nil
	if err := r.renderListItems(l.Items, l.Ordered, 0); err != nil {
		return err
	}
	// The space after a list goes under its last item
	if r.lastListItem != nil {
		r.lastListItem.SetSpacingAfter(twips(r.t.List.SpaceAfter))
	}
	return nil
}

func (r *DocxRenderer) renderListItems(items []ast.ListItem, ordered bool, level int) error {
	for _, item := range items {
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}

		// Add bullet/number or checkbox
		var prefix string
		if item.IsTask {
			// Task list item - use checkbox symbol
			if item.Checked {
				prefix = "☑" // Checked checkbox
			} else {
				prefix = "☐" // Unchecked checkbox
			}
		} else if ordered {
			r.listCounters[level]++
			prefix = formatOrderedBullet(r.listCounters[level], level)
		} else {
			prefix = getBulletChar(level)
		}

		// Indentation based on level
		r.indent(para, float64(level+1)*r.t.List.Indent, 0)
		r.space(para, r.t.Text.LineHeight, 0, 0)
		r.lastListItem = para

		bulletRun, err := para.AddRun()
		if err != nil {
			return err
		}
		bulletRun.SetText(prefix + " ")
		bulletRun.SetSize(halfPoints(r.t.Text.Size))
		bulletRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
		bulletRun.SetColor(docxColor(r.t.Text.Color))

		// Add content
		for _, astRun := range item.Runs {
			if err := r.addRunToParagraph(para, astRun); err != nil {
				return err
			}
		}

		// What the item holds besides its text goes under it, moved in to
		// about where the text starts after the bullet
		if len(item.Blocks) > 0 {
			para.SetSpacingAfter(twips(r.t.Text.ParagraphSpacing / 2))
			inset := r.inset + float64(level+1)*r.t.List.Indent + r.t.Text.Size
			if err := r.renderItemBlocks(item.Blocks, inset); err != nil {
				return err
			}
		}

		// Nested items
		if len(item.Children) > 0 {
			// A nested list is numbered or not by itself, and counts from one
			r.listCounters[level+1] = 0
			if err := r.renderListItems(item.Children, item.ChildrenOrdered, level+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func getBulletChar(level int) string {
	bullets := []string{"•", "◦", "▪"}
	return bullets[level%len(bullets)]
}

func formatOrderedBullet(num int, level int) string {
	switch level % 3 {
	case 0:
		return fmt.Sprintf("%d.", num)
	case 1:
		if num <= 26 {
			return fmt.Sprintf("%c.", 'a'+num-1)
		}
		return fmt.Sprintf("%d.", num)
	default:
		return fmt.Sprintf("%d)", num)
	}
}

func (r *DocxRenderer) renderCodeBlock(cb ast.CodeBlock) error {
	// Create a single-cell table for IDE-like appearance
	table, err := r.addTable(1, 1)
	if err != nil {
		return err
	}
	r.fullWidth(table)

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	// Set cell background and width
	cell.SetShading(docxColor(r.t.Code.Background))
	cell.SetWidth(r.contentTwips())

	// Set cell border (thin gray border)
	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4, // 0.5pt (in eighths of a point)
		Color: docxColor(r.t.Code.Border),
	}
	cell.SetBorders(domain.TableBorders{
		Top:    borderStyle,
		Bottom: borderStyle,
		Left:   borderStyle,
		Right:  borderStyle,
	})

	// Add code with monospace font
	para, err := cell.AddParagraph()
	if err != nil {
		return err
	}
	r.pad([]domain.Paragraph{para}, r.t.Code.Padding, r.t.Code.BlockLineHeight)

	lines := splitLines(cb.Code)
	for i, line := range lines {
		if i > 0 {
			// Add break for new line
			run, err := para.AddRun()
			if err != nil {
				return err
			}
			run.AddBreak(domain.BreakTypeLine)
		}

		run, err := para.AddRun()
		if err != nil {
			return err
		}

		if line == "" {
			run.SetText(" ") // Preserve empty lines
		} else {
			run.SetText(line)
		}
		run.SetFont(domain.Font{Name: r.t.Fonts.Code}) // Consolas
		run.SetSize(halfPoints(r.t.Code.BlockSize))
		run.SetColor(docxColor(r.t.Code.BlockColor))
	}

	return r.gap(r.t.Code.SpaceAfter)
}

func splitLines(s string) []string {
	var lines []string
	var current []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, string(current))
			current = nil
		} else if s[i] != '\r' {
			current = append(current, s[i])
		}
	}
	if len(current) > 0 {
		lines = append(lines, string(current))
	}
	return lines
}

func (r *DocxRenderer) renderTable(t ast.Table) error {
	numCols := len(t.Header.Cells)
	if numCols == 0 && len(t.Rows) > 0 {
		numCols = len(t.Rows[0].Cells)
	}
	numRows := len(t.Rows) + 1

	if numCols == 0 {
		return nil
	}

	table, err := r.addTable(numRows, numCols)
	if err != nil {
		return err
	}
	r.fullWidth(table)

	colWidths := calcColumnWidths(t, numCols, r.contentTwips())

	// Border style for all cells
	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4, // 0.5pt
		Color: docxColor(r.t.Table.Border),
	}
	borders := domain.TableBorders{
		Top:    borderStyle,
		Bottom: borderStyle,
		Left:   borderStyle,
		Right:  borderStyle,
	}

	// Colors
	headerColor := docxColor(r.t.Table.HeaderBackground)
	evenRowColor := docxColor(r.t.Table.RowBackground)
	oddRowColor := docxColor(r.t.Table.StripeBackground)

	// Render header row
	if len(t.Header.Cells) > 0 {
		row, err := table.Row(0)
		if err != nil {
			return err
		}

		for j, astCell := range t.Header.Cells {
			cell, err := row.Cell(j)
			if err != nil {
				return err
			}

			cell.SetWidth(colWidths[j])
			cell.SetShading(headerColor)
			cell.SetBorders(borders)

			para, err := cell.AddParagraph()
			if err != nil {
				return err
			}

			// Set alignment
			if j < len(t.Alignments) {
				switch t.Alignments[j] {
				case ast.AlignCenter:
					para.SetAlignment(domain.AlignmentCenter)
				case ast.AlignRight:
					para.SetAlignment(domain.AlignmentRight)
				}
			}

			r.pad([]domain.Paragraph{para}, r.t.Table.CellPadding, r.t.Table.LineHeight)

			header := r.bodyBase()
			header.bold = true
			for _, astRun := range astCell.Runs {
				if err := r.addRun(para, astRun, header); err != nil {
					return err
				}
			}
		}
	}

	// Render data rows
	for i, astRow := range t.Rows {
		rowIdx := i + 1
		row, err := table.Row(rowIdx)
		if err != nil {
			return err
		}

		// Zebra striping
		var rowColor domain.Color
		if i%2 == 0 {
			rowColor = evenRowColor
		} else {
			rowColor = oddRowColor
		}

		for j, astCell := range astRow.Cells {
			cell, err := row.Cell(j)
			if err != nil {
				return err
			}

			cell.SetWidth(colWidths[j])
			cell.SetShading(rowColor)
			cell.SetBorders(borders)

			para, err := cell.AddParagraph()
			if err != nil {
				return err
			}

			// Set alignment
			if j < len(t.Alignments) {
				switch t.Alignments[j] {
				case ast.AlignCenter:
					para.SetAlignment(domain.AlignmentCenter)
				case ast.AlignRight:
					para.SetAlignment(domain.AlignmentRight)
				}
			}

			r.pad([]domain.Paragraph{para}, r.t.Table.CellPadding, r.t.Table.LineHeight)

			for _, astRun := range astCell.Runs {
				if err := r.addRunToParagraph(para, astRun); err != nil {
					return err
				}
			}
		}
	}

	return r.gap(r.t.Table.SpaceAfter)
}

// calcColumnWidths computes proportional column widths in twips based on content length.
func calcColumnWidths(t ast.Table, numCols, totalTwips int) []int {
	minColTwips := mmToTwips(15) // minimum 15mm per column

	// Measure max character count per column across header + all rows
	maxLen := make([]int, numCols)
	for j := 0; j < numCols; j++ {
		if j < len(t.Header.Cells) {
			l := cellTextLen(t.Header.Cells[j])
			if l > maxLen[j] {
				maxLen[j] = l
			}
		}
		for _, row := range t.Rows {
			if j < len(row.Cells) {
				l := cellTextLen(row.Cells[j])
				if l > maxLen[j] {
					maxLen[j] = l
				}
			}
		}
		// Ensure at least 1 so we don't divide by zero
		if maxLen[j] < 1 {
			maxLen[j] = 1
		}
	}

	// If all minimum widths exceed total, fall back to equal distribution
	if minColTwips*numCols >= totalTwips {
		w := totalTwips / numCols
		widths := make([]int, numCols)
		for j := range widths {
			widths[j] = w
		}
		return widths
	}

	// Proportional distribution with minimum width enforcement
	totalChars := 0
	for _, l := range maxLen {
		totalChars += l
	}

	widths := make([]int, numCols)
	remaining := totalTwips
	clamped := 0

	// First pass: assign minimums where proportional width is too small
	for j := 0; j < numCols; j++ {
		proportional := totalTwips * maxLen[j] / totalChars
		if proportional < minColTwips {
			widths[j] = minColTwips
			remaining -= minColTwips
			clamped++
		}
	}

	// Second pass: distribute remaining space proportionally among unclamped columns
	if clamped < numCols {
		unclampedChars := 0
		for j := 0; j < numCols; j++ {
			if widths[j] == 0 {
				unclampedChars += maxLen[j]
			}
		}
		if unclampedChars > 0 {
			for j := 0; j < numCols; j++ {
				if widths[j] == 0 {
					widths[j] = remaining * maxLen[j] / unclampedChars
				}
			}
		}
	}

	return widths
}

// cellTextLen returns the total character count of all runs in a table cell.
func cellTextLen(cell ast.TableCell) int {
	n := 0
	for _, r := range cell.Runs {
		n += len([]rune(r.Text))
	}
	return n
}

func (r *DocxRenderer) renderBlockquote(bq ast.Blockquote) error {
	for _, elem := range bq.Elements {
		switch e := elem.(type) {
		case ast.Paragraph:
			para, err := r.doc.AddParagraph()
			if err != nil {
				return err
			}

			// The bar is the paragraph's left border
			quote := r.t.Quote
			para.SetBorders(domain.ParagraphBorders{Left: ruleBorder(quote.BarWidth, quote.Bar)})
			r.indent(para, quote.Indent, 0)
			r.space(para, r.t.Text.LineHeight, 0, quote.SpaceAfter)
			base := r.bodyBase()
			base.italic = quote.Italic

			for _, astRun := range e.Runs {
				if err := r.addRun(para, astRun, base); err != nil {
					return err
				}
			}

		case ast.Blockquote:
			if err := r.renderBlockquote(e); err != nil {
				return err
			}

		case ast.List:
			if err := r.renderList(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *DocxRenderer) renderHorizontalRule() error {
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}

	// A paragraph with nothing in it but a border below
	rule := r.t.Rule
	r.inside(para)
	para.SetBorderBottom(ruleBorder(rule.Width, rule.Color))
	para.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingExact, Value: 20})
	para.SetSpacingBefore(twips(rule.Space))
	para.SetSpacingAfter(twips(rule.Space))
	return nil
}

func (r *DocxRenderer) renderImage(img ast.Image) error {
	// Try to embed the actual image; fall back to a text placeholder on failure
	// (e.g. unreachable URL or unsupported format).
	if img.URL != "" {
		if err := r.embedImage(img); err == nil {
			return nil
		}
	}
	return r.renderImagePlaceholder(img)
}

// embedImage loads the image referenced by img.URL and embeds it into the
// document, scaling it to fit the content width. The docx library only accepts
// a file path, so the bytes are written to a temporary file (with a matching
// extension) that is removed once the library has read it into memory.
func (r *DocxRenderer) embedImage(img ast.Image) error {
	data, displayWidth, err := loadRasterImage(img.URL, r.opts, img.Width)
	if err != nil {
		return err
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid image dimensions")
	}

	ext := imageFormatExt(format)
	if ext == "" {
		return fmt.Errorf("unsupported image format %q", format)
	}

	tmp, err := os.CreateTemp("", "markout-img-*"+ext)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Honor an explicit width hint, then scale down to the content width
	// (96 DPI), preserving aspect ratio.
	maxWidthPx := int((r.t.Page.ContentWidth() - r.inset) * 96.0 / 72.0)
	w, h := cfg.Width, cfg.Height
	if displayWidth > 0 {
		h = h * displayWidth / w
		w = displayWidth
	}
	if w > maxWidthPx {
		h = h * maxWidthPx / w
		w = maxWidthPx
	}

	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	para.SetAlignment(domain.AlignmentCenter)
	r.inside(para)
	r.space(para, 0, 0, r.t.Text.ParagraphSpacing)
	if _, err := para.AddImageWithSize(tmpPath, domain.NewImageSize(w, h)); err != nil {
		return err
	}

	// Caption with the alt text, if any.
	if img.Alt != "" {
		capRun, err := para.AddRun()
		if err != nil {
			return err
		}
		capRun.AddBreak(domain.BreakTypeLine)
		altRun, err := para.AddRun()
		if err != nil {
			return err
		}
		altRun.SetText(img.Alt)
		altRun.SetColor(docxColor(r.t.Text.Muted))
		altRun.SetItalic(true)
		altRun.SetSize(halfPoints(r.t.Caption.Size))
		altRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
	}

	return nil
}

// imageFormatExt maps an image.DecodeConfig format name to a file extension
// understood by the docx library. Returns "" for unsupported formats.
func imageFormatExt(format string) string {
	switch format {
	case "png":
		return ".png"
	case "jpeg":
		return ".jpg"
	case "gif":
		return ".gif"
	default:
		return ""
	}
}

func (r *DocxRenderer) renderImagePlaceholder(img ast.Image) error {
	// Render image as a placeholder text with the URL.
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	r.space(para, r.t.Text.LineHeight, 0, r.t.Text.ParagraphSpacing)

	para.SetAlignment(domain.AlignmentCenter)
	r.inside(para)

	// Add image placeholder
	run, err := para.AddRun()
	if err != nil {
		return err
	}

	// Display alt text or URL
	displayText := img.Alt
	if displayText == "" {
		displayText = "[Image]"
	}

	run.SetText("🖼 " + displayText)
	run.SetColor(docxColor(r.t.Text.Muted))
	run.SetItalic(true)
	run.SetSize(halfPoints(r.t.Text.Size))
	run.SetFont(domain.Font{Name: r.t.Fonts.Body})

	// Add URL on next line if present
	if img.URL != "" {
		urlRun, err := para.AddRun()
		if err != nil {
			return err
		}
		urlRun.AddBreak(domain.BreakTypeLine)

		urlTextRun, err := para.AddRun()
		if err != nil {
			return err
		}
		urlTextRun.SetText(img.URL)
		urlTextRun.SetColor(docxColor(r.t.Link.Color))
		urlTextRun.SetUnderline(domain.UnderlineSingle)
		urlTextRun.SetSize(halfPoints(r.t.Caption.Size))
		urlTextRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
	}

	return nil
}

func (r *DocxRenderer) renderFootnoteSection() error {
	// Add separator line
	sepPara, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	// A short line: a top border on a paragraph indented from the right
	sepPara.SetBorderTop(ruleBorder(0.5, r.t.Footnote.Rule))
	sepPara.SetIndentRight(int(float64(r.contentTwips()) * 0.7))
	sepPara.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingExact, Value: twips(10)})
	sepPara.SetSpacingBefore(twips(20))

	// Render each footnote
	for _, fn := range r.footnotes {
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}
		r.space(para, r.t.Footnote.LineHeight, 0, 4)

		// Footnote number
		numRun, err := para.AddRun()
		if err != nil {
			return err
		}
		numRun.SetText(itoa(fn.Index) + ". ")
		numRun.SetSize(halfPoints(r.t.Footnote.Size))
		numRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
		numRun.SetColor(docxColor(r.t.Text.Muted))

		// Footnote content; its paragraphs are lines of the one paragraph
		first := true
		for _, elem := range fn.Elements {
			if p, ok := elem.(ast.Paragraph); ok {
				if !first {
					br, err := para.AddRun()
					if err != nil {
						return err
					}
					br.AddBreak(domain.BreakTypeLine)
				}
				first = false
				note := runBase{size: r.t.Footnote.Size, font: r.t.Fonts.Body, color: r.t.Text.Muted}
				for _, astRun := range p.Runs {
					if err := r.addRun(para, astRun, note); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// renderAlert renders an alert/callout as a single-cell table with colored border
func (r *DocxRenderer) renderAlert(alert ast.Alert) error {
	// Get colors based on alert type
	colors := alertColors(r.t.Alert, alert.Type)
	bgColor, borderColor := colors.Background.Hex(), colors.Border.Hex()

	// Create a single-cell table (like code blocks)
	table, err := r.addTable(1, 1)
	if err != nil {
		return err
	}
	r.fullWidth(table)

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(hexToColor(bgColor))
	cell.SetWidth(r.contentTwips())

	barEighths := int(r.t.Alert.BarWidth*8 + 0.5)

	// Colored left border, thin gray on other sides
	thinBorder := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: docxColor(r.t.Code.Border),
	}
	cell.SetBorders(domain.TableBorders{
		Top:    thinBorder,
		Bottom: thinBorder,
		Left: domain.BorderStyle{
			Style: domain.BorderSingle,
			Width: barEighths, // the thick bar on the left
			Color: hexToColor(borderColor),
		},
		Right: thinBorder,
	})

	// Add title line
	titlePara, err := cell.AddParagraph()
	if err != nil {
		return err
	}
	inCell := []domain.Paragraph{titlePara}
	titleRun, err := titlePara.AddRun()
	if err != nil {
		return err
	}
	titleRun.SetText(alert.Title)
	titleRun.SetBold(true)
	titleRun.SetSize(halfPoints(r.t.Text.Size))
	titleRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
	titleRun.SetColor(hexToColor(borderColor))

	// Add content elements
	for _, elem := range alert.Elements {
		if p, ok := elem.(ast.Paragraph); ok {
			contentPara, err := cell.AddParagraph()
			if err != nil {
				return err
			}
			inCell = append(inCell, contentPara)
			for _, astRun := range p.Runs {
				if err := r.addRunToParagraph(contentPara, astRun); err != nil {
					return err
				}
			}
		}
	}

	r.pad(inCell, r.t.Alert.Padding, r.t.Text.LineHeight)
	return r.gap(r.t.Alert.SpaceAfter)
}

// renderMermaidDiagram renders a mermaid diagram as a labeled placeholder
func (r *DocxRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) error {
	table, err := r.addTable(1, 1)
	if err != nil {
		return err
	}
	r.fullWidth(table)

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(docxColor(r.t.Diagram.Background))
	cell.SetWidth(r.contentTwips())

	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: docxColor(r.t.Diagram.Border),
	}
	cell.SetBorders(domain.TableBorders{
		Top: borderStyle, Bottom: borderStyle,
		Left: borderStyle, Right: borderStyle,
	})

	// Label
	labelPara, err := cell.AddParagraph()
	if err != nil {
		return err
	}
	labelRun, err := labelPara.AddRun()
	if err != nil {
		return err
	}
	labelRun.SetText("Mermaid Diagram")
	labelRun.SetBold(true)
	labelRun.SetSize(halfPoints(r.t.Text.Size))
	labelRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
	labelRun.SetColor(docxColor(r.t.Diagram.Text))

	// Source code
	codePara, err := cell.AddParagraph()
	if err != nil {
		return err
	}
	r.pad([]domain.Paragraph{labelPara, codePara}, r.t.Code.Padding, r.t.Code.BlockLineHeight)

	lines := splitLines(diagram.Source)
	for i, line := range lines {
		if i > 0 {
			br, err := codePara.AddRun()
			if err != nil {
				return err
			}
			br.AddBreak(domain.BreakTypeLine)
		}
		run, err := codePara.AddRun()
		if err != nil {
			return err
		}
		if line == "" {
			run.SetText(" ")
		} else {
			run.SetText(line)
		}
		run.SetFont(domain.Font{Name: r.t.Fonts.Code})
		run.SetSize(halfPoints(r.t.Code.BlockSize))
		run.SetColor(docxColor(r.t.Diagram.Text))
	}

	return r.gap(r.t.Code.SpaceAfter)
}

// renderMathBlock renders a block-level math expression
func (r *DocxRenderer) renderMathBlock(math ast.MathBlock) error {
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	para.SetAlignment(domain.AlignmentCenter)
	r.space(para, r.t.Text.LineHeight, 0, r.t.Text.ParagraphSpacing)
	r.inside(para)

	run, err := para.AddRun()
	if err != nil {
		return err
	}
	run.SetText(math.Expression)
	run.SetItalic(true)
	run.SetSize(halfPoints(r.t.Text.Size))
	run.SetFont(domain.Font{Name: r.t.Fonts.Body})
	run.SetColor(docxColor(r.t.Colors.Math))

	return nil
}

// renderDescriptionList renders a definition/description list
func (r *DocxRenderer) renderDescriptionList(dl ast.DescriptionList) error {
	term := r.bodyBase()
	term.bold = true

	for _, item := range dl.Items {
		// Render term as bold paragraph
		termPara, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}
		r.space(termPara, r.t.Text.LineHeight, 0, 0)
		for _, run := range item.Term {
			if err := r.addRun(termPara, run, term); err != nil {
				return err
			}
		}

		// Render each definition as indented paragraph
		for n, def := range item.Definitions {
			defPara, err := r.doc.AddParagraph()
			if err != nil {
				return err
			}
			after := 0.0
			if n == len(item.Definitions)-1 {
				after = 4
			}
			r.indent(defPara, r.t.List.Indent, 0)
			r.space(defPara, r.t.Text.LineHeight, 0, after)

			for _, run := range def {
				if err := r.addRunToParagraph(defPara, run); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// renderTableOfContents renders a generated table of contents
func (r *DocxRenderer) renderTableOfContents(toc ast.TableOfContents) error {
	// Title
	titlePara, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	r.space(titlePara, 0, 8, 8)
	titleRun, err := titlePara.AddRun()
	if err != nil {
		return err
	}
	titleRun.SetText("Table of Contents")
	titleRun.SetBold(true)
	titleRun.SetSize(halfPoints(r.t.Heading[2].Size))
	titleRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
	titleRun.SetColor(docxColor(r.t.Text.Color))

	// TOC items with level-based indent
	for _, item := range toc.Items {
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}

		// Indent based on level
		r.indent(para, float64(item.Level-1)*r.t.List.Indent, 0)
		r.space(para, r.t.Text.LineHeight, 0, 0)

		run, err := para.AddRun()
		if err != nil {
			return err
		}
		run.SetText(item.Title)
		run.SetSize(halfPoints(r.t.Text.Size))
		run.SetFont(domain.Font{Name: r.t.Fonts.Body})
		run.SetColor(docxColor(r.t.Link.Color))
	}

	return r.gap(r.t.Text.ParagraphSpacing)
}

// renderFrontMatter renders YAML front matter as a styled metadata block
func (r *DocxRenderer) renderFrontMatter(fm ast.FrontMatter) error {
	// Create a single-cell table for the metadata block
	table, err := r.addTable(1, 1)
	if err != nil {
		return err
	}
	r.fullWidth(table)

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(docxColor(r.t.Box.Background))
	cell.SetWidth(r.contentTwips())

	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: docxColor(r.t.Box.Border),
	}
	cell.SetBorders(domain.TableBorders{
		Top: borderStyle, Bottom: borderStyle,
		Left: borderStyle, Right: borderStyle,
	})

	// Render known fields first
	var inCell []domain.Paragraph
	renderField := func(label, value string) error {
		if value == "" {
			return nil
		}
		para, err := cell.AddParagraph()
		if err != nil {
			return err
		}
		inCell = append(inCell, para)
		labelRun, err := para.AddRun()
		if err != nil {
			return err
		}
		labelRun.SetText(label + ": ")
		labelRun.SetBold(true)
		labelRun.SetSize(halfPoints(r.t.Text.Size))
		labelRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
		labelRun.SetColor(docxColor(r.t.Text.Muted))

		valueRun, err := para.AddRun()
		if err != nil {
			return err
		}
		valueRun.SetText(value)
		valueRun.SetSize(halfPoints(r.t.Text.Size))
		valueRun.SetFont(domain.Font{Name: r.t.Fonts.Body})
		valueRun.SetColor(docxColor(r.t.Text.Color))
		return nil
	}

	if err := renderField("Title", fm.Title); err != nil {
		return err
	}
	if err := renderField("Author", fm.Author); err != nil {
		return err
	}
	if err := renderField("Date", fm.Date); err != nil {
		return err
	}

	// Render any remaining raw fields
	for _, key := range sortedKeys(fm.Raw) {
		if key == "title" || key == "author" || key == "date" {
			continue
		}
		if strVal, ok := fm.Raw[key].(string); ok {
			if err := renderField(key, strVal); err != nil {
				return err
			}
		}
	}

	r.pad(inCell, r.t.Alert.Padding, r.t.Text.LineHeight)
	return r.gap(r.t.Text.ParagraphSpacing)
}

// addRunning sets the theme's header and footer on the document. Page
// numbers are fields, which the word processor fills in.
func (r *DocxRenderer) addRunning(info docInfo) error {
	if r.t.Header.Empty() && r.t.Footer.Empty() {
		return nil
	}
	section, err := r.doc.DefaultSection()
	if err != nil {
		return err
	}
	if h := r.t.Header; !h.Empty() {
		header, err := section.Header(domain.HeaderDefault)
		if err != nil {
			return err
		}
		if err := r.fillRunning(header, h, info, true); err != nil {
			return err
		}
	}
	if f := r.t.Footer; !f.Empty() {
		footer, err := section.Footer(domain.FooterDefault)
		if err != nil {
			return err
		}
		if err := r.fillRunning(footer, f, info, false); err != nil {
			return err
		}
	}
	return nil
}

// runningPart is what headers and footers have in common.
type runningPart interface {
	AddTable(rows, cols int) (domain.Table, error)
}

// fillRunning lays a header or footer out as a one-row table without
// borders: left, center and right text each in a cell of its own.
func (r *DocxRenderer) fillRunning(part runningPart, run theme.Running, info docInfo, isHeader bool) error {
	table, err := part.AddTable(1, 3)
	if err != nil {
		return err
	}
	r.fullWidth(table)
	row, err := table.Row(0)
	if err != nil {
		return err
	}

	// The rule is the border on the side of the page's text
	var borders domain.TableBorders
	if run.Rule {
		line := domain.BorderStyle{Style: domain.BorderSingle, Width: 4, Color: docxColor(r.t.Rule.Color)}
		if isHeader {
			borders.Bottom = line
		} else {
			borders.Top = line
		}
	}

	total := r.contentTwips()
	slots := []struct {
		text  string
		align domain.Alignment
	}{{run.Left, domain.AlignmentLeft}, {run.Center, domain.AlignmentCenter}, {run.Right, domain.AlignmentRight}}
	for i, slot := range slots {
		cell, err := row.Cell(i)
		if err != nil {
			return err
		}
		cell.SetWidth(total / 3)
		if run.Rule {
			cell.SetBorders(borders)
		}
		para, err := cell.AddParagraph()
		if err != nil {
			return err
		}
		para.SetAlignment(slot.align)

		for _, part := range pageParts(info.static(slot.text)) {
			piece, err := para.AddRun()
			if err != nil {
				return err
			}
			piece.SetSize(halfPoints(run.Size))
			piece.SetFont(domain.Font{Name: r.t.Fonts.Body})
			piece.SetColor(docxColor(run.Color))
			switch part {
			case "{page}":
				err = piece.AddField(docx.NewPageNumberField())
			case "{pages}":
				err = piece.AddField(docx.NewPageCountField())
			default:
				err = piece.SetText(part)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- layout ---------------------------------------------------------------
//
// The helpers below write the theme's spacing into the document.

// space sets a paragraph's line height and the space before and after it.
// A line height of zero leaves it to the word processor.
func (r *DocxRenderer) space(p domain.Paragraph, lineHeight, before, after float64) {
	if lineHeight > 0 {
		// "At least": taller content, such as an image, still gets its room
		p.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingAtLeast, Value: twips(lineHeight)})
	}
	p.SetSpacingBefore(twips(before))
	p.SetSpacingAfter(twips(after))
}

// indent sets a paragraph's left and right indent, counted from the list
// item it is in, if any.
func (r *DocxRenderer) indent(p domain.Paragraph, left, right float64) {
	p.SetIndentLeft(twips(left + r.inset))
	p.SetIndentRight(twips(right))
}

// pad gives the paragraphs of a table cell the padding of their panel:
// indents on both sides, space above the first and below the last.
func (r *DocxRenderer) pad(paragraphs []domain.Paragraph, padding, lineHeight float64) {
	for i, p := range paragraphs {
		before, after := 0.0, 0.0
		if i == 0 {
			before = padding
		}
		if i == len(paragraphs)-1 {
			after = padding
		}
		r.space(p, lineHeight, before, after)
		p.SetIndentLeft(twips(padding))
		p.SetIndentRight(twips(padding))
	}
}

// gap adds vertical space after a table, as an empty paragraph of that
// height. Tables cannot carry space of their own.
func (r *DocxRenderer) gap(height float64) error {
	if height < 1 {
		height = 1
	}
	p, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	p.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingExact, Value: twips(height)})
	return nil
}

// fullWidth makes a table as wide as the text. Without it some viewers size
// a table to its content, whatever the widths of its cells say.
func (r *DocxRenderer) fullWidth(table domain.Table) {
	table.SetWidth(domain.TableWidth{Type: domain.WidthDXA, Value: r.contentTwips()})
}

// ruleBorder is a line of the given thickness in points.
func ruleBorder(width float64, c theme.Color) domain.BorderStyle {
	eighths := int(width*8 + 0.5)
	if eighths < 2 {
		eighths = 2
	}
	return domain.BorderStyle{Style: domain.BorderSingle, Width: eighths, Color: docxColor(c)}
}

// runBase is the look of the text a run belongs to; the run's own
// formatting is applied on top of it.
type runBase struct {
	size    float64
	font    string
	color   theme.Color
	noColor bool // leave plain text in the word processor's default color
	bold    bool
	italic  bool
}

// bodyBase is the look of ordinary text.
func (r *DocxRenderer) bodyBase() runBase {
	return runBase{size: r.t.Text.Size, font: r.t.Fonts.Body, color: r.t.Text.Color}
}

// near reports whether two lengths in points are within a point of each other.
func near(a, b float64) bool {
	return a-b < 1 && b-a < 1
}

// twips converts points to twentieths of a point, the unit of DOCX lengths.
func twips(pt float64) int {
	return int(pt * 20)
}

// halfPoints converts points to half-points, the unit of DOCX font sizes.
func halfPoints(pt float64) int {
	return int(pt * 2)
}

// docxColor converts a theme color.
func docxColor(c theme.Color) domain.Color {
	return domain.Color{R: c.R, G: c.G, B: c.B}
}

// contentTwips is the width blocks have: between the page margins, less the
// inset of the list item they are in.
func (r *DocxRenderer) contentTwips() int {
	return twips(r.t.Page.ContentWidth() - r.inset)
}

// addTable adds a table to the body, noting how far it is to be moved in;
// see indentTables.
func (r *DocxRenderer) addTable(rows, cols int) (domain.Table, error) {
	r.tableInsets = append(r.tableInsets, twips(r.inset))
	return r.doc.AddTable(rows, cols)
}

// inside moves a paragraph that has no indent of its own in to the list item
// it is in.
func (r *DocxRenderer) inside(p domain.Paragraph) {
	if r.inset > 0 {
		p.SetIndentLeft(twips(r.inset))
	}
}

// renderItemBlocks renders the blocks of a list item under its text, moved in
// by inset points.
func (r *DocxRenderer) renderItemBlocks(blocks []ast.Element, inset float64) error {
	outer, counters, last := r.inset, r.listCounters, r.lastListItem
	r.inset = inset
	defer func() { r.inset, r.listCounters, r.lastListItem = outer, counters, last }()
	for _, block := range blocks {
		if err := r.renderElement(block); err != nil {
			return err
		}
	}
	return nil
}

// Helper functions

func mmToTwips(mm float64) int {
	return int(mm * 1440 / 25.4)
}

func hexToColor(hex string) domain.Color {
	var r, g, b uint8
	if len(hex) == 6 {
		fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	}
	return domain.Color{R: r, G: g, B: b}
}

// RenderDocxToFile is a convenience function
func RenderDocxToFile(doc *ast.Document, filename string) error {
	renderer := NewDocxRenderer()
	return renderer.RenderToFile(doc, filename)
}

// RenderDocxToFileWithBaseDir renders to DOCX, resolving relative image paths
// against baseDir (typically the directory of the source Markdown file).
func RenderDocxToFileWithBaseDir(doc *ast.Document, filename, baseDir string) error {
	return RenderDocx(doc, filename, Options{BaseDir: baseDir})
}

// RenderDocx renders to DOCX with the given options.
func RenderDocx(doc *ast.Document, filename string, opts Options) error {
	renderer := NewDocxRenderer()
	renderer.opts = opts
	return renderer.RenderToFile(doc, filename)
}

// postProcessDocx modifies the DOCX to add table row properties
// that prevent tables from splitting awkwardly across pages
func postProcessDocx(inputFile, outputFile string, tableInsets []int) error {
	// Open the input DOCX (which is a ZIP file)
	zipReader, err := zip.OpenReader(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open docx: %w", err)
	}
	defer zipReader.Close()

	// Create output file
	outFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	defer outFile.Close()

	zipWriter := zip.NewWriter(outFile)
	defer zipWriter.Close()

	// Process each file in the ZIP
	for _, file := range zipReader.File {
		rc, err := file.Open()
		if err != nil {
			return err
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}

		// Modify document.xml to add table row properties
		if file.Name == "word/document.xml" {
			content = addTableRowProperties(content)
			content = indentTables(content, tableInsets)
		}
		// The library marks every field as needing an update, which makes
		// Word ask "This document contains fields that may refer to other
		// files. Do you want to update the fields?" on opening. Page numbers
		// are recalculated by the word processor anyway.
		if strings.HasPrefix(file.Name, "word/") && strings.HasSuffix(file.Name, ".xml") {
			content = bytes.ReplaceAll(content, []byte(` w:dirty="true"`), nil)
		}

		// Write to output
		writer, err := zipWriter.Create(file.Name)
		if err != nil {
			return err
		}
		_, err = writer.Write(content)
		if err != nil {
			return err
		}
	}

	return nil
}

// indentTables moves tables in from the left margin: insets has the distance
// in twips for each table of the body, in order. A table in a list item is
// moved in like the text around it; the library has no call for that.
func indentTables(content []byte, insets []int) []byte {
	const open = "<w:tbl>"
	str := string(content)
	if strings.Count(str, open) != len(insets) {
		return content
	}
	var out strings.Builder
	for _, inset := range insets {
		at := strings.Index(str, open) + len(open)
		out.WriteString(str[:at])
		str = str[at:]
		if inset <= 0 {
			continue
		}
		end := strings.Index(str, "</w:tblPr>")
		if end < 0 {
			continue
		}
		// The schema wants tblInd after the width and before these
		for _, later := range []string{"<w:tblBorders", "<w:shd", "<w:tblLayout", "<w:tblCellMar", "<w:tblLook"} {
			if i := strings.Index(str[:end], later); i >= 0 && i < end {
				end = i
			}
		}
		out.WriteString(str[:end])
		fmt.Fprintf(&out, `<w:tblInd w:w="%d" w:type="dxa"/>`, inset)
		str = str[end:]
	}
	out.WriteString(str)
	return []byte(out.String())
}

// addTableRowProperties adds cantSplit and tblHeader to table rows
func addTableRowProperties(content []byte) []byte {
	str := string(content)

	// Pattern: <w:tr> or <w:tr ...>
	// We need to add <w:trPr><w:cantSplit/></w:trPr> after <w:tr...>
	// For the first row of each table, also add <w:tblHeader/>

	// Replace table rows to add cantSplit
	// This is a simplified approach - add cantSplit to all rows

	// Find all <w:tr> tags and add properties
	var result strings.Builder
	i := 0
	tableRowCount := 0
	inTable := false

	for i < len(str) {
		// Check for table start
		if i+6 < len(str) && str[i:i+6] == "<w:tbl" {
			inTable = true
			tableRowCount = 0
		}

		// Check for table end
		if i+8 < len(str) && str[i:i+8] == "</w:tbl>" {
			inTable = false
		}

		// Check for table row
		if inTable && i+5 < len(str) && str[i:i+5] == "<w:tr" {
			// Find the end of the opening tag
			tagEnd := strings.Index(str[i:], ">")
			if tagEnd != -1 {
				tagEnd += i + 1

				// Write the opening tag
				result.WriteString(str[i:tagEnd])
				i = tagEnd

				// Check if trPr already exists
				nextContent := str[i:]
				hasTrPr := len(nextContent) > 7 && nextContent[:7] == "<w:trPr"

				if !hasTrPr {
					// Add trPr with cantSplit (and tblHeader for first row)
					if tableRowCount == 0 {
						// First row - add tblHeader for repeat on each page
						result.WriteString("<w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>")
					} else {
						// Other rows - just cantSplit
						result.WriteString("<w:trPr><w:cantSplit/></w:trPr>")
					}
				}

				tableRowCount++
				continue
			}
		}

		result.WriteByte(str[i])
		i++
	}

	return []byte(result.String())
}
