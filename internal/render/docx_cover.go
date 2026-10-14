package render

import (
	"strings"

	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/hkmt-sw/markout/internal/ast"
)

// The cover page holds the document's title and subtitle, and under them the
// rest of its control block. The text starts on the page after it.

const (
	// coverTitleStyle and coverSubtitleStyle are Word's own styles of a
	// document's title and subtitle, so that a Word user can restyle them.
	// They are not headings: they are in neither the navigation pane nor a
	// table of contents.
	coverTitleStyle    = "Title"
	coverSubtitleStyle = "Subtitle"

	// coverBreakMark is written as the text of the paragraph that starts the
	// page after the cover, and found again by startNewPage.
	coverBreakMark = "markout-cover-break"
)

// renderCover sets the front matter as the cover page. The title and the
// subtitle are on the page in large type, so the control block leaves them
// out.
func (r *DocxRenderer) renderCover(fm ast.FrontMatter) error {
	c := r.t.Cover
	before := c.Top // space above the first line, whatever it is
	drawn := false
	put := func(text, style string, size float64, bold bool) error {
		if text == "" {
			return nil
		}
		para, err := r.doc.AddParagraph()
		if err != nil {
			return err
		}
		if err := para.SetStyle(style); err != nil {
			return err
		}
		para.SetSpacingBefore(twips(before))
		para.SetSpacingAfter(0)
		base := runBase{size: size, font: r.t.Fonts.Heading, bold: bold, styled: true, styledBold: true}
		if style == coverTitleStyle {
			base.color = c.TitleColor
		} else {
			base.color = c.SubtitleColor
		}
		before, drawn = coverSubtitleGap, true
		return r.addRun(para, ast.InlineRun{Text: text}, base)
	}
	if err := put(r.info.title, coverTitleStyle, c.TitleSize, true); err != nil {
		return err
	}
	if err := put(frontValue(fm, "subtitle"), coverSubtitleStyle, c.SubtitleSize, false); err != nil {
		return err
	}

	// What the page says already is not a row of the block
	omit := []string{"subtitle"}
	if fm.Title != "" {
		omit = append(omit, "title")
	}
	fields := controlFields(fm, r.opts.Lang, r.t.Control.Labels, omit...)
	if len(fields) == 0 {
		return nil
	}
	if drawn {
		// The space under the subtitle, as the space before the table
		if err := r.gap(coverBlockGap); err != nil {
			return err
		}
	} else if c.Top >= 1 {
		if err := r.gap(c.Top); err != nil {
			return err
		}
	}
	return r.renderControlBlock(fields)
}

// endCover starts the page after the cover. A tiny paragraph is added for
// the purpose, whatever comes next (a table cannot start a page by itself),
// and it is the one that starts the page; see startNewPage.
func (r *DocxRenderer) endCover() error {
	p, err := r.doc.AddParagraph()
	if err != nil {
		return err
	}
	p.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingExact, Value: twips(1)})
	run, err := p.AddRun()
	if err != nil {
		return err
	}
	return run.SetText(coverBreakMark)
}

// coverHidesRunning says whether the cover page is to have no header or
// footer, as the first page of the section: it has them only if the theme
// does.
func (r *DocxRenderer) coverHidesRunning() bool {
	return r.cover && (!r.t.Header.Empty() || !r.t.Footer.Empty())
}

// emptyFirstPage gives the section the header or the footer of its first page,
// which is the cover: a single empty paragraph, as a part may not be empty.
func (r *DocxRenderer) emptyFirstPage(section domain.Section, header bool) error {
	var part interface {
		AddParagraph() (domain.Paragraph, error)
	}
	var err error
	if header {
		part, err = section.Header(domain.HeaderFirst)
	} else {
		part, err = section.Footer(domain.FooterFirst)
	}
	if err != nil {
		return err
	}
	_, err = part.AddParagraph()
	return err
}

// startNewPage turns the paragraph marked by endCover into one that starts a
// page, and takes the mark out of it.
func startNewPage(content []byte) []byte {
	str := string(content)
	at := strings.Index(str, coverBreakMark)
	if at < 0 {
		return content
	}
	// The paragraph around the mark, and the run that holds it
	start := strings.LastIndex(str[:at], "<w:p>")
	runStart := strings.LastIndex(str[:at], "<w:r>")
	runEnd := strings.Index(str[at:], "</w:r>")
	if start < 0 || runStart < start || runEnd < 0 {
		return []byte(strings.Replace(str, coverBreakMark, "", 1))
	}
	str = str[:runStart] + str[at+runEnd+len("</w:r>"):]
	// The paragraph has properties of its own (its line height). The schema
	// wants this one among the first of them; the paragraph has no style.
	open := start + len("<w:p>")
	pPr := open + len(str[open:]) - len(strings.TrimLeft(str[open:], " \t\r\n"))
	if strings.HasPrefix(str[pPr:], "<w:pPr>") {
		pPr += len("<w:pPr>")
		str = str[:pPr] + "<w:pageBreakBefore/>" + str[pPr:]
	} else {
		str = str[:open] + "<w:pPr><w:pageBreakBefore/></w:pPr>" + str[open:]
	}
	return []byte(str)
}

// setTitlePage gives the section a first page of its own, as far as headers
// and footers go: the cover page, which has empty ones.
func setTitlePage(content []byte) []byte {
	const end = "</w:sectPr>"
	str := string(content)
	at := strings.LastIndex(str, end)
	if at < 0 {
		return content
	}
	// titlePg comes late in the section's properties: after the page size,
	// margins, columns and the like, before the text direction and the grid
	sect := str[strings.LastIndex(str[:at], "<w:sectPr"):at]
	insert := len(sect)
	for _, later := range []string{"<w:textDirection", "<w:bidi", "<w:rtlGutter", "<w:docGrid", "<w:printerSettings", "<w:sectPrChange"} {
		if i := strings.Index(sect, later); i >= 0 && i < insert {
			insert = i
		}
	}
	at -= len(sect) - insert
	return []byte(str[:at] + "<w:titlePg/>" + str[at:])
}
