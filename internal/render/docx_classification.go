package render

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/mmonterroca/docxgo/v2/domain"

	"github.com/hkmt-sw/markout/internal/theme"
)

// The classification band and the watermark are drawn on every page of a DOCX
// as Word draws such things: as VML shapes in the header, positioned against
// the page. A shape in a header is repeated on every page and takes no room
// from the text. The library has no calls for shapes, so the shapes are put
// into the header parts when the file is post-processed; see addPageMarks.
//
// The band is a text box of the VML shape type 202, the one Word writes for
// a text box. The watermark is the shape type 136 (WordArt) with a text path,
// written as Word writes its own text watermark (Design > Watermark): the
// text is 1 pt high and stretched to fit the shape, which is rotated by 315
// degrees, i.e. 45 degrees up from the left, and lies behind the text.

const (
	// watermarkLineHeight is the height of the watermark's shape, as a
	// multiple of its text size: a line of the font's text, as Word makes
	// the shape of a watermark.
	watermarkLineHeight = 1.15

	// Z-index of the shapes. Negative is behind the text, and the higher
	// is in front: the band over the watermark, as in the PDF.
	bandZ      = -251657216
	watermarkZ = -251658240

	// firstShapeID is the number of the first shape; Word's own are numbered
	// from 2049 in the headers, and the number must differ between shapes.
	firstShapeID = 2049

	vmlNamespaces = ` xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:w10="urn:schemas-microsoft-com:office:word"`
)

// pageMarks is what the headers of a DOCX carry on every page. The zero value
// is nothing.
type pageMarks struct {
	band  *bandMark
	water *waterMark
}

// bandMark is the band across the top of the page. Lengths are in points.
type bandMark struct {
	text              string
	font              string
	size              float64
	width, height     float64
	color, background theme.Color
}

// waterMark is the watermark text and the size of the shape that holds it.
type waterMark struct {
	text          string
	font          string
	width, height float64
	color         theme.Color
}

func (m pageMarks) any() bool { return m.band != nil || m.water != nil }

// pageMarks works out what the document's headers carry.
func (r *DocxRenderer) pageMarks() pageMarks {
	var m pageMarks
	page := r.t.Page
	metrics := boldMetrics()
	if r.band.drawn {
		cls := r.t.Classification
		b := &bandMark{
			text: r.band.text, font: r.t.Fonts.Body, size: cls.Size,
			width: page.Width, height: cls.Height,
			color: r.band.color, background: r.band.background,
		}
		// A classification too long for the page is set smaller
		if metrics != nil {
			if room, w := page.Width-2*page.MarginLeft, metrics.width(b.text, b.size); w > room && room > 0 {
				b.size *= room / w
			}
		}
		m.band = b
	}
	if r.watermark != "" {
		mark := r.t.Watermark
		size := mark.Size
		w := 0.6 * size * float64(len([]rune(r.watermark)))
		if metrics != nil {
			w = metrics.width(r.watermark, size)
		}
		// As in the PDF: the text and its height fit along the diagonal of
		// the shorter side of the page, less a tenth
		if limit := math.Min(page.Width, page.Height) * math.Sqrt2 * watermarkFill; w+size > limit {
			size = limit / (w/size + 1)
			w = w / mark.Size * size
			if metrics != nil {
				w = metrics.width(r.watermark, size)
			}
		}
		m.water = &waterMark{
			text: r.watermark, font: r.t.Fonts.Body,
			width: w, height: size * watermarkLineHeight, color: mark.Color,
		}
	}
	return m
}

// boldMetrics measures text in the bold sans the PDF draws marks with; it is
// metric-compatible with Arial Bold.
var boldMetrics = sync.OnceValue(func() *glyphBoxes {
	boxes, _ := newGlyphBoxes(fontSansBold)
	return boxes
})

// run returns the runs, for a header paragraph, that hold the shapes of the
// n-th header part: each in a picture of its own, as Word writes them. Shapes
// must have numbers of their own in the document.
func (m pageMarks) run(n int) string {
	const open, shut = "<w:r><w:rPr><w:noProof/></w:rPr><w:pict>", "</w:pict></w:r>"
	var b strings.Builder
	if m.water != nil {
		b.WriteString(open + m.water.shape(n) + shut)
	}
	if m.band != nil {
		b.WriteString(open + m.band.shape(n) + shut)
	}
	return b.String()
}

// shape is the text box of the band: the full width of the page at its top
// edge, the text centered in it.
func (b *bandMark) shape(n int) string {
	var s strings.Builder
	// The shape type is the one Word writes for a text box
	s.WriteString(`<v:shapetype id="_x0000_t202" coordsize="21600,21600" o:spt="202" path="m,l,21600r21600,l21600,xe">` +
		`<v:stroke joinstyle="miter"/><v:path gradientshapeok="t" o:connecttype="rect"/></v:shapetype>`)
	fmt.Fprintf(&s, `<v:shape id="ClassificationBand%d" o:spid="_x0000_s%d" type="#_x0000_t202" `+
		`style="position:absolute;margin-left:0;margin-top:0;width:%spt;height:%spt;z-index:%d;`+
		`mso-position-horizontal-relative:page;mso-position-vertical-relative:page;v-text-anchor:middle" `+
		`o:allowincell="f" fillcolor="%s" stroked="f">`,
		n, firstShapeID+2*n, pt(b.width), pt(b.height), bandZ, vmlColor(b.background))
	s.WriteString(`<v:textbox inset="0,0,0,0"><w:txbxContent><w:p><w:pPr>` +
		`<w:spacing w:before="0" w:after="0" w:line="240" w:lineRule="auto"/><w:ind w:left="0" w:right="0"/><w:jc w:val="center"/></w:pPr>`)
	fmt.Fprintf(&s, `<w:r><w:rPr><w:rFonts w:ascii="%[1]s" w:hAnsi="%[1]s" w:cs="%[1]s"/><w:b/><w:bCs/>`+
		`<w:color w:val="%[2]s"/><w:sz w:val="%[3]d"/><w:szCs w:val="%[3]d"/></w:rPr><w:t xml:space="preserve">%[4]s</w:t></w:r>`,
		escapeXML(b.font), b.color.Hex(), halfPoints(b.size), escapeXML(b.text))
	s.WriteString(`</w:p></w:txbxContent></v:textbox><w10:wrap anchorx="page" anchory="page"/></v:shape>`)
	return s.String()
}

// shape is the watermark as Word writes it: WordArt with a text path, centered
// on the page, rotated up to the right, filled and not outlined.
func (w *waterMark) shape(n int) string {
	var s strings.Builder
	s.WriteString(`<v:shapetype id="_x0000_t136" coordsize="21600,21600" o:spt="136" adj="10800" path="m@7,l@8,m@5,21600l@6,21600e">` +
		`<v:formulas><v:f eqn="sum #0 0 10800"/><v:f eqn="prod #0 2 1"/><v:f eqn="sum 21600 0 @1"/><v:f eqn="sum 0 0 @2"/>` +
		`<v:f eqn="sum 21600 0 @3"/><v:f eqn="if @0 @3 0"/><v:f eqn="if @0 21600 @1"/><v:f eqn="if @0 0 @2"/>` +
		`<v:f eqn="if @0 @4 21600"/><v:f eqn="mid @5 @6"/><v:f eqn="mid @8 @5"/><v:f eqn="mid @7 @8"/>` +
		`<v:f eqn="mid @6 @7"/><v:f eqn="sum @6 0 @5"/></v:formulas>` +
		`<v:path textpathok="t" o:connecttype="custom" o:connectlocs="@9,0;@10,10800;@11,21600;@12,10800" o:connectangles="270,180,90,0"/>` +
		`<v:textpath on="t" fitshape="t"/><v:handles><v:h position="#0,bottomRight" xrange="6629,14971"/></v:handles>` +
		`<o:lock v:ext="edit" text="t" shapetype="t"/></v:shapetype>`)
	fmt.Fprintf(&s, `<v:shape id="PowerPlusWaterMarkObject%d" o:spid="_x0000_s%d" type="#_x0000_t136" `+
		`style="position:absolute;margin-left:0;margin-top:0;width:%spt;height:%spt;rotation:315;z-index:%d;`+
		`mso-position-horizontal:center;mso-position-horizontal-relative:page;`+
		`mso-position-vertical:center;mso-position-vertical-relative:page" `+
		`o:allowincell="f" fillcolor="%s" stroked="f">`,
		n, firstShapeID+2*n+1, pt(w.width), pt(w.height), watermarkZ, vmlColor(w.color))
	fmt.Fprintf(&s, `<v:textpath style="font-family:&quot;%s&quot;;font-size:1pt;font-weight:bold" string="%s"/>`,
		escapeXML(strings.ReplaceAll(w.font, `"`, "")), escapeXML(w.text))
	s.WriteString(`<w10:wrap anchorx="page" anchory="page"/></v:shape>`)
	return s.String()
}

// pt writes a length in points for a VML style.
func pt(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// vmlColor is a color as VML writes it.
func vmlColor(c theme.Color) string { return "#" + c.Hex() }

// escapeXML escapes a string for an XML attribute or for XML text.
func escapeXML(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// headerPart is the name of a header part of the package.
var headerPart = regexp.MustCompile(`^word/header\d+\.xml$`)

// addPageMarks puts the shapes into a header part: it declares the namespaces
// VML needs on the root, and puts the run with the shapes into the last
// paragraph of the part (a header ends with a paragraph).
func addPageMarks(content []byte, run string) []byte {
	str := string(content)
	root := strings.Index(str, "<w:hdr")
	end := strings.LastIndex(str, "</w:p>")
	if root < 0 || end < root {
		return content
	}
	str = str[:end] + run + str[end:]
	open := root + len("<w:hdr")
	return []byte(str[:open] + vmlNamespaces + str[open:])
}

// marksOnlyParagraph adds the paragraph that holds the shapes to a header the
// theme does not have: as small as a paragraph can be, so that it takes no
// room from the page's text.
func marksOnlyParagraph(part interface {
	AddParagraph() (domain.Paragraph, error)
}) error {
	p, err := part.AddParagraph()
	if err != nil {
		return err
	}
	p.SetLineSpacing(domain.LineSpacing{Rule: domain.LineSpacingExact, Value: twips(1)})
	p.SetSpacingBefore(0)
	p.SetSpacingAfter(0)
	return nil
}
