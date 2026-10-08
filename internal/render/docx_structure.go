package render

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/theme"
)

// What makes the output a Word document and not only a picture of one:
// headings in Word's heading styles, lists that Word numbers, a table of
// contents Word can update, and bookmarks that links within the document
// jump to. The library writes the references (a paragraph's style, its list,
// its bookmark); the definitions they refer to are written here, from the
// theme, and put into the file after it is saved.

// ---- Bookmarks --------------------------------------------------------------

// collectHeadings gives every heading with an ID a bookmark name, so that a
// link can refer to a heading that comes after it.
func (r *DocxRenderer) collectHeadings(elems []ast.Element) {
	for _, elem := range elems {
		switch e := elem.(type) {
		case ast.Heading:
			if e.ID == "" {
				continue
			}
			if _, seen := r.bookmarks[e.ID]; !seen {
				r.bookmarks[e.ID] = r.newBookmarkName(e.ID)
			}
			// A link may also name the heading the way GitHub and GitLab
			// do, which keeps accented and other non-ASCII letters
			slug := headingSlug(e.Runs)
			if n := r.slugs[slug]; n > 0 {
				slug = fmt.Sprintf("%s-%d", slug, n)
			}
			r.slugs[headingSlug(e.Runs)]++
			if _, taken := r.bookmarks[slug]; !taken && slug != "" {
				r.bookmarks[slug] = r.bookmarks[e.ID]
			}
		case ast.List:
			r.collectItemHeadings(e.Items)
		}
	}
}

func (r *DocxRenderer) collectItemHeadings(items []ast.ListItem) {
	for _, item := range items {
		r.collectHeadings(item.Blocks)
		r.collectItemHeadings(item.Children)
	}
}

// headingSlug is the anchor GitHub gives a heading: its text in lower case,
// with spaces as hyphens and punctuation dropped.
func headingSlug(runs []ast.InlineRun) string {
	var b strings.Builder
	for _, run := range runs {
		for _, c := range strings.ToLower(run.Text) {
			switch {
			case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '_':
				b.WriteRune(c)
			case c == ' ':
				b.WriteByte('-')
			}
		}
	}
	return b.String()
}

// maxBookmarkName is the longest name Word accepts for a bookmark.
const maxBookmarkName = 40

// newBookmarkName turns a heading ID into a name Word accepts: letters,
// digits and underscores, at most 40 characters. The leading underscore
// keeps the bookmark out of Word's list of bookmarks, like the ones Word
// makes for a table of contents.
func (r *DocxRenderer) newBookmarkName(id string) string {
	var b strings.Builder
	b.WriteByte('_')
	for _, c := range id {
		if b.Len() >= maxBookmarkName-4 {
			break
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	for n := 2; r.bookmarkNames[name]; n++ {
		name = fmt.Sprintf("%s_%d", b.String(), n)
	}
	r.bookmarkNames[name] = true
	return name
}

// bookmarkFor returns the bookmark a link to "#id" jumps to.
func (r *DocxRenderer) bookmarkFor(link string) (string, bool) {
	if !strings.HasPrefix(link, "#") {
		return "", false
	}
	id := link[1:]
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}
	if name, ok := r.bookmarks[id]; ok {
		return name, true
	}
	name, ok := r.bookmarks[strings.ToLower(id)]
	return name, ok
}

// ---- Lists ------------------------------------------------------------------

const (
	bulletNum      = 1 // the list every bulleted item belongs to
	maxListLevel   = 8 // Word has nine levels
	numberingLevel = maxListLevel + 1
)

// listHang is how far the text of a list item is from its bullet or number.
func (r *DocxRenderer) listHang() float64 {
	return float64(int(r.t.Text.Size*1.6 + 0.5))
}

// itemIndent is where the text of a list item at a level starts.
func (r *DocxRenderer) itemIndent(level int) float64 {
	return float64(level+1)*r.t.List.Indent + r.listHang()
}

// newOrderedNum starts a numbered list: Word counts the items of each
// separately.
func (r *DocxRenderer) newOrderedNum() int {
	r.orderedNums++
	return bulletNum + r.orderedNums
}

// numberingXML is word/numbering.xml: how bullets and numbers look at each
// level, and one list for the bullets and one for each numbered list.
func (r *DocxRenderer) numberingXML() []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)

	font := xmlEscape(r.t.Fonts.Body)
	level := func(ilvl int, format, text string) {
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/>`,
			ilvl, format, text)
		fmt.Fprintf(&b, `<w:pPr><w:ind w:left="%d" w:hanging="%d"/></w:pPr>`, twips(r.itemIndent(ilvl)), twips(r.listHang()))
		fmt.Fprintf(&b, `<w:rPr><w:rFonts w:ascii="%s" w:hAnsi="%s" w:cs="%s" w:hint="default"/><w:color w:val="%s"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:lvl>`,
			font, font, font, r.t.Text.Color.Hex(), halfPoints(r.t.Text.Size), halfPoints(r.t.Text.Size))
	}

	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for i := 0; i < numberingLevel; i++ {
		level(i, "bullet", getBulletChar(i))
	}
	b.WriteString(`</w:abstractNum>`)

	// The same forms formatOrderedBullet gives PDF: 1. a. 1)
	b.WriteString(`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for i := 0; i < numberingLevel; i++ {
		switch i % 3 {
		case 0:
			level(i, "decimal", fmt.Sprintf("%%%d.", i+1))
		case 1:
			level(i, "lowerLetter", fmt.Sprintf("%%%d.", i+1))
		default:
			level(i, "decimal", fmt.Sprintf("%%%d)", i+1))
		}
	}
	b.WriteString(`</w:abstractNum>`)

	fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="0"/></w:num>`, bulletNum)
	for n := 1; n <= r.orderedNums; n++ {
		// Lists made from one definition go on counting where the last one
		// stopped unless each is told to start again.
		fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/>`, bulletNum+n)
		for i := 0; i < numberingLevel; i++ {
			fmt.Fprintf(&b, `<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="1"/></w:lvlOverride>`, i)
		}
		b.WriteString(`</w:num>`)
	}
	b.WriteString(`</w:numbering>`)
	return []byte(b.String())
}

// ---- Styles -----------------------------------------------------------------

// styleDefinitions are the styles the document uses, written from the theme:
// Normal for ordinary text, the heading styles, the styles of table of
// contents entries, and the character style of links (their color; a link in
// the text is underlined by itself, an entry of the table of contents is
// not). Text in these styles
// carries no formatting of its own, so changing a style in Word changes the
// text.
func (r *DocxRenderer) styleDefinitions() map[string]string {
	styles := map[string]string{}
	fonts := func(name string) string {
		name = xmlEscape(name)
		return fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:cs="%s"/>`, name, name, name)
	}
	size := func(pt float64) string {
		return fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPoints(pt), halfPoints(pt))
	}

	text := r.t.Text
	styles["Normal"] = `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:uiPriority w:val="0"/><w:qFormat/>` +
		`<w:rPr>` + fonts(r.t.Fonts.Body) + `<w:color w:val="` + text.Color.Hex() + `"/>` + size(text.Size) + `</w:rPr></w:style>`

	for i, hd := range r.t.Heading {
		// Black is what a word processor uses when no color is given
		color := hd.Color.Hex()
		if hd.Color == theme.Black {
			color = "auto"
		}
		styles[fmt.Sprintf("Heading%d", i+1)] = fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>`+
				`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="%d" w:after="%d" w:line="%d" w:lineRule="atLeast"/><w:outlineLvl w:val="%d"/></w:pPr>`+
				`<w:rPr>%s<w:b/><w:bCs/><w:color w:val="%s"/>%s</w:rPr></w:style>`,
			i+1, i+1, twips(hd.SpaceBefore), twips(hd.SpaceAfter), twips(hd.LineHeight), i,
			fonts(r.t.Fonts.Heading), color, size(hd.Size))
	}

	for n := 1; n <= 9; n++ {
		styles[fmt.Sprintf("TOC%d", n)] = fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="TOC%d"><w:name w:val="toc %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/>`+
				`<w:pPr><w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="atLeast"/><w:ind w:left="%d"/></w:pPr></w:style>`,
			n, n, twips(text.LineHeight), twips(float64(n-1)*r.t.List.Indent))
	}

	styles["Hyperlink"] = `<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/>` +
		`<w:rPr><w:color w:val="` + r.t.Link.Color.Hex() + `"/></w:rPr></w:style>`
	return styles
}

// replaceStyles puts the definitions into word/styles.xml in place of the
// library's own styles of the same IDs.
func replaceStyles(content []byte, styles map[string]string) []byte {
	ids := make([]string, 0, len(styles))
	for id := range styles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		def := styles[id]
		old := regexp.MustCompile(`(?s)<w:style\s[^>]*w:styleId="` + regexp.QuoteMeta(id) + `"[^>]*>.*?</w:style>`)
		if loc := old.FindIndex(content); loc != nil {
			content = append(content[:loc[0]:loc[0]], append([]byte(def), content[loc[1]:]...)...)
		} else {
			content = bytes.Replace(content, []byte("</w:styles>"), []byte(def+"</w:styles>"), 1)
		}
	}
	return content
}

// ---- Table of contents ------------------------------------------------------

// The entries of a table of contents are ordinary paragraphs. These two
// marks, written as runs before the first and after the last, become the
// start and the end of a TOC field around them, which is what lets Word
// update the table. What is between them is what the field shows until then.
const (
	tocBeginMark = "markout-toc-begin"
	tocEndMark   = "markout-toc-end"
)

const (
	tocFieldBegin = `<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> TOC \o "1-6" \h \z \u </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`
	tocFieldEnd = `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
)

// wrapTOC replaces the runs holding the marks with the field characters.
func wrapTOC(content []byte) []byte {
	str := string(content)
	for mark, field := range map[string]string{tocBeginMark: tocFieldBegin, tocEndMark: tocFieldEnd} {
		for {
			at := strings.Index(str, mark)
			if at < 0 {
				break
			}
			start := strings.LastIndex(str[:at], "<w:r>")
			end := strings.Index(str[at:], "</w:r>")
			if start < 0 || end < 0 {
				// Not in a run of the form the library writes: drop the mark
				str = str[:at] + str[at+len(mark):]
				continue
			}
			str = str[:start] + field + str[at+end+len("</w:r>"):]
		}
	}
	return []byte(str)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
