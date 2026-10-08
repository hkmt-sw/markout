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

	"github.com/signintech/gopdf"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

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

	// What the fonts can draw, and what the document has that they cannot;
	// see pdf_chars.go.
	face        string                      // the font selected, without its size
	glyphs      map[string]map[rune]bool    // by face: whether it has the character
	widths      map[string]map[rune]float64 // by font and size: character widths
	glyphMissed bool
	missing     []rune // characters left out, in order of appearance
	missingSeen map[rune]bool
	rightToLeft bool

	// Bookmarks and links within the document; see pdf_outline.go.
	anchors  anchors         // what links may call the headings
	anchored map[string]bool // headings that have been drawn
	outline  []outlineEntry
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
	r.glyphs, r.widths = map[string]map[rune]bool{}, map[string]map[rune]float64{}
	r.missing, r.missingSeen, r.rightToLeft = nil, nil, false
	r.anchors, _ = collectAnchors(astDoc.Elements)
	r.anchored, r.outline = map[string]bool{}, nil
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

	// Headers and footers go on last, when the page count is known
	if err := r.drawRunning(infoOf(astDoc)); err != nil {
		return err
	}

	// The bookmarks are nested by heading level, which gopdf's outline
	// object has to be told about in the finished file
	top, last := r.nestOutline()
	data, err := r.pdf.GetBytesPdfReturnErr()
	if err != nil {
		return err
	}
	if top > 0 {
		data = fixOutlineRoot(data, top, last)
	}
	if err := os.WriteFile(filename, data, 0o666); err != nil {
		return err
	}
	if r.opts.Warn != nil {
		for _, w := range r.warnings() {
			r.opts.Warn(w)
		}
	}
	return nil
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
	r.face = fontName
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
	text = r.usable(text)
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
	r.markHeading(h, level)

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

		if len(item.Blocks) > 0 {
			r.renderItemBlocks(item.Blocks, indent+bulletWidth)
		}

		if len(item.Children) > 0 {
			// A nested list is numbered or not by itself, and counts from one
			r.listCounters[level+1] = 0
			r.renderListItems(item.Children, item.ChildrenOrdered, level+1)
		}
	}
}

// renderItemBlocks draws what a list item holds besides its text (code,
// quotes, tables, more paragraphs) under that text, between margins moved in
// by inset to where the text starts.
func (r *PdfRenderer) renderItemBlocks(blocks []ast.Element, inset float64) {
	left, width, counters := r.marginLeft, r.contentWidth, r.listCounters
	r.marginLeft, r.contentWidth = left+inset, width-inset
	r.currentY += r.t.Text.ParagraphSpacing / 2
	for _, block := range blocks {
		r.renderElement(block)
	}
	// A list inside one of the blocks counts its own items
	r.marginLeft, r.contentWidth, r.listCounters = left, width, counters
}

func (r *PdfRenderer) renderCodeBlock(cb ast.CodeBlock) {
	code := r.t.Code
	padding := code.Padding

	// Code in a language the block names is colored by what it means
	r.setFont(r.codeFont(), "", code.BlockSize)
	lines := r.wrapCode(highlight(cb, r.t), r.contentWidth-2*padding)

	r.panel(len(lines), code.BlockLineHeight, padding, func(y, h float64) {
		r.fillColor(code.Background)
		r.strokeColor(code.Border)
		r.rect(r.marginLeft, y, r.contentWidth, h, "FD")
	}, func(i int, y float64) {
		r.setFont(r.codeFont(), "", code.BlockSize)
		x := r.marginLeft + padding
		for _, span := range lines[i] {
			r.textColor(span.color)
			r.pdf.SetX(x)
			r.pdf.SetY(y)
			r.cell(span.text)
			width, _ := r.pdf.MeasureTextWidth(span.text)
			x += width
		}
	})

	r.currentY += code.SpaceAfter
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
			inner := colWidthsPt[j] - 2*cellPadding
			for li, line := range laidOut[j] {
				// Each line is placed by the column's alignment
				offset := 0.0
				if j < len(t.Alignments) {
					switch t.Alignments[j] {
					case ast.AlignRight:
						offset = inner - line.width()
					case ast.AlignCenter:
						offset = (inner - line.width()) / 2
					}
				}
				if offset < 0 {
					offset = 0
				}
				r.drawLine(line, x+offset, r.currentY+cellPadding+float64(li)*lineHeight, lineHeight)
			}
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
			lines := r.layout(e.Runs, style, r.contentWidth-indent, r.contentWidth-indent)
			if len(lines) == 0 && len(e.Runs) > 0 {
				lines = []textLine{{}} // text that cannot be drawn still takes its line
			}

			// The bar runs beside the text, and stops and starts again where
			// the paragraph goes on to another page.
			bar := func(from, to float64) {
				r.strokeColor(quote.Bar)
				r.lineWidth(quote.BarWidth)
				r.line(r.marginLeft+5, from, r.marginLeft+5, to)
				r.lineWidth(0.5)
			}
			startY := r.currentY
			for _, line := range lines {
				if r.currentY+lineHeight > r.pageHeight-r.marginBottom {
					bar(startY, r.currentY)
					r.addPage()
					r.currentY = r.marginTop
					startY = r.currentY
				}
				r.drawLine(line, r.marginLeft+indent, r.currentY, lineHeight)
				r.currentY += lineHeight
			}
			r.resetText()
			bar(startY, r.currentY)

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
		caption := textStyle{font: r.bodyFont(), size: r.t.Caption.Size, italic: true, color: r.t.Text.Muted, lineHeight: 14}
		r.renderRuns([]ast.InlineRun{{Text: img.Alt}}, caption, r.marginLeft, r.contentWidth)
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
	displayText := img.Alt
	if displayText == "" {
		displayText = "[Image]"
	}
	label := r.bodyStyle()
	label.italic, label.color = true, r.t.Text.Muted
	r.renderRuns([]ast.InlineRun{{Text: "[IMG] " + displayText}}, label, r.marginLeft, r.contentWidth)

	// Show URL
	if img.URL != "" {
		url := r.bodyStyle()
		url.size, url.color = r.t.Caption.Size, r.t.Link.Color
		r.renderRuns([]ast.InlineRun{{Text: img.URL}}, url, r.marginLeft, r.contentWidth)
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

	// The title is the first line of the box
	x := r.marginLeft + padding
	r.panel(len(contentLines)+1, lineHeight, padding, func(y, h float64) {
		r.fillColor(bgColor)
		r.rect(r.marginLeft, y, r.contentWidth, h, "F")

		// Colored left border
		r.strokeColor(borderColor)
		r.lineWidth(r.t.Alert.BarWidth)
		r.line(r.marginLeft, y, r.marginLeft, y+h)
		r.lineWidth(0.5)
	}, func(i int, y float64) {
		if i > 0 {
			r.drawLine(contentLines[i-1], x, y, lineHeight)
			return
		}
		r.setFont(r.bodyFont(), "B", r.t.Text.Size)
		r.textColor(borderColor)
		r.pdf.SetX(x)
		r.pdf.SetY(y)
		r.cell(alert.Title)
	})
	r.resetText()

	r.currentY += r.t.Alert.SpaceAfter
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderMermaidDiagram(diagram ast.MermaidDiagram) {
	padding := r.t.Code.Padding
	bg, border, textColor := r.t.Diagram.Background, r.t.Diagram.Border, r.t.Diagram.Text

	// The label is the first line of the box, the source follows
	r.setFont(r.codeFont(), "", r.t.Code.BlockSize)
	lines := r.codeLines(diagram.Source, r.contentWidth-2*padding)

	r.panel(len(lines)+1, r.t.Code.BlockLineHeight, padding, func(y, h float64) {
		r.fillColor(bg)
		r.strokeColor(border)
		r.rect(r.marginLeft, y, r.contentWidth, h, "FD")
	}, func(i int, y float64) {
		r.textColor(textColor)
		r.pdf.SetX(r.marginLeft + padding)
		r.pdf.SetY(y)
		if i == 0 {
			r.setFont(r.bodyFont(), "B", r.t.Text.Size)
			r.cell("Mermaid Diagram")
			return
		}
		r.setFont(r.codeFont(), "", r.t.Code.BlockSize)
		r.cell(lines[i-1])
	})

	r.currentY += r.t.Code.SpaceAfter
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.strokeColor(theme.Black)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderMathBlock(math ast.MathBlock) {
	lineHeight := r.t.Text.LineHeight

	r.setFont(r.bodyFont(), "I", r.t.Text.Size)
	r.textColor(r.t.Colors.Math)

	// The expression is shown as written, centered; a long one is broken
	// into lines.
	expression := r.usable(strings.ReplaceAll(math.Expression, "\n", " "))
	for _, line := range r.wrapChars(expression, r.contentWidth) {
		r.checkPageBreak(lineHeight)
		textWidth, _ := r.pdf.MeasureTextWidth(line)
		x := r.marginLeft + (r.contentWidth-textWidth)/2
		if x < r.marginLeft {
			x = r.marginLeft
		}

		r.pdf.SetX(x)
		r.pdf.SetY(r.currentY)
		r.cell(line)
		r.currentY += lineHeight
	}
	r.currentY += r.t.Text.ParagraphSpacing

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
	entry := r.bodyStyle()
	entry.color = r.t.Link.Color
	for _, item := range toc.Items {
		indent := float64(item.Level-1) * r.t.List.Indent
		// An entry is a link to its heading
		run := ast.InlineRun{Text: item.Title}
		if _, ok := r.anchors[item.ID]; ok {
			run.Link = "#" + item.ID
		}
		r.renderRuns([]ast.InlineRun{run}, entry, r.marginLeft+indent, r.contentWidth-indent)
	}

	r.currentY += 8
	r.setFont(r.bodyFont(), "", r.t.Text.Size)
	r.textColor(theme.Black)
}

func (r *PdfRenderer) renderFrontMatter(fm ast.FrontMatter) {
	lineHeight := r.t.Text.LineHeight
	padding := r.t.Alert.Padding
	x, width := r.marginLeft+padding, r.contentWidth-2*padding

	// A row is one line of the box: a field's first line starts after its
	// label, the lines of a long value follow under it.
	type row struct {
		label string
		at    float64 // where the value starts on this line
		line  textLine
	}
	var rows []row
	addField := func(label, value string) {
		if value == "" {
			return
		}
		r.setFont(r.bodyFont(), "B", r.t.Text.Size)
		labelWidth, _ := r.pdf.MeasureTextWidth(label + ": ")
		lines := r.layout([]ast.InlineRun{{Text: value}}, r.bodyStyle(), width-labelWidth, width)
		if len(lines) == 0 {
			lines = []textLine{{}}
		}
		for i, line := range lines {
			if i == 0 {
				rows = append(rows, row{label: label + ": ", at: x + labelWidth, line: line})
			} else {
				rows = append(rows, row{at: x, line: line})
			}
		}
	}

	addField("Title", fm.Title)
	addField("Author", fm.Author)
	addField("Date", fm.Date)
	for _, key := range sortedKeys(fm.Raw) {
		if key == "title" || key == "author" || key == "date" {
			continue
		}
		// Only text values are shown; lists and maps have no single line
		if strVal, ok := fm.Raw[key].(string); ok {
			addField(key, strVal)
		}
	}
	if len(rows) == 0 {
		return
	}

	r.panel(len(rows), lineHeight, padding, func(y, h float64) {
		r.fillColor(r.t.Box.Background)
		r.strokeColor(r.t.Box.Border)
		r.rect(r.marginLeft, y, r.contentWidth, h, "FD")
	}, func(i int, y float64) {
		if label := rows[i].label; label != "" {
			r.setFont(r.bodyFont(), "B", r.t.Text.Size)
			r.textColor(r.t.Text.Muted)
			r.pdf.SetX(x)
			r.pdf.SetY(y)
			r.cell(label)
		}
		r.drawLine(rows[i].line, rows[i].at, y, lineHeight)
	})

	r.currentY += 8
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
