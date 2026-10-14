package render

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/lang"
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
	var headings []ast.Heading
	r.anchors, headings = collectAnchors(elems)
	for _, h := range headings {
		r.bookmarks[h.ID] = r.newBookmarkName(h.ID)
	}
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
	id, ok := r.anchors.resolve(link)
	if !ok {
		return "", false
	}
	name, ok := r.bookmarks[id]
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
// contents entries, Title and Subtitle for the cover page, and the character
// style of links (their color; a link in the text is underlined by itself,
// an entry of the table of contents is not). Text in these styles carries no
// formatting of its own, so changing a style in Word changes the text.
func (r *DocxRenderer) styleDefinitions() map[string]string {
	styles := map[string]string{}
	fonts := func(name string) string {
		name = xmlEscape(name)
		return fmt.Sprintf(`<w:rFonts w:ascii="%s" w:hAnsi="%s" w:cs="%s"/>`, name, name, name)
	}
	size := func(pt float64) string {
		return fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, halfPoints(pt), halfPoints(pt))
	}

	// In a document that is hyphenated, headings, the table of contents and
	// what is in the style for it are not
	whole := ""
	if r.t.Text.Hyphenated() {
		whole = `<w:suppressAutoHyphens/>`
		styles[wholeStyle] = `<w:style w:type="paragraph" w:customStyle="1" w:styleId="` + wholeStyle + `"><w:name w:val="Unhyphenated"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/>` +
			`<w:pPr>` + whole + `</w:pPr></w:style>`
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
				`<w:pPr><w:keepNext/><w:keepLines/>%s<w:spacing w:before="%d" w:after="%d" w:line="%d" w:lineRule="atLeast"/><w:outlineLvl w:val="%d"/></w:pPr>`+
				`<w:rPr>%s<w:b/><w:bCs/><w:color w:val="%s"/>%s</w:rPr></w:style>`,
			i+1, i+1, whole, twips(hd.SpaceBefore), twips(hd.SpaceAfter), twips(hd.LineHeight), i,
			fonts(r.t.Fonts.Heading), color, size(hd.Size))
	}

	for n := 1; n <= 9; n++ {
		styles[fmt.Sprintf("TOC%d", n)] = fmt.Sprintf(
			`<w:style w:type="paragraph" w:styleId="TOC%d"><w:name w:val="toc %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/>`+
				`<w:pPr>%s<w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="atLeast"/><w:ind w:left="%d"/></w:pPr></w:style>`,
			n, n, whole, twips(text.LineHeight), twips(float64(n-1)*r.t.List.Indent))
	}

	// The title and the subtitle of a cover page; the lines of the cover keep
	// the proportions of a first-level heading
	if r.cover {
		c := r.t.Cover
		ratio := 1.2
		if h1 := r.t.Heading[0]; h1.Size > 0 {
			ratio = h1.LineHeight / h1.Size
		}
		for _, s := range []struct {
			id       string
			priority int
			size     float64
			color    theme.Color
			bold     string
		}{
			{coverTitleStyle, 10, c.TitleSize, c.TitleColor, `<w:b/><w:bCs/>`},
			{coverSubtitleStyle, 11, c.SubtitleSize, c.SubtitleColor, ``},
		} {
			styles[s.id] = fmt.Sprintf(
				`<w:style w:type="paragraph" w:styleId="%s"><w:name w:val="%s"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="%d"/><w:qFormat/>`+
					`<w:pPr>%s<w:spacing w:before="0" w:after="0" w:line="%d" w:lineRule="atLeast"/></w:pPr>`+
					`<w:rPr>%s%s<w:color w:val="%s"/>%s</w:rPr></w:style>`,
				s.id, s.id, s.priority, whole, twips(s.size*ratio), fonts(r.t.Fonts.Heading), s.bold, s.color.Hex(), size(s.size))
		}
	}

	styles["Hyperlink"] = `<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/>` +
		`<w:rPr><w:color w:val="` + r.t.Link.Color.Hex() + `"/></w:rPr></w:style>`
	return styles
}

// setDocxLanguage names the language of the document in word/styles.xml, as
// the default of all its text: it is what a word processor checks the
// spelling in and hyphenates by. Without a language the file is left as it
// is, and the word processor uses its own.
func setDocxLanguage(content []byte, tag string) []byte {
	const open = "<w:docDefaults>"
	if tag == "" || !bytes.Contains(content, []byte(open)) {
		return content
	}
	def := open + `<w:rPrDefault><w:rPr><w:lang w:val="` + xmlEscape(lang.Regional(tag)) + `"/></w:rPr></w:rPrDefault>`
	return bytes.Replace(content, []byte(open), []byte(def), 1)
}

var defaultTabStop = regexp.MustCompile(`<w:defaultTabStop [^>]*/>`)

// hyphenateDocx turns on hyphenation in word/settings.xml, with the same
// limit on lines in a row that end in a hyphen as PDF has. The word
// processor hyphenates by the language of the text.
func hyphenateDocx(content []byte) []byte {
	// The settings have an order; these two come right after the tab stop
	at := defaultTabStop.FindIndex(content)
	if at == nil {
		return content
	}
	on := fmt.Sprintf(`<w:autoHyphenation/><w:consecutiveHyphenLimit w:val="%d"/>`, maxBrokenLines)
	return append(content[:at[1]:at[1]], append([]byte(on), content[at[1]:]...)...)
}

// styleElement matches a style definition in word/styles.xml and captures its ID.
var styleElement = regexp.MustCompile(`(?s)<w:style\s[^>]*w:styleId="([^"]*)"[^>]*>.*?</w:style>`)

// replaceStyles puts the definitions into word/styles.xml in place of the
// library's own styles of the same IDs. A definition whose ID the file does
// not have is added at the end, in the order of the IDs.
func replaceStyles(content []byte, styles map[string]string) []byte {
	var out []byte
	done := make(map[string]bool, len(styles))
	last := 0
	for _, loc := range styleElement.FindAllSubmatchIndex(content, -1) {
		id := string(content[loc[2]:loc[3]])
		def, ok := styles[id]
		if !ok || done[id] {
			continue
		}
		done[id] = true
		out = append(out, content[last:loc[0]]...)
		out = append(out, def...)
		last = loc[1]
	}
	content = append(out, content[last:]...)
	var added strings.Builder
	for _, id := range slices.Sorted(maps.Keys(styles)) {
		if !done[id] {
			added.WriteString(styles[id])
		}
	}
	if added.Len() == 0 {
		return content
	}
	return bytes.Replace(content, []byte("</w:styles>"), []byte(added.String()+"</w:styles>"), 1)
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
