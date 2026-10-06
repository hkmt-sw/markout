package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/signintech/gopdf"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/config"
)

// stripEmojis removes emoji characters that can't be rendered by standard fonts
func stripEmojis(s string) string {
	var result strings.Builder
	for _, r := range s {
		// Keep basic ASCII and common Unicode ranges that fonts can render
		// Skip emoji ranges and other characters that might cause rendering issues
		if r < 0x1F600 || r > 0x1FAFF {
			// Not in main emoji ranges
			if !isEmoji(r) {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

// isEmoji checks if a rune is an emoji character
func isEmoji(r rune) bool {
	// Emoji ranges
	switch {
	case r >= 0x1F600 && r <= 0x1F64F: // Emoticons
		return true
	case r >= 0x1F300 && r <= 0x1F5FF: // Misc Symbols and Pictographs
		return true
	case r >= 0x1F680 && r <= 0x1F6FF: // Transport and Map
		return true
	case r >= 0x1F1E0 && r <= 0x1F1FF: // Flags
		return true
	case r >= 0x2600 && r <= 0x26FF: // Misc symbols
		return true
	case r >= 0x2700 && r <= 0x27BF: // Dingbats
		return true
	case r >= 0xFE00 && r <= 0xFE0F: // Variation Selectors
		return true
	case r >= 0x1F900 && r <= 0x1F9FF: // Supplemental Symbols and Pictographs
		return true
	case r >= 0x1FA00 && r <= 0x1FA6F: // Chess Symbols
		return true
	case r >= 0x1FA70 && r <= 0x1FAFF: // Symbols and Pictographs Extended-A
		return true
	case r >= 0x231A && r <= 0x231B: // Watch, Hourglass
		return true
	case r >= 0x23E9 && r <= 0x23F3: // Various symbols
		return true
	case r >= 0x23F8 && r <= 0x23FA: // Various symbols
		return true
	case r == 0x200D: // Zero-width joiner
		return true
	case r == 0x20E3: // Combining enclosing keycap
		return true
	case r >= 0xE0020 && r <= 0xE007F: // Tags
		return true
	}
	// Also check for Unicode symbol categories that often cause issues
	if unicode.Is(unicode.So, r) && r > 0x2000 {
		return true
	}
	return false
}

// mmToPoints converts millimeters to points (1mm = 2.8346 points)
func mmToPoints(mm float64) float64 {
	return mm * 2.8346
}

// PdfRenderer renders AST to PDF format
type PdfRenderer struct {
	pdf          *gopdf.GoPdf
	listCounters map[int]int
	pageWidth    float64                  // in points
	pageHeight   float64                  // in points
	marginLeft   float64                  // in points
	marginRight  float64                  // in points
	marginTop    float64                  // in points
	marginBottom float64                  // in points
	contentWidth float64                  // in points
	currentY     float64                  // in points
	footnotes    []ast.FootnoteDefinition // Collected footnotes
	opts         Options                  // base directory and image loading policy

	// trace, when set, receives one line for everything drawn: the page, the
	// position, the font and the text. Tests compare it with a recorded
	// layout to catch changes in the output.
	trace *strings.Builder
	page  int
	font  string
}

// NewPdfRenderer creates a new PDF renderer
func NewPdfRenderer() *PdfRenderer {
	return &PdfRenderer{
		listCounters: make(map[int]int),
	}
}

// RenderToFile renders AST document to a PDF file
func (r *PdfRenderer) RenderToFile(astDoc *ast.Document, filename string) error {
	r.pdf = &gopdf.GoPdf{}
	r.pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	r.footnotes = nil // Reset footnotes
	r.page = 0

	if err := r.setupDocument(); err != nil {
		return err
	}

	r.addPage()
	r.currentY = r.marginTop

	for _, elem := range astDoc.Elements {
		r.renderElement(elem)
	}

	// Render collected footnotes at the end
	if len(r.footnotes) > 0 {
		r.renderFootnoteSection()
	}

	return r.pdf.WritePdf(filename)
}

func (r *PdfRenderer) setupDocument() error {
	// A4 in points: 595.28 x 841.89
	r.pageWidth = 595.28
	r.pageHeight = 841.89

	// Margins: 20mm converted to points
	r.marginLeft = mmToPoints(20)
	r.marginRight = mmToPoints(20)
	r.marginTop = mmToPoints(20)
	r.marginBottom = mmToPoints(20)
	r.contentWidth = r.pageWidth - r.marginLeft - r.marginRight

	// Register UTF-8 fonts
	if err := r.registerUTF8Fonts(); err != nil {
		return err
	}

	// Set default font
	if err := r.pdf.SetFont("Arial", "", 11); err != nil {
		return err
	}
	r.font = "Arial/11"

	return nil
}

// registerUTF8Fonts loads the embedded Liberation fonts. The family names are
// kept ("Arial"/"Courier") so the rest of the renderer is unchanged; the glyphs
// come from the embedded data, which works on every OS and covers Hungarian.
func (r *PdfRenderer) registerUTF8Fonts() error {
	fonts := []struct {
		family string
		data   []byte
	}{
		{"Arial", fontSansRegular},
		{"Arial-Bold", fontSansBold},
		{"Arial-Italic", fontSansItalic},
		{"Arial-BoldItalic", fontSansBoldItalic},
		{"Courier", fontMonoRegular},
	}
	for _, f := range fonts {
		if err := r.pdf.AddTTFFontData(f.family, f.data); err != nil {
			return err
		}
	}
	return nil
}

func (r *PdfRenderer) setFont(name string, style string, size int) {
	fontName := name
	if name == "Arial" {
		switch style {
		case "B":
			fontName = "Arial-Bold"
		case "I":
			fontName = "Arial-Italic"
		case "BI":
			fontName = "Arial-BoldItalic"
		}
	}
	r.pdf.SetFont(fontName, "", size)
	r.font = fmt.Sprintf("%s/%d", fontName, size)
}

// The drawing primitives below wrap gopdf so that everything put on a page is
// also written to the trace.

func (r *PdfRenderer) logf(format string, args ...any) {
	if r.trace != nil {
		fmt.Fprintf(r.trace, "p%d ", r.page)
		fmt.Fprintf(r.trace, format, args...)
		r.trace.WriteByte('\n')
	}
}

func (r *PdfRenderer) addPage() {
	r.pdf.AddPage()
	r.page++
}

func (r *PdfRenderer) cell(text string) {
	r.logf("text  x=%.1f y=%.1f %s %q", r.pdf.GetX(), r.pdf.GetY(), r.font, text)
	r.pdf.Cell(nil, text)
}

func (r *PdfRenderer) rect(x, y, w, h float64, style string) {
	r.logf("rect  x=%.1f y=%.1f w=%.1f h=%.1f %s", x, y, w, h, style)
	r.pdf.RectFromUpperLeftWithStyle(x, y, w, h, style)
}

func (r *PdfRenderer) line(x1, y1, x2, y2 float64) {
	r.logf("line  x=%.1f y=%.1f to x=%.1f y=%.1f", x1, y1, x2, y2)
	r.pdf.Line(x1, y1, x2, y2)
}

func (r *PdfRenderer) image(holder gopdf.ImageHolder, x, y, w, h float64) error {
	r.logf("image x=%.1f y=%.1f w=%.1f h=%.1f", x, y, w, h)
	return r.pdf.ImageByHolder(holder, x, y, &gopdf.Rect{W: w, H: h})
}

func (r *PdfRenderer) checkPageBreak(height float64) {
	if r.currentY+height > r.pageHeight-r.marginBottom {
		r.addPage()
		r.currentY = r.marginTop
	}
}

func (r *PdfRenderer) renderElement(elem ast.Element) {
	switch e := elem.(type) {
	case ast.Heading:
		r.renderHeading(e)
	case ast.Paragraph:
		r.renderParagraph(e)
	case ast.List:
		r.renderList(e)
	case ast.CodeBlock:
		r.renderCodeBlock(e)
	case ast.Table:
		r.renderTable(e)
	case ast.Blockquote:
		r.renderBlockquote(e)
	case ast.HorizontalRule:
		r.renderHorizontalRule()
	case ast.Image:
		r.renderImage(e)
	case ast.FootnoteDefinition:
		// Collect footnotes to render at the end
		r.footnotes = append(r.footnotes, e)
	case ast.Alert:
		r.renderAlert(e)
	case ast.MermaidDiagram:
		r.renderMermaidDiagram(e)
	case ast.MathBlock:
		r.renderMathBlock(e)
	case ast.DescriptionList:
		r.renderDescriptionList(e)
	case ast.TableOfContents:
		r.renderTableOfContents(e)
	case ast.FrontMatter:
		r.renderFrontMatter(e)
	}
}

func (r *PdfRenderer) renderHeading(h ast.Heading) {
	var fontSize int
	var spaceBefore, spaceAfter, lineHeight float64

	switch h.Level {
	case 1:
		fontSize = 24
		spaceBefore = 20
		spaceAfter = 12
		lineHeight = 28
	case 2:
		fontSize = 20
		spaceBefore = 18
		spaceAfter = 10
		lineHeight = 24
	case 3:
		fontSize = 16
		spaceBefore = 14
		spaceAfter = 8
		lineHeight = 20
	case 4:
		fontSize = 14
		spaceBefore = 12
		spaceAfter = 6
		lineHeight = 18
	case 5:
		fontSize = 12
		spaceBefore = 10
		spaceAfter = 6
		lineHeight = 16
	default:
		fontSize = 11
		spaceBefore = 10
		spaceAfter = 6
		lineHeight = 14
	}

	r.currentY += spaceBefore
	r.checkPageBreak(lineHeight)

	r.setFont("Arial", "B", fontSize)
	text := r.runsToPlainText(h.Runs)
	r.renderText(text, r.marginLeft, lineHeight)

	r.currentY += spaceAfter
	r.setFont("Arial", "", 11)
}

func (r *PdfRenderer) renderParagraph(p ast.Paragraph) {
	lineHeight := 16.0
	r.checkPageBreak(lineHeight)

	r.renderRuns(p.Runs, r.marginLeft, lineHeight)

	r.currentY += 8 // Paragraph spacing
}

// renderRuns renders inline runs with proper formatting and clickable links
func (r *PdfRenderer) renderRuns(runs []ast.InlineRun, startX float64, lineHeight float64) {
	if len(runs) == 0 {
		return
	}

	r.pdf.SetX(startX)
	r.pdf.SetY(r.currentY)
	currentX := startX
	needSpace := false // the previous run ended in whitespace

	for _, run := range runs {
		// Get text to render
		text := run.Text
		if run.FootnoteIndex > 0 {
			text = "[" + itoa(run.FootnoteIndex) + "]"
		} else {
			// Strip emojis that can't be rendered by standard fonts
			text = stripEmojis(text)
		}

		if text == "" {
			continue
		}

		// Set font based on formatting
		fontSize := 11
		style := ""
		if run.Bold && run.Italic {
			style = "BI"
		} else if run.Bold {
			style = "B"
		} else if run.Italic || run.Math {
			style = "I"
		}

		// Superscripts and subscripts are set smaller and shifted off the line.
		yShift := 0.0
		if run.Superscript || run.Subscript {
			fontSize = 8
			yShift = -1
			if run.Subscript {
				yShift = 5
			}
		}

		if run.Code {
			fontSize = 10
			r.setFont("Courier", "", fontSize)
		} else {
			r.setFont("Arial", style, fontSize)
		}

		// Set color
		textColor := config.RGB{R: 26, G: 26, B: 46} // Normal text
		switch {
		case run.Math:
			textColor = config.ColorMathTextRGB
		case run.Link != "":
			textColor = config.ColorLinkRGB
		case run.Inserted:
			textColor = config.ColorSuccessRGB
		case run.Deleted:
			textColor = config.ColorCodeAccentRGB
		case run.Code:
			textColor = config.ColorCodeAccentRGB
		case run.FootnoteIndex > 0:
			textColor = config.ColorLinkRGB
		case run.Strikethrough:
			textColor = config.ColorTextTertiaryRGB
		}
		r.pdf.SetTextColor(textColor.R, textColor.G, textColor.B)

		// Color chip: render a colored square before the hex code
		if run.ColorChip != "" {
			chipColor := hexStringToRGB(strings.TrimPrefix(run.ColorChip, "#"))
			r.pdf.SetFillColor(chipColor.R, chipColor.G, chipColor.B)
			r.rect(currentX, r.currentY, 10, 10, "F")
			currentX += 12
			r.pdf.SetFillColor(255, 255, 255) // Reset
		}

		// Word wrap within this run. A space is only laid out where the source
		// has whitespace, so adjacent runs ("x" + superscript "2", bold text
		// followed by a comma) stay together.
		spaceWidth, _ := r.pdf.MeasureTextWidth(" ")
		words := strings.Fields(text)
		leadingSpace := strings.TrimLeft(text, " \t\n") != text
		for i, word := range words {
			wordWidth, _ := r.pdf.MeasureTextWidth(word)
			gap := i > 0 || leadingSpace || needSpace
			joined := gap && i > 0 // the gap is inside this run and takes its decoration

			if gap && currentX > startX {
				currentX += spaceWidth
			}

			// Check if word fits on current line
			if currentX+wordWidth > startX+r.contentWidth && currentX > startX {
				// Move to next line
				r.currentY += lineHeight
				r.checkPageBreak(lineHeight)
				currentX = startX
				joined = false
			}

			decoStart := currentX
			if joined {
				decoStart -= spaceWidth
			}
			y := r.currentY + yShift

			if run.Highlight {
				c := config.ColorHighlightRGB
				r.pdf.SetFillColor(c.R, c.G, c.B)
				r.rect(decoStart, r.currentY-1, currentX+wordWidth-decoStart, float64(fontSize)+3, "F")
				r.pdf.SetFillColor(255, 255, 255) // Reset
			}

			// Render the word
			r.pdf.SetX(currentX)
			r.pdf.SetY(y)
			r.cell(word)

			if run.Link != "" {
				// Add clickable link: AddExternalLink(url, x, y, w, h)
				r.pdf.AddExternalLink(run.Link, currentX, r.currentY-2, wordWidth, lineHeight)
			}

			underline := run.Underline || run.Inserted
			strike := run.Strikethrough || run.Deleted
			if underline || strike {
				r.pdf.SetStrokeColor(textColor.R, textColor.G, textColor.B)
				r.pdf.SetLineWidth(0.6)
				if underline {
					ly := y + float64(fontSize) + 1
					r.line(decoStart, ly, currentX+wordWidth, ly)
				}
				if strike {
					ly := y + float64(fontSize)*0.58
					r.line(decoStart, ly, currentX+wordWidth, ly)
				}
				r.pdf.SetStrokeColor(0, 0, 0)
				r.pdf.SetLineWidth(0.5)
			}

			currentX += wordWidth
		}
		needSpace = strings.TrimRight(text, " \t\n") != text
	}

	r.currentY += lineHeight
	r.pdf.SetTextColor(0, 0, 0)
	r.setFont("Arial", "", 11)
}

func (r *PdfRenderer) renderText(text string, x float64, lineHeight float64) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}

	currentLine := ""

	for _, word := range words {
		testLine := currentLine
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		testWidth, _ := r.pdf.MeasureTextWidth(testLine)
		if testWidth > r.contentWidth && currentLine != "" {
			r.checkPageBreak(lineHeight)
			r.pdf.SetX(x)
			r.pdf.SetY(r.currentY)
			r.cell(currentLine)
			r.currentY += lineHeight
			currentLine = word
		} else {
			currentLine = testLine
		}
	}

	if currentLine != "" {
		r.checkPageBreak(lineHeight)
		r.pdf.SetX(x)
		r.pdf.SetY(r.currentY)
		r.cell(currentLine)
		r.currentY += lineHeight
	}
}

func (r *PdfRenderer) renderList(l ast.List) {
	r.listCounters = make(map[int]int)
	r.renderListItems(l.Items, l.Ordered, 0)
	r.currentY += 6
}

func (r *PdfRenderer) renderListItems(items []ast.ListItem, ordered bool, level int) {
	indent := float64(level+1) * 20.0 // 20 points per level
	lineHeight := 16.0

	for _, item := range items {
		r.checkPageBreak(lineHeight)

		var prefix string
		if item.IsTask {
			if item.Checked {
				prefix = "[x]"
			} else {
				prefix = "[ ]"
			}
		} else if ordered {
			r.listCounters[level]++
			prefix = formatOrderedBullet(r.listCounters[level], level)
		} else {
			prefix = getBulletChar(level)
		}

		r.setFont("Arial", "", 11)

		bulletText := prefix + " "
		r.pdf.SetX(r.marginLeft + indent)
		r.pdf.SetY(r.currentY)
		r.cell(bulletText)

		bulletWidth, _ := r.pdf.MeasureTextWidth(bulletText)
		text := r.runsToPlainText(item.Runs)
		startX := r.marginLeft + indent + bulletWidth

		words := strings.Fields(text)
		if len(words) > 0 {
			currentLine := ""
			firstLine := true
			remainingWidth := r.contentWidth - indent - bulletWidth

			for _, word := range words {
				testLine := currentLine
				if testLine != "" {
					testLine += " "
				}
				testLine += word

				testWidth, _ := r.pdf.MeasureTextWidth(testLine)
				maxWidth := remainingWidth
				if !firstLine {
					maxWidth = r.contentWidth - indent
				}

				if testWidth > maxWidth && currentLine != "" {
					if firstLine {
						r.pdf.SetX(startX)
						r.pdf.SetY(r.currentY)
						r.cell(currentLine)
						r.currentY += lineHeight
						firstLine = false
					} else {
						r.checkPageBreak(lineHeight)
						r.pdf.SetX(r.marginLeft + indent)
						r.pdf.SetY(r.currentY)
						r.cell(currentLine)
						r.currentY += lineHeight
					}
					currentLine = word
				} else {
					currentLine = testLine
				}
			}

			if currentLine != "" {
				if firstLine {
					r.pdf.SetX(startX)
					r.pdf.SetY(r.currentY)
					r.cell(currentLine)
				} else {
					r.checkPageBreak(lineHeight)
					r.pdf.SetX(r.marginLeft + indent)
					r.pdf.SetY(r.currentY)
					r.cell(currentLine)
				}
				r.currentY += lineHeight
			}
		} else {
			r.currentY += lineHeight
		}

		if len(item.Children) > 0 {
			r.renderListItems(item.Children, ordered, level+1)
		}
	}
}

func (r *PdfRenderer) renderCodeBlock(cb ast.CodeBlock) {
	lineHeight := 12.0
	padding := 8.0

	lines := strings.Split(cb.Code, "\n")
	blockHeight := float64(len(lines))*lineHeight + 2*padding

	r.checkPageBreak(blockHeight)

	r.pdf.SetFillColor(245, 245, 245)
	r.pdf.SetStrokeColor(200, 200, 200)

	startY := r.currentY
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	r.setFont("Courier", "", 9)
	r.pdf.SetTextColor(0, 0, 0)

	codeY := startY + padding
	for _, line := range lines {
		if line == "" {
			line = " "
		}
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(codeY)
		r.cell(line)
		codeY += lineHeight
	}

	r.currentY = startY + blockHeight + 8
	r.setFont("Arial", "", 11)
	r.pdf.SetStrokeColor(0, 0, 0)
}

func (r *PdfRenderer) renderTable(t ast.Table) {
	numCols := len(t.Header.Cells)
	if numCols == 0 && len(t.Rows) > 0 {
		numCols = len(t.Rows[0].Cells)
	}
	if numCols == 0 {
		return
	}

	colWidthsPt := calcColumnWidthsPt(t, numCols, r.contentWidth)
	cellPadding := 4.0
	lineHeight := 13.0
	minRowHeight := lineHeight + 2*cellPadding

	headerCells := t.Header.Cells

	// colX returns the x position for column j
	colX := func(j int) float64 {
		x := r.marginLeft
		for k := 0; k < j; k++ {
			x += colWidthsPt[k]
		}
		return x
	}

	// wrapText splits text into lines that fit within maxWidth
	wrapText := func(text string, maxWidth float64) []string {
		if maxWidth <= 0 {
			return []string{text}
		}
		words := strings.Fields(text)
		if len(words) == 0 {
			return []string{""}
		}
		var lines []string
		currentLine := ""
		for _, word := range words {
			testLine := currentLine
			if testLine != "" {
				testLine += " "
			}
			testLine += word
			w, _ := r.pdf.MeasureTextWidth(testLine)
			if w > maxWidth && currentLine != "" {
				lines = append(lines, currentLine)
				currentLine = word
			} else {
				currentLine = testLine
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		return lines
	}

	// calcRowHeight returns the row height needed for the tallest cell
	calcRowHeight := func(cells []ast.TableCell, bold bool) (float64, [][]string) {
		if bold {
			r.setFont("Arial", "B", 11)
		} else {
			r.setFont("Arial", "", 11)
		}
		maxLines := 1
		wrappedTexts := make([][]string, numCols)
		for j := 0; j < numCols; j++ {
			text := ""
			if j < len(cells) {
				text = r.runsToPlainText(cells[j].Runs)
			}
			innerWidth := colWidthsPt[j] - 2*cellPadding
			lines := wrapText(text, innerWidth)
			wrappedTexts[j] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
		h := float64(maxLines)*lineHeight + 2*cellPadding
		if h < minRowHeight {
			h = minRowHeight
		}
		return h, wrappedTexts
	}

	// drawRow renders backgrounds + text for one row. All cell backgrounds are
	// drawn first, then all text, so any text that overflows a narrow column is
	// not painted over by the next column's background.
	drawRow := func(wrappedTexts [][]string, rowH float64, isHeader bool, rowIdx int) {
		r.pdf.SetStrokeColor(209, 213, 222)

		if isHeader {
			r.pdf.SetFillColor(241, 243, 249)
		} else if rowIdx%2 == 1 {
			r.pdf.SetFillColor(248, 249, 252)
		} else {
			r.pdf.SetFillColor(255, 255, 255)
		}
		for j := 0; j < numCols; j++ {
			r.rect(colX(j), r.currentY, colWidthsPt[j], rowH, "FD")
		}

		if isHeader {
			r.setFont("Arial", "B", 11)
		} else {
			r.setFont("Arial", "", 11)
		}
		r.pdf.SetTextColor(26, 26, 46)
		for j := 0; j < numCols; j++ {
			x := colX(j)
			for li, line := range wrappedTexts[j] {
				r.pdf.SetX(x + cellPadding)
				r.pdf.SetY(r.currentY + cellPadding + float64(li)*lineHeight)
				r.cell(line)
			}
		}
		r.currentY += rowH
	}

	// Render header
	renderHeader := func() {
		if len(headerCells) == 0 {
			return
		}
		rowH, wrappedTexts := calcRowHeight(headerCells, true)
		r.checkPageBreak(rowH)
		drawRow(wrappedTexts, rowH, true, 0)
		r.setFont("Arial", "", 11)
	}

	renderHeader()

	// Render data rows
	for rowIdx, row := range t.Rows {
		rowH, wrappedTexts := calcRowHeight(row.Cells, false)

		if r.currentY+rowH > r.pageHeight-r.marginBottom {
			r.addPage()
			r.currentY = r.marginTop
			renderHeader()
		}

		drawRow(wrappedTexts, rowH, false, rowIdx)
	}

	r.currentY += 8
	r.pdf.SetStrokeColor(0, 0, 0)
	r.pdf.SetTextColor(0, 0, 0)
}

func (r *PdfRenderer) renderBlockquote(bq ast.Blockquote) {
	indent := 20.0
	lineHeight := 16.0

	for _, elem := range bq.Elements {
		switch e := elem.(type) {
		case ast.Paragraph:
			r.checkPageBreak(lineHeight)
			startY := r.currentY

			r.setFont("Arial", "I", 11)
			text := r.runsToPlainText(e.Runs)

			words := strings.Fields(text)
			if len(words) > 0 {
				currentLine := ""
				remainingWidth := r.contentWidth - indent

				for _, word := range words {
					testLine := currentLine
					if testLine != "" {
						testLine += " "
					}
					testLine += word

					testWidth, _ := r.pdf.MeasureTextWidth(testLine)
					if testWidth > remainingWidth && currentLine != "" {
						r.checkPageBreak(lineHeight)
						r.pdf.SetX(r.marginLeft + indent)
						r.pdf.SetY(r.currentY)
						r.cell(currentLine)
						r.currentY += lineHeight
						currentLine = word
					} else {
						currentLine = testLine
					}
				}

				if currentLine != "" {
					r.checkPageBreak(lineHeight)
					r.pdf.SetX(r.marginLeft + indent)
					r.pdf.SetY(r.currentY)
					r.cell(currentLine)
					r.currentY += lineHeight
				}
			}

			endY := r.currentY

			r.pdf.SetStrokeColor(180, 180, 180)
			r.pdf.SetLineWidth(3)
			r.line(r.marginLeft+5, startY, r.marginLeft+5, endY)
			r.pdf.SetLineWidth(0.5)

			r.currentY += 4
			r.setFont("Arial", "", 11)

		case ast.Blockquote:
			r.renderBlockquote(e)

		case ast.List:
			r.renderList(e)
		}
	}

	r.pdf.SetStrokeColor(0, 0, 0)
}

func (r *PdfRenderer) renderHorizontalRule() {
	r.currentY += 12
	r.checkPageBreak(1)

	r.pdf.SetStrokeColor(180, 180, 180)
	r.pdf.SetLineWidth(1)
	r.line(r.marginLeft, r.currentY, r.marginLeft+r.contentWidth, r.currentY)

	r.currentY += 12
	r.pdf.SetStrokeColor(0, 0, 0)
	r.pdf.SetLineWidth(0.5)
}

func (r *PdfRenderer) renderImage(img ast.Image) {
	// Try to embed the actual image; fall back to a text placeholder on failure.
	if img.URL != "" {
		if err := r.embedImage(img); err == nil {
			return
		}
	}
	r.renderImagePlaceholder(img)
}

// embedImage loads the image referenced by img.URL and draws it into the PDF,
// scaling it to fit the content width. Returns an error if the image cannot be
// loaded or is in an unsupported format.
func (r *PdfRenderer) embedImage(img ast.Image) error {
	data, displayWidth, err := loadRasterImage(img.URL, r.opts, img.Width)
	if err != nil {
		return err
	}

	// Determine pixel dimensions so we can preserve the aspect ratio.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid image dimensions")
	}

	holder, err := gopdf.ImageHolderByBytes(data)
	if err != nil {
		return err
	}

	// Convert to points assuming 96 DPI, honoring an explicit width hint, then
	// clamp to the content width.
	const pxToPt = 72.0 / 96.0
	aspect := float64(cfg.Height) / float64(cfg.Width)
	var w float64
	if displayWidth > 0 {
		w = float64(displayWidth) * pxToPt
	} else {
		w = float64(cfg.Width) * pxToPt
	}
	h := w * aspect
	if w > r.contentWidth {
		w = r.contentWidth
		h = w * aspect
	}

	// Cap the height to the usable page height.
	maxHeight := r.pageHeight - r.marginTop - r.marginBottom
	if h > maxHeight {
		scale := maxHeight / h
		h = maxHeight
		w *= scale
	}

	r.checkPageBreak(h)
	if err := r.image(holder, r.marginLeft, r.currentY, w, h); err != nil {
		return err
	}
	r.currentY += h + 8

	// Render the alt text as a caption when present.
	if img.Alt != "" {
		r.setFont("Arial", "I", 9)
		r.pdf.SetTextColor(74, 74, 104)
		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(img.Alt)
		r.currentY += 14
		r.pdf.SetTextColor(0, 0, 0)
		r.setFont("Arial", "", 11)
	}

	return nil
}

// loadImageBytes resolves url to raw image bytes. It supports data: URIs,
// http(s) URLs, and local file paths (resolved against baseDir). It is shared
// by the PDF and DOCX renderers.
func loadImageBytes(url string, opts Options) ([]byte, error) {
	baseDir := opts.BaseDir
	switch {
	case strings.HasPrefix(url, "data:"):
		idx := strings.Index(url, ",")
		if idx < 0 {
			return nil, fmt.Errorf("malformed data URI")
		}
		meta, payload := url[5:idx], url[idx+1:]
		if strings.Contains(meta, "base64") {
			return base64.StdEncoding.DecodeString(payload)
		}
		return []byte(payload), nil

	case IsRemoteURL(url):
		if !opts.RemoteImages {
			return nil, ErrRemoteImageSkipped
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch image: status %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes))

	default:
		path := url
		if !filepath.IsAbs(path) && baseDir != "" {
			path = filepath.Join(baseDir, path)
		}
		return readLocalImage(path)
	}
}

// readLocalImage reads an image file from disk. Only regular files up to
// MaxImageBytes are read: a path to a device or pipe (/dev/zero, a FIFO) would
// otherwise be read without end.
func readLocalImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("image %s is not a regular file", path)
	}
	if info.Size() > MaxImageBytes {
		return nil, fmt.Errorf("image %s is larger than %d MB", path, MaxImageBytes/(1024*1024))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, MaxImageBytes))
}

// MaxImageBytes caps the size of an image, downloaded or local (20MB).
const MaxImageBytes = 20 * 1024 * 1024

// renderImagePlaceholder draws the original text placeholder used when an image
// cannot be embedded (e.g. unreachable URL or unsupported format).
func (r *PdfRenderer) renderImagePlaceholder(img ast.Image) {
	lineHeight := 16.0
	r.checkPageBreak(lineHeight * 2)

	// Render image as placeholder with alt text and URL
	r.setFont("Arial", "I", 11)
	r.pdf.SetTextColor(74, 74, 104) // Muted color for placeholder

	displayText := img.Alt
	if displayText == "" {
		displayText = "[Image]"
	}

	r.pdf.SetX(r.marginLeft)
	r.pdf.SetY(r.currentY)
	r.cell("[IMG] " + displayText)
	r.currentY += lineHeight

	// Show URL
	if img.URL != "" {
		r.setFont("Arial", "", 9)
		r.pdf.SetTextColor(37, 99, 235) // Link color

		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(img.URL)
		r.currentY += lineHeight
	}

	r.currentY += 8
	r.pdf.SetTextColor(0, 0, 0)
	r.setFont("Arial", "", 11)
}

func (r *PdfRenderer) renderAlert(alert ast.Alert) {
	bgColor, borderColor := getAlertColorsRGB(alert.Type)
	lineHeight := 16.0
	padding := 8.0

	// Wrap the content first so the box can be sized to it
	r.setFont("Arial", "", 11)
	var contentLines []string
	for _, elem := range alert.Elements {
		if p, ok := elem.(ast.Paragraph); ok {
			wrapped := r.wrapText(r.runsToPlainText(p.Runs), r.contentWidth-2*padding)
			contentLines = append(contentLines, wrapped...)
		}
	}
	blockHeight := float64(len(contentLines)+1)*lineHeight + 2*padding
	r.checkPageBreak(blockHeight)

	startY := r.currentY

	// Draw background
	r.pdf.SetFillColor(bgColor.R, bgColor.G, bgColor.B)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "F")

	// Draw colored left border
	r.pdf.SetStrokeColor(borderColor.R, borderColor.G, borderColor.B)
	r.pdf.SetLineWidth(3)
	r.line(r.marginLeft, startY, r.marginLeft, startY+blockHeight)
	r.pdf.SetLineWidth(0.5)

	// Title
	r.setFont("Arial", "B", 11)
	r.pdf.SetTextColor(borderColor.R, borderColor.G, borderColor.B)
	r.pdf.SetX(r.marginLeft + padding)
	r.pdf.SetY(startY + padding)
	r.cell(stripEmojis(alert.Title))
	r.currentY = startY + padding + lineHeight

	// Content
	r.setFont("Arial", "", 11)
	r.pdf.SetTextColor(26, 26, 46)
	for _, line := range contentLines {
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(r.currentY)
		r.cell(line)
		r.currentY += lineHeight
	}

	r.currentY = startY + blockHeight + 8
	r.pdf.SetStrokeColor(0, 0, 0)
	r.pdf.SetTextColor(0, 0, 0)
}

// wrapText breaks text into lines that fit maxWidth in the current font.
func (r *PdfRenderer) wrapText(text string, maxWidth float64) []string {
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if w, _ := r.pdf.MeasureTextWidth(candidate); w > maxWidth && current != "" {
			lines = append(lines, current)
			current = word
		} else {
			current = candidate
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func (r *PdfRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) {
	lineHeight := 12.0
	padding := 8.0

	lines := strings.Split(diagram.Source, "\n")
	blockHeight := float64(len(lines)+1)*lineHeight + 2*padding // +1 for label
	r.checkPageBreak(blockHeight)

	startY := r.currentY
	bg := config.ColorMermaidBgRGB
	border := config.ColorMermaidBorderRGB
	textColor := config.ColorMermaidTextRGB

	// Background
	r.pdf.SetFillColor(bg.R, bg.G, bg.B)
	r.pdf.SetStrokeColor(border.R, border.G, border.B)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	// Label
	r.setFont("Arial", "B", 11)
	r.pdf.SetTextColor(textColor.R, textColor.G, textColor.B)
	r.pdf.SetX(r.marginLeft + padding)
	r.pdf.SetY(startY + padding)
	r.cell("Mermaid Diagram")

	// Source code
	r.setFont("Courier", "", 9)
	codeY := startY + padding + lineHeight
	for _, line := range lines {
		if line == "" {
			line = " "
		}
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(codeY)
		r.cell(line)
		codeY += lineHeight
	}

	r.currentY = startY + blockHeight + 8
	r.setFont("Arial", "", 11)
	r.pdf.SetStrokeColor(0, 0, 0)
	r.pdf.SetTextColor(0, 0, 0)
}

func (r *PdfRenderer) renderMathBlock(math ast.MathBlock) {
	lineHeight := 16.0
	r.checkPageBreak(lineHeight)

	r.setFont("Arial", "I", 11)
	c := config.ColorMathTextRGB
	r.pdf.SetTextColor(c.R, c.G, c.B)

	// Center the math expression
	textWidth, _ := r.pdf.MeasureTextWidth(math.Expression)
	x := r.marginLeft + (r.contentWidth-textWidth)/2
	if x < r.marginLeft {
		x = r.marginLeft
	}

	r.pdf.SetX(x)
	r.pdf.SetY(r.currentY)
	r.cell(math.Expression)
	r.currentY += lineHeight + 8

	r.setFont("Arial", "", 11)
	r.pdf.SetTextColor(0, 0, 0)
}

func (r *PdfRenderer) renderDescriptionList(dl ast.DescriptionList) {
	lineHeight := 16.0
	indent := 20.0

	for _, item := range dl.Items {
		r.checkPageBreak(lineHeight)

		// Term - bold
		r.setFont("Arial", "B", 11)
		r.pdf.SetTextColor(26, 26, 46)
		text := r.runsToPlainText(item.Term)
		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(text)
		r.currentY += lineHeight

		// Definitions - indented
		r.setFont("Arial", "", 11)
		for _, def := range item.Definitions {
			r.checkPageBreak(lineHeight)
			text := r.runsToPlainText(def)
			r.pdf.SetX(r.marginLeft + indent)
			r.pdf.SetY(r.currentY)
			r.cell(text)
			r.currentY += lineHeight
		}
		r.currentY += 4
	}
	r.currentY += 4
}

func (r *PdfRenderer) renderTableOfContents(toc ast.TableOfContents) {
	lineHeight := 16.0

	// Title
	r.currentY += 8
	r.checkPageBreak(lineHeight)
	r.setFont("Arial", "B", 16)
	r.pdf.SetTextColor(26, 26, 46)
	r.pdf.SetX(r.marginLeft)
	r.pdf.SetY(r.currentY)
	r.cell("Table of Contents")
	r.currentY += lineHeight + 8

	// Items
	r.setFont("Arial", "", 11)
	r.pdf.SetTextColor(37, 99, 235) // Link color
	for _, item := range toc.Items {
		r.checkPageBreak(lineHeight)
		indent := float64(item.Level-1) * 20.0
		r.pdf.SetX(r.marginLeft + indent)
		r.pdf.SetY(r.currentY)
		r.cell(item.Title)
		r.currentY += lineHeight
	}

	r.currentY += 8
	r.setFont("Arial", "", 11)
	r.pdf.SetTextColor(0, 0, 0)
}

func (r *PdfRenderer) renderFrontMatter(fm ast.FrontMatter) {
	lineHeight := 16.0
	padding := 8.0

	// Count fields
	fieldCount := 0
	if fm.Title != "" {
		fieldCount++
	}
	if fm.Author != "" {
		fieldCount++
	}
	if fm.Date != "" {
		fieldCount++
	}
	for key := range fm.Raw {
		if key != "title" && key != "author" && key != "date" {
			fieldCount++
		}
	}
	if fieldCount == 0 {
		return
	}

	blockHeight := float64(fieldCount)*lineHeight + 2*padding
	r.checkPageBreak(blockHeight)
	startY := r.currentY

	// Background
	r.pdf.SetFillColor(248, 249, 252) // Elevated surface
	r.pdf.SetStrokeColor(226, 228, 235)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	renderField := func(label, value string) {
		if value == "" {
			return
		}
		r.setFont("Arial", "B", 11)
		r.pdf.SetTextColor(74, 74, 104) // Secondary
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(r.currentY)
		r.cell(label + ": ")
		labelWidth, _ := r.pdf.MeasureTextWidth(label + ": ")

		r.setFont("Arial", "", 11)
		r.pdf.SetTextColor(26, 26, 46) // Primary
		r.pdf.SetX(r.marginLeft + padding + labelWidth)
		r.pdf.SetY(r.currentY)
		r.cell(value)
		r.currentY += lineHeight
	}

	r.currentY = startY + padding
	renderField("Title", fm.Title)
	renderField("Author", fm.Author)
	renderField("Date", fm.Date)

	for key, value := range fm.Raw {
		if key == "title" || key == "author" || key == "date" {
			continue
		}
		if strVal, ok := value.(string); ok {
			renderField(key, strVal)
		}
	}

	r.currentY = startY + blockHeight + 8
	r.pdf.SetStrokeColor(0, 0, 0)
	r.pdf.SetTextColor(0, 0, 0)
	r.setFont("Arial", "", 11)
}

// getAlertColorsRGB returns background and border colors for an alert type
func getAlertColorsRGB(alertType ast.AlertType) (bg, border config.RGB) {
	switch alertType {
	case ast.AlertNote:
		return config.ColorAlertNoteBgRGB, config.ColorAlertNoteBorderRGB
	case ast.AlertTip:
		return config.ColorAlertTipBgRGB, config.ColorAlertTipBorderRGB
	case ast.AlertImportant:
		return config.ColorAlertImportantBgRGB, config.ColorAlertImportantBorderRGB
	case ast.AlertCaution:
		return config.ColorAlertCautionBgRGB, config.ColorAlertCautionBorderRGB
	case ast.AlertWarning:
		return config.ColorAlertWarningBgRGB, config.ColorAlertWarningBorderRGB
	default:
		return config.ColorAlertNoteBgRGB, config.ColorAlertNoteBorderRGB
	}
}

// hexStringToRGB converts a hex color string to RGB values
func hexStringToRGB(hex string) config.RGB {
	var r, g, b uint8
	switch len(hex) {
	case 3:
		fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
		r = r*16 + r
		g = g*16 + g
		b = b*16 + b
	case 6:
		fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	case 8:
		fmt.Sscanf(hex[:6], "%02x%02x%02x", &r, &g, &b) // Ignore alpha
	}
	return config.RGB{R: r, G: g, B: b}
}

func (r *PdfRenderer) runsToPlainText(runs []ast.InlineRun) string {
	var sb strings.Builder
	for _, run := range runs {
		if run.FootnoteIndex > 0 {
			// Add superscript footnote reference
			sb.WriteString("[")
			sb.WriteString(itoa(run.FootnoteIndex))
			sb.WriteString("]")
		} else {
			// Strip emojis that can't be rendered
			sb.WriteString(stripEmojis(run.Text))
		}
	}
	return sb.String()
}

// itoa converts int to string without importing strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (r *PdfRenderer) renderFootnoteSection() {
	lineHeight := 14.0

	// Add separator
	r.currentY += 20
	r.checkPageBreak(lineHeight)

	r.pdf.SetStrokeColor(180, 180, 180)
	r.pdf.SetLineWidth(0.5)
	r.line(r.marginLeft, r.currentY, r.marginLeft+r.contentWidth*0.3, r.currentY)
	r.currentY += 10

	// Render each footnote
	r.setFont("Arial", "", 9)
	for _, fn := range r.footnotes {
		r.checkPageBreak(lineHeight)

		// Render footnote number
		prefix := itoa(fn.Index) + ". "
		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(prefix)

		prefixWidth, _ := r.pdf.MeasureTextWidth(prefix)

		// Render footnote content
		for _, elem := range fn.Elements {
			if p, ok := elem.(ast.Paragraph); ok {
				text := r.runsToPlainText(p.Runs)
				r.pdf.SetX(r.marginLeft + prefixWidth)
				r.pdf.SetY(r.currentY)

				// Word wrap
				words := strings.Fields(text)
				remainingWidth := r.contentWidth - prefixWidth
				currentLine := ""
				firstLine := true

				for _, word := range words {
					testLine := currentLine
					if testLine != "" {
						testLine += " "
					}
					testLine += word

					testWidth, _ := r.pdf.MeasureTextWidth(testLine)
					maxWidth := remainingWidth
					if !firstLine {
						maxWidth = r.contentWidth - 20 // Indent continuation
					}

					if testWidth > maxWidth && currentLine != "" {
						if firstLine {
							r.cell(currentLine)
							r.currentY += lineHeight
							firstLine = false
						} else {
							r.checkPageBreak(lineHeight)
							r.pdf.SetX(r.marginLeft + 20)
							r.pdf.SetY(r.currentY)
							r.cell(currentLine)
							r.currentY += lineHeight
						}
						currentLine = word
					} else {
						currentLine = testLine
					}
				}

				if currentLine != "" {
					if firstLine {
						r.cell(currentLine)
					} else {
						r.checkPageBreak(lineHeight)
						r.pdf.SetX(r.marginLeft + 20)
						r.pdf.SetY(r.currentY)
						r.cell(currentLine)
					}
					r.currentY += lineHeight
				}
			}
		}
		r.currentY += 4 // Space between footnotes
	}

	r.setFont("Arial", "", 11)
	r.pdf.SetStrokeColor(0, 0, 0)
}

// calcColumnWidthsPt computes column widths in points for PDF tables. Each
// column gets at least enough width for its longest unbreakable word (so text
// never overflows into the neighbouring cell), and the remaining space is
// distributed in proportion to each column's content length.
func calcColumnWidthsPt(t ast.Table, numCols int, totalWidth float64) []float64 {
	const (
		minCol  = 40.0 // minimum ~14mm per column in points
		charW   = 5.5  // approximate point width per character at 11pt
		cellPad = 4.0  // matches renderTable's cellPadding
	)

	// Per-column content length (for proportioning) and longest single word
	// (for the minimum width that prevents horizontal overflow).
	maxLen := make([]int, numCols)
	maxWord := make([]int, numCols)
	consider := func(cell ast.TableCell, j int) {
		if l := cellTextLen(cell); l > maxLen[j] {
			maxLen[j] = l
		}
		if w := cellLongestWord(cell); w > maxWord[j] {
			maxWord[j] = w
		}
	}
	for j := 0; j < numCols; j++ {
		if j < len(t.Header.Cells) {
			consider(t.Header.Cells[j], j)
		}
		for _, row := range t.Rows {
			if j < len(row.Cells) {
				consider(row.Cells[j], j)
			}
		}
		if maxLen[j] < 1 {
			maxLen[j] = 1
		}
	}

	// Minimum width per column, capped so one very long token can't claim the
	// whole table.
	minW := make([]float64, numCols)
	sumMin := 0.0
	for j := 0; j < numCols; j++ {
		m := float64(maxWord[j])*charW + 2*cellPad
		if m < minCol {
			m = minCol
		}
		if capW := totalWidth * 0.7; m > capW {
			m = capW
		}
		minW[j] = m
		sumMin += m
	}

	widths := make([]float64, numCols)

	// If the minimums already exceed the available width, scale them to fit.
	if sumMin >= totalWidth {
		for j := 0; j < numCols; j++ {
			widths[j] = minW[j] / sumMin * totalWidth
		}
		return widths
	}

	// Give each column its minimum and distribute the slack by content length.
	extra := totalWidth - sumMin
	totalChars := 0
	for _, l := range maxLen {
		totalChars += l
	}
	for j := 0; j < numCols; j++ {
		share := 0.0
		if totalChars > 0 {
			share = extra * float64(maxLen[j]) / float64(totalChars)
		}
		widths[j] = minW[j] + share
	}
	return widths
}

// cellLongestWord returns the length (in runes) of the longest whitespace-
// delimited token in a table cell, used to size columns so unbreakable tokens
// (paths, identifiers) don't overflow.
func cellLongestWord(cell ast.TableCell) int {
	longest := 0
	for _, run := range cell.Runs {
		for _, w := range strings.Fields(run.Text) {
			if l := len([]rune(w)); l > longest {
				longest = l
			}
		}
	}
	return longest
}

// RenderPdfToFile is a convenience function
func RenderPdfToFile(doc *ast.Document, filename string) error {
	renderer := NewPdfRenderer()
	return renderer.RenderToFile(doc, filename)
}

// RenderPdfToFileWithBaseDir renders to PDF, resolving relative image paths
// against baseDir (typically the directory of the source Markdown file).
func RenderPdfToFileWithBaseDir(doc *ast.Document, filename, baseDir string) error {
	return RenderPdf(doc, filename, Options{BaseDir: baseDir})
}

// RenderPdf renders to PDF with the given options.
func RenderPdf(doc *ast.Document, filename string, opts Options) error {
	renderer := NewPdfRenderer()
	renderer.opts = opts
	return renderer.RenderToFile(doc, filename)
}
