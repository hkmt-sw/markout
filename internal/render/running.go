package render

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// docInfo is what a header or footer can say about the document.
type docInfo struct {
	title, author, date string
}

// infoOf collects the document's title, author and date from its front
// matter. A document without a title there is called by its first top-level
// heading.
func infoOf(doc *ast.Document) docInfo {
	var info docInfo
	heading := ""
	for _, elem := range doc.Elements {
		switch e := elem.(type) {
		case ast.FrontMatter:
			info.title, info.author, info.date = e.Title, e.Author, e.Date
		case ast.Heading:
			if heading == "" && e.Level == 1 {
				var b strings.Builder
				for _, run := range e.Runs {
					b.WriteString(run.Text)
				}
				heading = strings.Join(strings.Fields(b.String()), " ")
			}
		}
	}
	if info.title == "" {
		info.title = heading
	}
	return info
}

// static fills in the placeholders that are the same on every page.
func (d docInfo) static(text string) string {
	return strings.NewReplacer("{title}", d.title, "{author}", d.author, "{date}", d.date).Replace(text)
}

// expand fills in every placeholder for one page.
func (d docInfo) expand(text string, page, pages int) string {
	text = d.static(text)
	return strings.NewReplacer("{page}", strconv.Itoa(page), "{pages}", strconv.Itoa(pages)).Replace(text)
}

var pagePlaceholder = regexp.MustCompile(`\{pages?\}`)

// pageParts splits text at {page} and {pages}, so a format with live page
// number fields can put a field at each. Every other piece is literal text.
func pageParts(text string) []string {
	var parts []string
	last := 0
	for _, loc := range pagePlaceholder.FindAllStringIndex(text, -1) {
		if loc[0] > last {
			parts = append(parts, text[last:loc[0]])
		}
		parts = append(parts, text[loc[0]:loc[1]])
		last = loc[1]
	}
	if last < len(text) {
		parts = append(parts, text[last:])
	}
	return parts
}

// drawRunning puts the theme's header and footer on every page. It runs
// after the document is laid out, when the number of pages is known.
func (r *PdfRenderer) drawRunning(info docInfo) error {
	if r.t.Header.Empty() && r.t.Footer.Empty() {
		return nil
	}
	pages := r.page
	for page := 1; page <= pages; page++ {
		if err := r.pdf.SetPage(page); err != nil {
			return err
		}
		r.page = page

		// Each sits in the middle of its margin, with the rule on the side of
		// the page's text.
		if h := r.t.Header; !h.Empty() {
			y := (r.marginTop - h.Size) / 2
			r.drawRunningLine(h, info, page, pages, y, y+h.Size+5)
		}
		if f := r.t.Footer; !f.Empty() {
			y := r.pageHeight - (r.marginBottom+f.Size)/2
			r.drawRunningLine(f, info, page, pages, y, y-5)
		}
	}
	r.resetText()
	return nil
}

func (r *PdfRenderer) drawRunningLine(run theme.Running, info docInfo, page, pages int, y, ruleY float64) {
	r.setFont(r.bodyFont(), "", run.Size)
	r.textColor(run.Color)

	put := func(text string, align int) {
		text = r.usable(info.expand(text, page, pages))
		if text == "" {
			return
		}
		width, _ := r.pdf.MeasureTextWidth(text)
		x := r.marginLeft
		switch align {
		case 1:
			x += (r.contentWidth - width) / 2
		case 2:
			x += r.contentWidth - width
		}
		r.pdf.SetX(x)
		r.pdf.SetY(y)
		r.cell(text)
	}
	put(run.Left, 0)
	put(run.Center, 1)
	put(run.Right, 2)

	if run.Rule {
		r.strokeColor(r.t.Rule.Color)
		r.lineWidth(0.5)
		r.line(r.marginLeft, ruleY, r.marginLeft+r.contentWidth, ruleY)
		r.strokeColor(theme.Black)
	}
}
