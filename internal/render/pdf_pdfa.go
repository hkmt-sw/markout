package render

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/hkmt-sw/markout/internal/ast"
)

// A PDF/A file can be opened in the same way in ten or fifty years: it
// carries what it needs (fonts, a color profile, its description) and
// nothing that depends on the machine it is opened on. markout makes
// PDF/A-2u, which also says which character each glyph stands for; the
// tagged structure of PDF/A-2a is not made.
//
// gopdf has no PDF/A mode. Measured with veraPDF on the sample documents,
// what its files lack comes to six rules, and each is put right here in the
// finished file, the way fixToUnicode puts the maps right:
//
//	6.1.3-1       a file identifier in the trailer
//	6.6.2.1-1     a metadata stream in the catalog
//	6.2.4.3-2     an output intent with an sRGB profile, since colors are DeviceRGB
//	6.2.11.3.2-1  CIDToGIDMap in the dictionaries of the fonts
//	6.1.7.1-1     an end of line before endstream
//	6.3.2-1       the F flag on the links
//
// A plain PDF is not touched.

// srgbProfile is the color profile the output intent names; see icc/README.md.
//
//go:embed icc/sRGB-v2-magic.icc
var srgbProfile []byte

// pdfaFor says whether a document is made as PDF/A. The front matter's pdfa
// key decides when it is true or false; without it, or with anything else
// (which is ignored, and named in the warning), requested does.
func pdfaFor(fm ast.FrontMatter, requested bool) (pdfa bool, warning string) {
	key, ok := findKey(fm, "pdfa")
	if !ok {
		return requested, ""
	}
	if asked, isBool := fm.Raw[key].(bool); isBool {
		return asked, ""
	}
	return requested, fmt.Sprintf("pdfa in the front matter must be true or false, not %v; it was ignored.", fm.Raw[key])
}

// pdfaOf says whether a document is made as PDF/A: it is decided by the
// front matter, when that is the first element of the document, and the
// warning says what is wrong with its pdfa key.
func pdfaOf(doc *ast.Document, requested bool) (pdfa bool, warning string) {
	if fm, ok := frontMatterOf(doc); ok {
		return pdfaFor(fm, requested)
	}
	return requested, ""
}

// pdfaMeta is what a PDF/A file says about itself, in its Info dictionary
// and, with the same values, in its XMP metadata.
type pdfaMeta struct {
	title, author string
	lang          string    // a normalized language tag, or ""
	date          time.Time // creation and modification, or the zero time
}

// pdfProducer names the program that made the file.
const pdfProducer = "markout"

// metaOf collects what the file says from what the document says: the
// title and author of its front matter, and its date when that is a day or a
// moment in a form markout reads. The date is the document's, not the day
// of the conversion, so that converting a document again gives the same file.
func metaOf(info docInfo, lang string) pdfaMeta {
	m := pdfaMeta{title: metaText(info.title), author: metaText(info.author), lang: lang}
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04", "2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, strings.TrimSpace(info.date)); err == nil {
			m.date = t
			break
		}
	}
	return m
}

// metaText is a title or an author as the metadata says it: on one line, and
// without the characters XML cannot hold, so that the Info dictionary and the
// XMP stream have the same text.
func metaText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r',
			r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000:
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// pdfString writes text as a PDF string in UTF-16.
func pdfString(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// pdfDate writes a time as a PDF date: D:20261014120000+02'00'.
func pdfDate(t time.Time) string {
	s := "D:" + t.Format("20060102150405")
	_, offset := t.Zone()
	if offset == 0 {
		return s + "Z"
	}
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	return fmt.Sprintf("%s%s%02d'%02d'", s, sign, offset/3600, offset%3600/60)
}

// infoDict is the Info dictionary of the file.
func (m pdfaMeta) infoDict() string {
	var b strings.Builder
	b.WriteString("<<\n")
	if m.title != "" {
		b.WriteString("/Title " + pdfString(m.title) + "\n")
	}
	if m.author != "" {
		b.WriteString("/Author " + pdfString(m.author) + "\n")
	}
	b.WriteString("/Producer " + pdfString(pdfProducer) + "\n")
	if !m.date.IsZero() {
		b.WriteString("/CreationDate (" + pdfDate(m.date) + ")\n")
		b.WriteString("/ModDate (" + pdfDate(m.date) + ")\n")
	}
	b.WriteString(">>")
	return b.String()
}

// xmpEscape escapes text for XML.
func xmpEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s)) // a bytes.Buffer does not fail
	return b.String()
}

// byteOrderMark is the character the XMP packet's begin attribute holds, in UTF-8.
const byteOrderMark = "\xEF\xBB\xBF"

// xmp is the XMP metadata of the file: what the Info dictionary says, and
// that the file is PDF/A-2u.
func (m pdfaMeta) xmp() []byte {
	var b strings.Builder
	b.WriteString("<?xpacket begin=\"" + byteOrderMark + "\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString("<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n")
	b.WriteString(" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n")
	b.WriteString("  <rdf:Description rdf:about=\"\"\n")
	b.WriteString("    xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n")
	b.WriteString("    xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n")
	b.WriteString("    xmlns:pdf=\"http://ns.adobe.com/pdf/1.3/\"\n")
	b.WriteString("    xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\">\n")
	b.WriteString("   <pdfaid:part>2</pdfaid:part>\n")
	b.WriteString("   <pdfaid:conformance>U</pdfaid:conformance>\n")
	if m.title != "" {
		b.WriteString("   <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">" + xmpEscape(m.title) + "</rdf:li></rdf:Alt></dc:title>\n")
	}
	if m.author != "" {
		b.WriteString("   <dc:creator><rdf:Seq><rdf:li>" + xmpEscape(m.author) + "</rdf:li></rdf:Seq></dc:creator>\n")
	}
	if m.lang != "" {
		b.WriteString("   <dc:language><rdf:Bag><rdf:li>" + xmpEscape(m.lang) + "</rdf:li></rdf:Bag></dc:language>\n")
	}
	b.WriteString("   <pdf:Producer>" + xmpEscape(pdfProducer) + "</pdf:Producer>\n")
	if !m.date.IsZero() {
		date := m.date.Format(time.RFC3339)
		b.WriteString("   <xmp:CreateDate>" + date + "</xmp:CreateDate>\n")
		b.WriteString("   <xmp:ModifyDate>" + date + "</xmp:ModifyDate>\n")
	}
	b.WriteString("  </rdf:Description>\n")
	b.WriteString(" </rdf:RDF>\n")
	b.WriteString("</x:xmpmeta>\n")
	b.WriteString("<?xpacket end=\"w\"?>")
	return []byte(b.String())
}

var (
	// The dictionary of a font that is a TrueType font program
	cidFontType2 = regexp.MustCompile(`/Subtype /CIDFontType2\n`)
	// The start of a link annotation's dictionary
	linkAnnotation = regexp.MustCompile(`/Type /Annot /Subtype /Link`)
	// The start of a stream, after its dictionary
	streamStart = []byte("\nstream\n")
	streamLen   = regexp.MustCompile(`/Length (\d+)`)
	trailerRoot = regexp.MustCompile(`/Root (\d+ 0 R)`)
)

// cidToGIDMapEdits say, in the dictionary of each font, that a character
// code is the glyph number: gopdf writes the codes it draws with as glyph
// numbers of the embedded font. PDF/A wants the map named, even when it is
// the identity.
func cidToGIDMapEdits(pdf []byte) []pdfEdit {
	var edits []pdfEdit
	for _, m := range cidFontType2.FindAllIndex(pdf, -1) {
		edits = append(edits, pdfEdit{at: m[0], with: []byte("/CIDToGIDMap /Identity\n")})
	}
	return edits
}

// linkFlagEdits set the "print" flag (F 4) on each link: PDF/A wants every
// annotation to have the flag set and the hidden ones off.
func linkFlagEdits(pdf []byte) []pdfEdit {
	var edits []pdfEdit
	for _, m := range linkAnnotation.FindAllIndex(pdf, -1) {
		end := bytes.Index(pdf[m[1]:], []byte("endobj"))
		if end < 0 || bytes.Contains(pdf[m[1]:m[1]+end], []byte(" /F ")) {
			continue
		}
		edits = append(edits, pdfEdit{at: m[1], with: []byte(" /F 4")})
	}
	return edits
}

// endstreamEdits put an end of line before "endstream" in the streams that
// have none: gopdf writes the maps of characters that way. The end of line is
// not part of the stream, so its length stays as it is. A stream whose end
// is not where its length says is an error.
func endstreamEdits(pdf []byte) ([]pdfEdit, error) {
	var edits []pdfEdit
	for pos := 0; ; {
		i := bytes.Index(pdf[pos:], streamStart)
		if i < 0 {
			return edits, nil
		}
		data := pos + i + len(streamStart)
		// The dictionary is in the object that holds the stream
		dictStart := bytes.LastIndex(pdf[:data], []byte(" obj\n"))
		m := streamLen.FindSubmatch(pdf[max(dictStart, 0):data])
		if m == nil {
			return nil, errors.New("a stream has no length")
		}
		length, _ := strconv.Atoi(string(m[1]))
		end := data + length
		rest := pdf[min(end, len(pdf)):]
		switch {
		case bytes.HasPrefix(rest, []byte("endstream")):
			edits = append(edits, pdfEdit{at: end, with: []byte("\n")})
		case bytes.HasPrefix(rest, []byte("\nendstream")), bytes.HasPrefix(rest, []byte("\r\nendstream")):
		default:
			return nil, errors.New("a stream does not end where its length says")
		}
		pos = end
	}
}

// makePDFA turns a PDF from gopdf into PDF/A-2u: the six repairs listed
// above. A file that is not laid out the way gopdf lays it out is an error,
// since it could not be made right.
func makePDFA(pdf []byte, meta pdfaMeta) ([]byte, error) {
	head := xrefHead.FindSubmatchIndex(pdf)
	catalog := catalogType.FindIndex(pdf)
	if head == nil || catalog == nil {
		return nil, errors.New("the PDF is not laid out the way gopdf lays it out")
	}
	// The objects added come after the last, in this order
	first, _ := strconv.Atoi(string(pdf[head[2]:head[3]]))
	metadata, profile := first+1, first+2 // the Info dictionary is the first

	edits := []pdfEdit{{at: catalog[1], with: []byte(fmt.Sprintf(
		"  /Metadata %d 0 R\n  /OutputIntents [<<\n    /Type /OutputIntent\n    /S /GTS_PDFA1\n"+
			"    /OutputConditionIdentifier (sRGB IEC61966-2.1)\n    /Info (sRGB IEC61966-2.1)\n"+
			"    /DestOutputProfile %d 0 R\n  >>]\n", metadata, profile))}}
	edits = append(edits, cidToGIDMapEdits(pdf)...)
	edits = append(edits, linkFlagEdits(pdf)...)
	ends, err := endstreamEdits(pdf)
	if err != nil {
		return nil, err
	}
	edits = append(edits, ends...)
	slices.SortStableFunc(edits, func(a, b pdfEdit) int { return a.at - b.at })

	pdf = editPDF(pdf, edits)
	xmp := meta.xmp()
	return appendObjects(pdf, [][]byte{
		[]byte(meta.infoDict()),
		stream(fmt.Sprintf("/Type /Metadata /Subtype /XML /Length %d", len(xmp)), xmp),
		stream(fmt.Sprintf("/N 3 /Length %d", len(srgbProfile)), srgbProfile),
	})
}

// stream is the body of an object that is a stream.
func stream(dict string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("<< " + dict + " >>\nstream\n")
	b.Write(data)
	b.WriteString("\nendstream")
	return b.Bytes()
}

// appendObjects adds objects to the end of the body of the PDF, numbered
// from the number of objects it has, and writes the table of objects and the
// trailer anew: with the first of the added objects as the Info dictionary,
// and an identifier made from the content of the file.
func appendObjects(pdf []byte, objects [][]byte) ([]byte, error) {
	head, tail := xrefHead.FindSubmatchIndex(pdf), xrefTail.FindSubmatchIndex(pdf)
	if head == nil || tail == nil {
		return nil, errors.New("the PDF has no table of objects")
	}
	count, _ := strconv.Atoi(string(pdf[head[2]:head[3]]))
	entries := head[1]
	trailerAt := entries + 20*count
	if trailerAt > len(pdf) {
		return nil, errors.New("the PDF has no table of objects")
	}
	trailer := pdf[trailerAt:tail[0]]
	root := trailerRoot.FindSubmatch(trailer)
	if root == nil || bytes.Contains(trailer, []byte("/Info")) || bytes.Contains(trailer, []byte("/Encrypt")) {
		return nil, errors.New("the PDF has a trailer that gopdf does not write")
	}

	var out bytes.Buffer
	out.Write(pdf[:head[0]+1]) // up to the table of objects
	var added []string
	for i, body := range objects {
		added = append(added, fmt.Sprintf("%010d 00000 n \n", out.Len()))
		fmt.Fprintf(&out, "%d 0 obj\n", count+i)
		out.Write(body)
		out.WriteString("\nendobj\n\n")
	}

	// The same file gives the same identifier: it is a hash of the file
	sum := sha256.Sum256(out.Bytes())
	id := strings.ToUpper(hex.EncodeToString(sum[:16]))

	start := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", count+len(objects))
	out.Write(pdf[entries:trailerAt])
	for _, entry := range added {
		out.WriteString(entry)
	}
	fmt.Fprintf(&out, "trailer\n<<\n/Size %d\n/Root %s\n/Info %d 0 R\n/ID [<%s> <%s>]\n>>\nstartxref\n%d\n%%%%EOF\n",
		count+len(objects), root[1], count, id, id, start)
	return out.Bytes(), nil
}
