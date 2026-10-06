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
	"github.com/hkmt-sw/markout/internal/config"
)

// DocxRenderer renders AST to DOCX format
type DocxRenderer struct {
	doc          domain.Document
	listCounters map[int]int
	footnotes    []ast.FootnoteDefinition // Collected footnotes
	opts         Options                  // base directory and image loading policy
}

// NewDocxRenderer creates a new DOCX renderer
func NewDocxRenderer() *DocxRenderer {
	return &DocxRenderer{
		listCounters: make(map[int]int),
	}
}

// RenderToFile renders AST document to a DOCX file
func (r *DocxRenderer) RenderToFile(astDoc *ast.Document, filename string) error {
	r.doc = docx.NewDocument()
	r.footnotes = nil // Reset footnotes
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

	// Save to temp file first
	tempFile := filename + ".tmp"
	if err := r.doc.SaveAs(tempFile); err != nil {
		return err
	}

	// Post-process to add table row properties (cantSplit, tblHeader)
	if err := postProcessDocx(tempFile, filename); err != nil {
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
	section.SetPageSize(domain.PageSizeA4)
	section.SetMargins(domain.Margins{
		Top:    mmToTwips(config.MarginTopMM),
		Bottom: mmToTwips(config.MarginBottomMM),
		Left:   mmToTwips(config.MarginLeftMM),
		Right:  mmToTwips(config.MarginRightMM),
	})
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
	var fontSize int
	switch h.Level {
	case 1:
		fontSize = int(config.FontSizeH1HPS)
	case 2:
		fontSize = int(config.FontSizeH2HPS)
	case 3:
		fontSize = int(config.FontSizeH3HPS)
	case 4:
		fontSize = int(config.FontSizeH4HPS)
	case 5:
		fontSize = int(config.FontSizeH5HPS)
	default:
		fontSize = int(config.FontSizeH6HPS)
	}

	for _, astRun := range h.Runs {
		run, err := para.AddRun()
		if err != nil {
			return err
		}
		run.SetText(astRun.Text)
		run.SetSize(fontSize)
		run.SetBold(true)
		run.SetFont(domain.Font{Name: config.FontBody})
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

	for _, astRun := range p.Runs {
		if err := r.addRunToParagraph(para, astRun); err != nil {
			return err
		}
	}

	return nil
}

func (r *DocxRenderer) addRunToParagraph(para domain.Paragraph, astRun ast.InlineRun) error {
	run, err := para.AddRun()
	if err != nil {
		return err
	}

	// Handle footnote reference
	if astRun.FootnoteIndex > 0 {
		run.SetText("[" + itoa(astRun.FootnoteIndex) + "]")
		run.SetSize(int(config.FontSizeSmall * 2)) // Smaller for footnote ref
		run.SetFont(domain.Font{Name: config.FontBody})
		run.SetColor(hexToColor(config.ColorAccent))
		return nil
	}

	// Handle hyperlinks
	if astRun.Link != "" {
		run.SetColor(hexToColor(config.ColorAccent)) // Electric blue for links
		run.SetUnderline(domain.UnderlineSingle)
		run.SetSize(int(config.FontSizeDefaultHPS))
		run.SetFont(domain.Font{Name: config.FontBody})
		// Add clickable hyperlink field
		linkField := docx.NewHyperlinkField(astRun.Link, astRun.Text)
		run.AddField(linkField)
		return nil
	}

	// Handle inline math
	if astRun.Math {
		run.SetText(astRun.Text)
		run.SetSize(int(config.FontSizeDefaultHPS))
		run.SetItalic(true)
		run.SetFont(domain.Font{Name: config.FontBody})
		run.SetColor(hexToColor(config.ColorMathText))
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
		chipRun.SetSize(int(config.FontSizeDefaultHPS))
		chipRun.SetFont(domain.Font{Name: config.FontBody})
		chipRun.SetColor(hexToColor(strings.TrimPrefix(astRun.ColorChip, "#")))

		// Then render the hex text as code
		run.SetText(astRun.Text)
		run.SetSize(int(config.FontSizeDefaultHPS))
		run.SetFont(domain.Font{Name: config.FontCode})
		run.SetColor(hexToColor(config.ColorCodeAccent))
		return nil
	}

	// Superscripts and subscripts use the Unicode script characters where
	// they exist, and smaller type otherwise.
	text, size := astRun.Text, int(config.FontSizeDefaultHPS)
	if astRun.Superscript || astRun.Subscript {
		if script, ok := scriptText(text, astRun.Superscript); ok {
			text = script
		} else {
			size = 14 // 7pt
		}
	}
	run.SetText(text)
	run.SetSize(size)

	if astRun.Code {
		run.SetFont(domain.Font{Name: config.FontCode})
		run.SetColor(hexToColor(config.ColorCodeAccent)) // Syntax red for inline code
	} else {
		run.SetFont(domain.Font{Name: config.FontBody})
		run.SetColor(hexToColor(config.ColorTextPrimary)) // Deep ink blue
	}

	if astRun.Bold {
		run.SetBold(true)
	}
	if astRun.Italic {
		run.SetItalic(true)
	}
	if astRun.Strikethrough {
		run.SetStrike(true)
		run.SetColor(hexToColor(config.ColorTextTertiary)) // Muted for strikethrough
	}
	if astRun.Underline {
		run.SetUnderline(domain.UnderlineSingle)
	}
	if astRun.Highlight {
		run.SetHighlight(domain.HighlightYellow)
	}
	if astRun.Inserted {
		run.SetUnderline(domain.UnderlineSingle)
		run.SetColor(hexToColor(config.ColorSuccess))
	}
	if astRun.Deleted {
		run.SetStrike(true)
		run.SetColor(hexToColor(config.ColorCodeAccent))
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
	return r.renderListItems(l.Items, l.Ordered, 0)
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
		indent := "    "
		for i := 0; i < level; i++ {
			indent += "    "
		}

		bulletRun, err := para.AddRun()
		if err != nil {
			return err
		}
		bulletRun.SetText(indent + prefix + "  ")
		bulletRun.SetSize(int(config.FontSizeDefaultHPS))
		bulletRun.SetFont(domain.Font{Name: config.FontBody})

		// Add content
		for _, astRun := range item.Runs {
			if err := r.addRunToParagraph(para, astRun); err != nil {
				return err
			}
		}

		// Nested items
		if len(item.Children) > 0 {
			if err := r.renderListItems(item.Children, ordered, level+1); err != nil {
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
	table, err := r.doc.AddTable(1, 1)
	if err != nil {
		return err
	}

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	// Set cell background and width
	cell.SetShading(hexToColor(config.ColorCodeBackground))
	cell.SetWidth(mmToTwips(config.ContentWidthMM))

	// Set cell border (thin gray border)
	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4, // 0.5pt (in eighths of a point)
		Color: domain.Color{R: 200, G: 200, B: 200},
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
		run.SetFont(domain.Font{Name: config.FontCode}) // Consolas
		run.SetSize(int(config.FontSizeCodeHPS))
	}

	return nil
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

	table, err := r.doc.AddTable(numRows, numCols)
	if err != nil {
		return err
	}

	colWidths := calcColumnWidths(t, numCols)

	// Border style for all cells
	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4, // 0.5pt
		Color: hexToColor(config.ColorTableBorder),
	}
	borders := domain.TableBorders{
		Top:    borderStyle,
		Bottom: borderStyle,
		Left:   borderStyle,
		Right:  borderStyle,
	}

	// Colors
	headerColor := hexToColor(config.ColorTableHeader)
	evenRowColor := domain.Color{R: 255, G: 255, B: 255}
	oddRowColor := domain.Color{R: 248, G: 248, B: 248}

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

			for _, astRun := range astCell.Runs {
				run, err := para.AddRun()
				if err != nil {
					return err
				}
				run.SetText(astRun.Text)
				run.SetBold(true)
				run.SetSize(int(config.FontSizeDefaultHPS))
				run.SetFont(domain.Font{Name: config.FontBody})
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

			for _, astRun := range astCell.Runs {
				run, err := para.AddRun()
				if err != nil {
					return err
				}
				run.SetText(astRun.Text)
				run.SetSize(int(config.FontSizeDefaultHPS))
				run.SetFont(domain.Font{Name: config.FontBody})
			}
		}
	}

	return nil
}

// calcColumnWidths computes proportional column widths in twips based on content length.
func calcColumnWidths(t ast.Table, numCols int) []int {
	totalTwips := mmToTwips(config.ContentWidthMM)
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

			// Add visual indicator
			indicatorRun, err := para.AddRun()
			if err != nil {
				return err
			}
			indicatorRun.SetText("│ ")
			indicatorRun.SetColor(hexToColor(config.ColorBlockquoteBorder))
			indicatorRun.SetFont(domain.Font{Name: config.FontBody})

			for _, astRun := range e.Runs {
				if err := r.addRunToParagraph(para, astRun); err != nil {
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

	para.SetAlignment(domain.AlignmentCenter)

	run, err := para.AddRun()
	if err != nil {
		return err
	}

	run.SetText("────────────────────────────────────────────────────────────────────────────────")
	run.SetColor(domain.Color{R: 180, G: 180, B: 180})
	run.SetSize(int(config.FontSizeDefaultHPS))

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
	data, err := loadRasterImage(img.URL, r.opts, img.Width)
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
	maxWidthMM := float64(config.ContentWidthMM)
	maxWidthPx := int(maxWidthMM * 96.0 / 25.4)
	w, h := cfg.Width, cfg.Height
	if img.Width > 0 {
		h = h * img.Width / w
		w = img.Width
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
		altRun.SetColor(hexToColor(config.ColorTextSecondary))
		altRun.SetItalic(true)
		altRun.SetSize(int(config.FontSizeSmall * 2))
		altRun.SetFont(domain.Font{Name: config.FontBody})
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

	para.SetAlignment(domain.AlignmentCenter)

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
	run.SetColor(hexToColor(config.ColorTextSecondary))
	run.SetItalic(true)
	run.SetSize(int(config.FontSizeDefaultHPS))
	run.SetFont(domain.Font{Name: config.FontBody})

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
		urlTextRun.SetColor(hexToColor(config.ColorAccent))
		urlTextRun.SetUnderline(domain.UnderlineSingle)
		urlTextRun.SetSize(int(config.FontSizeSmall * 2)) // Convert to half-points
		urlTextRun.SetFont(domain.Font{Name: config.FontBody})
	}

	return nil
}

func (r *DocxRenderer) renderFootnoteSection() error {
	// Add separator line
	sepPara, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	sepRun, err := sepPara.AddRun()
	if err != nil {
		return err
	}
	sepRun.SetText("────────────────────────────────")
	sepRun.SetColor(domain.Color{R: 180, G: 180, B: 180})
	sepRun.SetSize(int(config.FontSizeSmall * 2))

	// Render each footnote
	for _, fn := range r.footnotes {
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}

		// Footnote number
		numRun, err := para.AddRun()
		if err != nil {
			return err
		}
		numRun.SetText(itoa(fn.Index) + ". ")
		numRun.SetSize(int(config.FontSizeSmall * 2))
		numRun.SetFont(domain.Font{Name: config.FontBody})
		numRun.SetColor(hexToColor(config.ColorTextSecondary))

		// Footnote content
		for _, elem := range fn.Elements {
			if p, ok := elem.(ast.Paragraph); ok {
				for _, astRun := range p.Runs {
					contentRun, err := para.AddRun()
					if err != nil {
						return err
					}
					contentRun.SetText(astRun.Text)
					contentRun.SetSize(int(config.FontSizeSmall * 2))
					contentRun.SetFont(domain.Font{Name: config.FontBody})
					contentRun.SetColor(hexToColor(config.ColorTextSecondary))
				}
			}
		}
	}

	return nil
}

// renderAlert renders an alert/callout as a single-cell table with colored border
func (r *DocxRenderer) renderAlert(alert ast.Alert) error {
	// Get colors based on alert type
	bgColor, borderColor := getAlertColors(alert.Type)

	// Create a single-cell table (like code blocks)
	table, err := r.doc.AddTable(1, 1)
	if err != nil {
		return err
	}

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(hexToColor(bgColor))
	cell.SetWidth(mmToTwips(config.ContentWidthMM))

	// Colored left border, thin gray on other sides
	thinBorder := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: domain.Color{R: 200, G: 200, B: 200},
	}
	cell.SetBorders(domain.TableBorders{
		Top:    thinBorder,
		Bottom: thinBorder,
		Left: domain.BorderStyle{
			Style: domain.BorderSingle,
			Width: 24, // 3pt thick left border
			Color: hexToColor(borderColor),
		},
		Right: thinBorder,
	})

	// Add title line
	titlePara, err := cell.AddParagraph()
	if err != nil {
		return err
	}
	titleRun, err := titlePara.AddRun()
	if err != nil {
		return err
	}
	titleRun.SetText(alert.Title)
	titleRun.SetBold(true)
	titleRun.SetSize(int(config.FontSizeDefaultHPS))
	titleRun.SetFont(domain.Font{Name: config.FontBody})
	titleRun.SetColor(hexToColor(borderColor))

	// Add content elements
	for _, elem := range alert.Elements {
		if p, ok := elem.(ast.Paragraph); ok {
			contentPara, err := cell.AddParagraph()
			if err != nil {
				return err
			}
			for _, astRun := range p.Runs {
				if err := r.addRunToParagraph(contentPara, astRun); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// renderMermaidDiagram renders a mermaid diagram as a labeled placeholder
func (r *DocxRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) error {
	table, err := r.doc.AddTable(1, 1)
	if err != nil {
		return err
	}

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(hexToColor(config.ColorMermaidBg))
	cell.SetWidth(mmToTwips(config.ContentWidthMM))

	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: hexToColor(config.ColorMermaidBorder),
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
	labelRun.SetSize(int(config.FontSizeDefaultHPS))
	labelRun.SetFont(domain.Font{Name: config.FontBody})
	labelRun.SetColor(hexToColor(config.ColorMermaidText))

	// Source code
	codePara, err := cell.AddParagraph()
	if err != nil {
		return err
	}

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
		run.SetFont(domain.Font{Name: config.FontCode})
		run.SetSize(int(config.FontSizeCodeHPS))
		run.SetColor(hexToColor(config.ColorMermaidText))
	}

	return nil
}

// renderMathBlock renders a block-level math expression
func (r *DocxRenderer) renderMathBlock(math ast.MathBlock) error {
	para, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	para.SetAlignment(domain.AlignmentCenter)

	run, err := para.AddRun()
	if err != nil {
		return err
	}
	run.SetText(math.Expression)
	run.SetItalic(true)
	run.SetSize(int(config.FontSizeDefaultHPS))
	run.SetFont(domain.Font{Name: config.FontBody})
	run.SetColor(hexToColor(config.ColorMathText))

	return nil
}

// renderDescriptionList renders a definition/description list
func (r *DocxRenderer) renderDescriptionList(dl ast.DescriptionList) error {
	for _, item := range dl.Items {
		// Render term as bold paragraph
		termPara, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}
		for _, run := range item.Term {
			termRun, err := termPara.AddRun()
			if err != nil {
				return err
			}
			termRun.SetText(run.Text)
			termRun.SetBold(true)
			termRun.SetSize(int(config.FontSizeDefaultHPS))
			termRun.SetFont(domain.Font{Name: config.FontBody})
			termRun.SetColor(hexToColor(config.ColorTextPrimary))
		}

		// Render each definition as indented paragraph
		for _, def := range item.Definitions {
			defPara, err := r.doc.AddParagraph()
			if err != nil {
				return err
			}
			// Indent marker
			indentRun, err := defPara.AddRun()
			if err != nil {
				return err
			}
			indentRun.SetText("    ")
			indentRun.SetSize(int(config.FontSizeDefaultHPS))

			for _, run := range def {
				defRun, err := defPara.AddRun()
				if err != nil {
					return err
				}
				defRun.SetText(run.Text)
				defRun.SetSize(int(config.FontSizeDefaultHPS))
				defRun.SetFont(domain.Font{Name: config.FontBody})
				defRun.SetColor(hexToColor(config.ColorTextPrimary))
				if run.Bold {
					defRun.SetBold(true)
				}
				if run.Italic {
					defRun.SetItalic(true)
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
	titleRun, err := titlePara.AddRun()
	if err != nil {
		return err
	}
	titleRun.SetText("Table of Contents")
	titleRun.SetBold(true)
	titleRun.SetSize(int(config.FontSizeH3HPS))
	titleRun.SetFont(domain.Font{Name: config.FontBody})
	titleRun.SetColor(hexToColor(config.ColorTextPrimary))

	// TOC items with level-based indent
	for _, item := range toc.Items {
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}

		// Indent based on level
		indent := ""
		for i := 1; i < item.Level; i++ {
			indent += "    "
		}

		if indent != "" {
			indentRun, err := para.AddRun()
			if err != nil {
				return err
			}
			indentRun.SetText(indent)
			indentRun.SetSize(int(config.FontSizeDefaultHPS))
		}

		run, err := para.AddRun()
		if err != nil {
			return err
		}
		run.SetText(item.Title)
		run.SetSize(int(config.FontSizeDefaultHPS))
		run.SetFont(domain.Font{Name: config.FontBody})
		run.SetColor(hexToColor(config.ColorAccent))
	}

	return nil
}

// renderFrontMatter renders YAML front matter as a styled metadata block
func (r *DocxRenderer) renderFrontMatter(fm ast.FrontMatter) error {
	// Create a single-cell table for the metadata block
	table, err := r.doc.AddTable(1, 1)
	if err != nil {
		return err
	}

	row, err := table.Row(0)
	if err != nil {
		return err
	}

	cell, err := row.Cell(0)
	if err != nil {
		return err
	}

	cell.SetShading(hexToColor(config.ColorSurfaceElevated))
	cell.SetWidth(mmToTwips(config.ContentWidthMM))

	borderStyle := domain.BorderStyle{
		Style: domain.BorderSingle,
		Width: 4,
		Color: hexToColor(config.ColorBorder),
	}
	cell.SetBorders(domain.TableBorders{
		Top: borderStyle, Bottom: borderStyle,
		Left: borderStyle, Right: borderStyle,
	})

	// Render known fields first
	renderField := func(label, value string) error {
		if value == "" {
			return nil
		}
		para, err := cell.AddParagraph()
		if err != nil {
			return err
		}
		labelRun, err := para.AddRun()
		if err != nil {
			return err
		}
		labelRun.SetText(label + ": ")
		labelRun.SetBold(true)
		labelRun.SetSize(int(config.FontSizeDefaultHPS))
		labelRun.SetFont(domain.Font{Name: config.FontBody})
		labelRun.SetColor(hexToColor(config.ColorTextSecondary))

		valueRun, err := para.AddRun()
		if err != nil {
			return err
		}
		valueRun.SetText(value)
		valueRun.SetSize(int(config.FontSizeDefaultHPS))
		valueRun.SetFont(domain.Font{Name: config.FontBody})
		valueRun.SetColor(hexToColor(config.ColorTextPrimary))
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
	for key, value := range fm.Raw {
		if key == "title" || key == "author" || key == "date" {
			continue
		}
		if strVal, ok := value.(string); ok {
			if err := renderField(key, strVal); err != nil {
				return err
			}
		}
	}

	return nil
}

// getAlertColors returns background and border colors for an alert type
func getAlertColors(alertType ast.AlertType) (bg, border string) {
	switch alertType {
	case ast.AlertNote:
		return config.ColorAlertNoteBg, config.ColorAlertNoteBorder
	case ast.AlertTip:
		return config.ColorAlertTipBg, config.ColorAlertTipBorder
	case ast.AlertImportant:
		return config.ColorAlertImportantBg, config.ColorAlertImportantBorder
	case ast.AlertCaution:
		return config.ColorAlertCautionBg, config.ColorAlertCautionBorder
	case ast.AlertWarning:
		return config.ColorAlertWarningBg, config.ColorAlertWarningBorder
	default:
		return config.ColorAlertNoteBg, config.ColorAlertNoteBorder
	}
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
func postProcessDocx(inputFile, outputFile string) error {
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
