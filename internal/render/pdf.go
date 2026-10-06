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
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/signintech/gopdf"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
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
	t            theme.Theme              // how everything looks
	families     map[string]*pdfFamily    // fonts loaded for this document

	// trace, when set, receives one line for everything drawn: the page, the
	// position, the font and the text. Tests compare it with a recorded
	// layout to catch changes in the output.
	trace *strings.Builder
	page  int
	font  string

	// Current drawing state, kept so the trace can say how things look.
	text, fill, stroke theme.Color
	strokeWidth        float64
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
	r.t = r.opts.theme().PDF
	r.pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: r.t.Page.Width, H: r.t.Page.Height}})
	r.footnotes = nil // Reset footnotes
	r.page = 0
	// gopdf starts with black text and strokes, a thin line, and no fill set.
	r.text, r.fill, r.stroke, r.strokeWidth = theme.Black, theme.Black, theme.Black, 1

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
	page := r.t.Page
	r.pageWidth, r.pageHeight = page.Width, page.Height
	r.marginLeft, r.marginRight = page.MarginLeft, page.MarginRight
	r.marginTop, r.marginBottom = page.MarginTop, page.MarginBottom
	r.contentWidth = page.ContentWidth()

	if err := r.loadFonts(); err != nil {
		return err
	}

	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	return nil
}

func (r *PdfRenderer) bodyFont() string    { return r.t.Fonts.Body }
func (r *PdfRenderer) headingFont() string { return r.t.Fonts.Heading }
func (r *PdfRenderer) codeFont() string    { return r.t.Fonts.Code }

// setFont selects a font by the theme's name for its family ("sans", "mono",
// ...), a style ("", "B", "I" or "BI") and a size.
func (r *PdfRenderer) setFont(family string, style string, size float64) {
	fontName := r.family(family).fontName(style)
	r.pdf.SetFont(fontName, "", size)
	r.font = fmt.Sprintf("%s/%g", fontName, size)
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

func (r *PdfRenderer) textColor(c theme.Color) {
	r.text = c
	r.pdf.SetTextColor(c.R, c.G, c.B)
}

func (r *PdfRenderer) fillColor(c theme.Color) {
	r.fill = c
	r.pdf.SetFillColor(c.R, c.G, c.B)
}

func (r *PdfRenderer) strokeColor(c theme.Color) {
	r.stroke = c
	r.pdf.SetStrokeColor(c.R, c.G, c.B)
}

func (r *PdfRenderer) lineWidth(w float64) {
	r.strokeWidth = w
	r.pdf.SetLineWidth(w)
}

func (r *PdfRenderer) cell(text string) {
	r.logf("text  x=%.1f y=%.1f %s %q %s", r.pdf.GetX(), r.pdf.GetY(), r.font, text, r.text)
	r.pdf.Cell(nil, text)
}

func (r *PdfRenderer) rect(x, y, w, h float64, style string) {
	colors := ""
	if strings.Contains(style, "F") {
		colors += " fill=" + r.fill.String()
	}
	if strings.Contains(style, "D") {
		colors += fmt.Sprintf(" stroke=%s/%.1f", r.stroke, r.strokeWidth)
	}
	r.logf("rect  x=%.1f y=%.1f w=%.1f h=%.1f%s", x, y, w, h, colors)
	r.pdf.RectFromUpperLeftWithStyle(x, y, w, h, style)
}

func (r *PdfRenderer) line(x1, y1, x2, y2 float64) {
	r.logf("line  x=%.1f y=%.1f to x=%.1f y=%.1f %s/%.1f", x1, y1, x2, y2, r.stroke, r.strokeWidth)
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
	level := h.Level
	if level < 1 {
		level = 1
	}
	if level > len(r.t.Heading) {
		level = len(r.t.Heading)
	}
	hd := r.t.Heading[level-1]

	r.currentY += hd.SpaceBefore
	r.checkPageBreak(hd.LineHeight)

	style := textStyle{font: r.headingFont(), size: hd.Size, bold: true, color: hd.Color, lineHeight: hd.LineHeight}
	r.drawLines(r.layout(h.Runs, style, r.contentWidth, r.contentWidth), r.marginLeft, r.marginLeft, hd.LineHeight)

	r.currentY += hd.SpaceAfter
}

func (r *PdfRenderer) renderParagraph(p ast.Paragraph) {
	style := r.bodyStyle()
	r.checkPageBreak(style.lineHeight)

	r.renderRuns(p.Runs, style, r.marginLeft, r.contentWidth)

	r.currentY += r.t.Text.ParagraphSpacing
}

func (r *PdfRenderer) renderList(l ast.List) {
	r.listCounters = make(map[int]int)
	r.renderListItems(l.Items, l.Ordered, 0)
	r.currentY += r.t.List.SpaceAfter
}

func (r *PdfRenderer) renderListItems(items []ast.ListItem, ordered bool, level int) {
	indent := float64(level+1) * r.t.List.Indent
	style := r.bodyStyle()
	lineHeight := style.lineHeight

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

		r.setFont(r.bodyFont(), "", r.t.Text.Size)
		r.textColor(r.t.Text.Color)

		bulletText := prefix + " "
		r.pdf.SetX(r.marginLeft + indent)
		r.pdf.SetY(r.currentY)
		r.cell(bulletText)

		bulletWidth, _ := r.pdf.MeasureTextWidth(bulletText)

		// The first line starts after the bullet; the following lines start
		// under it.
		lines := r.layout(item.Runs, style, r.contentWidth-indent-bulletWidth, r.contentWidth-indent)
		if len(lines) == 0 {
			r.currentY += lineHeight
		} else {
			r.drawLines(lines, r.marginLeft+indent+bulletWidth, r.marginLeft+indent, lineHeight)
		}

		if len(item.Children) > 0 {
			r.renderListItems(item.Children, ordered, level+1)
		}
	}
}

func (r *PdfRenderer) renderCodeBlock(cb ast.CodeBlock) {
	code := r.t.Code
	lineHeight := code.BlockLineHeight
	padding := code.Padding

	// The code ends with a line break, which is not a line of its own
	lines := strings.Split(strings.TrimSuffix(cb.Code, "\n"), "\n")
	blockHeight := float64(len(lines))*lineHeight + 2*padding

	r.checkPageBreak(blockHeight)

	r.fillColor(code.Background)
	r.strokeColor(code.Border)

	startY := r.currentY
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	r.setFont(r.codeFont(), "", code.BlockSize)
	r.textColor(code.BlockColor)

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

	r.currentY = startY + blockHeight + code.SpaceAfter
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.strokeColor(theme.Black)
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
	table := r.t.Table
	cellPadding := table.CellPadding
	lineHeight := table.LineHeight
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

	// layoutRow lays out every cell of a row and returns the row height
	// needed for the tallest one.
	layoutRow := func(cells []ast.TableCell, bold bool) (float64, [][]textLine) {
		style := r.bodyStyle()
		style.bold, style.lineHeight = bold, lineHeight
		maxLines := 1
		laidOut := make([][]textLine, numCols)
		for j := 0; j < numCols; j++ {
			if j >= len(cells) {
				continue
			}
			innerWidth := colWidthsPt[j] - 2*cellPadding
			laidOut[j] = r.layout(cells[j].Runs, style, innerWidth, innerWidth)
			if len(laidOut[j]) > maxLines {
				maxLines = len(laidOut[j])
			}
		}
		h := float64(maxLines)*lineHeight + 2*cellPadding
		if h < minRowHeight {
			h = minRowHeight
		}
		return h, laidOut
	}

	// drawRow renders backgrounds + text for one row. All cell backgrounds are
	// drawn first, then all text, so any text that overflows a narrow column is
	// not painted over by the next column's background.
	drawRow := func(laidOut [][]textLine, rowH float64, isHeader bool, rowIdx int) {
		r.strokeColor(table.Border)

		if isHeader {
			r.fillColor(table.HeaderBackground)
		} else if rowIdx%2 == 1 {
			r.fillColor(table.StripeBackground)
		} else {
			r.fillColor(table.RowBackground)
		}
		for j := 0; j < numCols; j++ {
			r.rect(colX(j), r.currentY, colWidthsPt[j], rowH, "FD")
		}

		for j := 0; j < numCols; j++ {
			x := colX(j) + cellPadding
			r.drawLinesAt(laidOut[j], x, x, r.currentY+cellPadding, lineHeight)
		}
		r.resetText()
		r.currentY += rowH
	}

	// Render header
	renderHeader := func() {
		if len(headerCells) == 0 {
			return
		}
		rowH, laidOut := layoutRow(headerCells, true)
		r.checkPageBreak(rowH)
		drawRow(laidOut, rowH, true, 0)
	}

	renderHeader()

	// Render data rows
	for rowIdx, row := range t.Rows {
		rowH, laidOut := layoutRow(row.Cells, false)

		if r.currentY+rowH > r.pageHeight-r.marginBottom {
			r.addPage()
			r.currentY = r.marginTop
			renderHeader()
		}

		drawRow(laidOut, rowH, false, rowIdx)
	}

	r.currentY += table.SpaceAfter
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderBlockquote(bq ast.Blockquote) {
	quote := r.t.Quote
	indent := quote.Indent
	style := r.bodyStyle()
	style.italic = quote.Italic
	lineHeight := style.lineHeight

	for _, elem := range bq.Elements {
		switch e := elem.(type) {
		case ast.Paragraph:
			r.checkPageBreak(lineHeight)
			startY := r.currentY

			r.renderRuns(e.Runs, style, r.marginLeft+indent, r.contentWidth-indent)

			endY := r.currentY

			r.strokeColor(quote.Bar)
			r.lineWidth(quote.BarWidth)
			r.line(r.marginLeft+5, startY, r.marginLeft+5, endY)
			r.lineWidth(0.5)

			r.currentY += quote.SpaceAfter
			r.setFont(r.bodyFont(), "", r.t.Text.Size)

		case ast.Blockquote:
			r.renderBlockquote(e)

		case ast.List:
			r.renderList(e)
		}
	}

	r.strokeColor(theme.Black)
}

func (r *PdfRenderer) renderHorizontalRule() {
	rule := r.t.Rule
	r.currentY += rule.Space
	r.checkPageBreak(1)

	r.strokeColor(rule.Color)
	r.lineWidth(rule.Width)
	r.line(r.marginLeft, r.currentY, r.marginLeft+r.contentWidth, r.currentY)

	r.currentY += rule.Space
	r.strokeColor(theme.Black)
	r.lineWidth(0.5)
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
		r.setFont(r.bodyFont(), "I", r.t.Caption.Size)
		r.textColor(r.t.Text.Muted)
		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(img.Alt)
		r.currentY += 14
		r.textColor(theme.Black)
		r.setFont(r.bodyFont(), "", r.t.Text.Size)
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
	lineHeight := r.t.Text.LineHeight
	r.checkPageBreak(lineHeight * 2)

	// Render image as placeholder with alt text and URL
	r.setFont(r.bodyFont(), "I", r.t.Text.Size)
	r.textColor(r.t.Text.Muted)

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
		r.setFont(r.bodyFont(), "", r.t.Caption.Size)
		r.textColor(r.t.Link.Color)

		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(img.URL)
		r.currentY += lineHeight
	}

	r.currentY += 8
	r.textColor(theme.Black)
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
}

func (r *PdfRenderer) renderAlert(alert ast.Alert) {
	colors := alertColors(r.t.Alert, alert.Type)
	bgColor, borderColor := colors.Background, colors.Border
	style := r.bodyStyle()
	lineHeight := style.lineHeight
	padding := r.t.Alert.Padding

	// Lay the content out first so the box can be sized to it
	var contentLines []textLine
	for _, elem := range alert.Elements {
		if p, ok := elem.(ast.Paragraph); ok {
			width := r.contentWidth - 2*padding
			contentLines = append(contentLines, r.layout(p.Runs, style, width, width)...)
		}
	}
	blockHeight := float64(len(contentLines)+1)*lineHeight + 2*padding
	r.checkPageBreak(blockHeight)

	startY := r.currentY

	// Draw background
	r.fillColor(bgColor)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "F")

	// Draw colored left border
	r.strokeColor(borderColor)
	r.lineWidth(r.t.Alert.BarWidth)
	r.line(r.marginLeft, startY, r.marginLeft, startY+blockHeight)
	r.lineWidth(0.5)

	// Title
	r.setFont(r.bodyFont(), "B", r.t.Text.Size)
	r.textColor(borderColor)
	r.pdf.SetX(r.marginLeft + padding)
	r.pdf.SetY(startY + padding)
	r.cell(stripEmojis(alert.Title))
	r.currentY = startY + padding + lineHeight

	// Content
	x := r.marginLeft + padding
	r.drawLinesAt(contentLines, x, x, r.currentY, lineHeight)
	r.resetText()

	r.currentY = startY + blockHeight + r.t.Alert.SpaceAfter
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) {
	lineHeight := r.t.Code.BlockLineHeight
	padding := r.t.Code.Padding

	lines := strings.Split(strings.TrimSuffix(diagram.Source, "\n"), "\n")
	blockHeight := float64(len(lines)+1)*lineHeight + 2*padding // +1 for label
	r.checkPageBreak(blockHeight)

	startY := r.currentY
	bg, border, textColor := r.t.Diagram.Background, r.t.Diagram.Border, r.t.Diagram.Text

	// Background
	r.fillColor(bg)
	r.strokeColor(border)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	// Label
	r.setFont(r.bodyFont(), "B", r.t.Text.Size)
	r.textColor(textColor)
	r.pdf.SetX(r.marginLeft + padding)
	r.pdf.SetY(startY + padding)
	r.cell("Mermaid Diagram")

	// Source code
	r.setFont(r.codeFont(), "", r.t.Code.BlockSize)
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

	r.currentY = startY + blockHeight + r.t.Code.SpaceAfter
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderMathBlock(math ast.MathBlock) {
	lineHeight := r.t.Text.LineHeight
	r.checkPageBreak(lineHeight)

	r.setFont(r.bodyFont(), "I", r.t.Text.Size)
	r.textColor(r.t.Colors.Math)

	// Center the math expression
	textWidth, _ := r.pdf.MeasureTextWidth(math.Expression)
	x := r.marginLeft + (r.contentWidth-textWidth)/2
	if x < r.marginLeft {
		x = r.marginLeft
	}

	r.pdf.SetX(x)
	r.pdf.SetY(r.currentY)
	r.cell(math.Expression)
	r.currentY += lineHeight + r.t.Text.ParagraphSpacing

	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderDescriptionList(dl ast.DescriptionList) {
	def := r.bodyStyle()
	lineHeight := def.lineHeight
	indent := r.t.List.Indent

	for _, item := range dl.Items {
		r.checkPageBreak(lineHeight)

		// Term - bold
		term := def
		term.bold = true
		r.renderRuns(item.Term, term, r.marginLeft, r.contentWidth)

		// Definitions - indented
		for _, runs := range item.Definitions {
			r.checkPageBreak(lineHeight)
			r.renderRuns(runs, def, r.marginLeft+indent, r.contentWidth-indent)
		}
		r.currentY += 4
	}
	r.currentY += 4
}

func (r *PdfRenderer) renderTableOfContents(toc ast.TableOfContents) {
	lineHeight := r.t.Text.LineHeight

	// Title, in the size of a third-level heading
	r.currentY += 8
	r.checkPageBreak(lineHeight)
	r.setFont(r.headingFont(), "B", r.t.Heading[2].Size)
	r.textColor(r.t.Text.Color)
	r.pdf.SetX(r.marginLeft)
	r.pdf.SetY(r.currentY)
	r.cell("Table of Contents")
	r.currentY += lineHeight + 8

	// Items
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.textColor(r.t.Link.Color)
	for _, item := range toc.Items {
		r.checkPageBreak(lineHeight)
		indent := float64(item.Level-1) * r.t.List.Indent
		r.pdf.SetX(r.marginLeft + indent)
		r.pdf.SetY(r.currentY)
		r.cell(item.Title)
		r.currentY += lineHeight
	}

	r.currentY += 8
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderFrontMatter(fm ast.FrontMatter) {
	lineHeight := r.t.Text.LineHeight
	padding := r.t.Alert.Padding

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
	for key, value := range fm.Raw {
		// Only text values are shown; lists and maps have no single line
		if _, ok := value.(string); ok && key != "title" && key != "author" && key != "date" {
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
	r.fillColor(r.t.Box.Background)
	r.strokeColor(r.t.Box.Border)
	r.rect(r.marginLeft, startY, r.contentWidth, blockHeight, "FD")

	renderField := func(label, value string) {
		if value == "" {
			return
		}
		r.setFont(r.bodyFont(), "B", r.t.Text.Size)
		r.textColor(r.t.Text.Muted)
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(r.currentY)
		r.cell(label + ": ")
		labelWidth, _ := r.pdf.MeasureTextWidth(label + ": ")

		r.setFont(r.bodyFont(), "", r.t.Text.Size)
		r.textColor(r.t.Text.Color)
		r.pdf.SetX(r.marginLeft + padding + labelWidth)
		r.pdf.SetY(r.currentY)
		r.cell(value)
		r.currentY += lineHeight
	}

	r.currentY = startY + padding
	renderField("Title", fm.Title)
	renderField("Author", fm.Author)
	renderField("Date", fm.Date)

	for _, key := range sortedKeys(fm.Raw) {
		if key == "title" || key == "author" || key == "date" {
			continue
		}
		if strVal, ok := fm.Raw[key].(string); ok {
			renderField(key, strVal)
		}
	}

	r.currentY = startY + blockHeight + 8
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
}

// alertColors picks the colors for a kind of callout from a theme.
func alertColors(a theme.Alert, kind ast.AlertType) theme.AlertColors {
	switch kind {
	case ast.AlertTip:
		return a.Tip
	case ast.AlertImportant:
		return a.Important
	case ast.AlertCaution:
		return a.Caution
	case ast.AlertWarning:
		return a.Warning
	default:
		return a.Note
	}
}

// hexStringToRGB converts a hex color string to RGB values
func hexStringToRGB(hex string) theme.Color {
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
	return theme.Color{R: r, G: g, B: b}
}

// sortedKeys returns the keys of a metadata map in a stable order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
	note := r.t.Footnote
	lineHeight := note.LineHeight

	// Add separator
	r.currentY += 20
	r.checkPageBreak(lineHeight)

	r.strokeColor(note.Rule)
	r.lineWidth(0.5)
	r.line(r.marginLeft, r.currentY, r.marginLeft+r.contentWidth*0.3, r.currentY)
	r.currentY += 10

	// Render each footnote
	r.setFont(r.bodyFont(), "", note.Size)
	for _, fn := range r.footnotes {
		r.checkPageBreak(lineHeight)

		// Render footnote number
		r.textColor(r.t.Text.Color)
		prefix := itoa(fn.Index) + ". "
		r.pdf.SetX(r.marginLeft)
		r.pdf.SetY(r.currentY)
		r.cell(prefix)

		prefixWidth, _ := r.pdf.MeasureTextWidth(prefix)

		// Render footnote content: the first line follows the number, the
		// rest are indented.
		style := r.bodyStyle()
		style.size, style.lineHeight = note.Size, lineHeight
		for _, elem := range fn.Elements {
			p, ok := elem.(ast.Paragraph)
			if !ok {
				continue
			}
			lines := r.layout(p.Runs, style, r.contentWidth-prefixWidth, r.contentWidth-20)
			r.drawLines(lines, r.marginLeft+prefixWidth, r.marginLeft+20, lineHeight)
			r.setFont(r.bodyFont(), "", note.Size)
		}
		r.currentY += 4 // Space between footnotes
	}

	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.strokeColor(theme.Black)
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
