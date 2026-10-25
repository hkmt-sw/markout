package render

import (
	"archive/zip"
	"encoding/xml"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/theme"
)

const classBody = "\n# The Heading\n\nbody text\n"

// headerOf returns the header part of a page ("default" or "first"), or ""
// if the section has none.
func headerOf(t *testing.T, path, kind string) string {
	t.Helper()
	doc := docxPart(t, path, "word/document.xml")
	m := regexp.MustCompile(`<w:headerReference w:type="` + kind + `" r:id="(\w+)">`).FindStringSubmatch(doc)
	if m == nil {
		return ""
	}
	rels := docxPart(t, path, "word/_rels/document.xml.rels")
	rel := regexp.MustCompile(`Id="` + m[1] + `"[^>]*Target="([^"]+)"`).FindStringSubmatch(rels)
	if rel == nil {
		t.Fatalf("relationship %s is not in the package", m[1])
	}
	if !strings.Contains(docxPart(t, path, "[Content_Types].xml"), `PartName="/word/`+rel[1]+`"`) {
		t.Errorf("%s has no content type", rel[1])
	}
	return docxPart(t, path, "word/"+rel[1])
}

// hasMarks says whether a header part carries a band or a watermark.
func hasBand(header string) bool      { return strings.Contains(header, `type="#_x0000_t202"`) }
func hasWatermark(header string) bool { return strings.Contains(header, `type="#_x0000_t136"`) }

// marksTheme is the default theme with a header and a footer.
func marksTheme() theme.Set {
	set := theme.Default()
	set.DOCX.Header.Left, set.DOCX.Header.Right = "{title}", "{date}"
	set.DOCX.Footer.Center = "Page {page} of {pages}"
	return set
}

func TestDOCXClassificationBand(t *testing.T) {
	set := theme.Default()
	out, warnings := coverDocx(t, "---\ntitle: T\nclassification: Szigorúan titkos\n---\n"+classBody, set)
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	header := headerOf(t, out, "default")
	if !hasBand(header) {
		t.Fatalf("no band in the default header:\n%s", header)
	}
	if hasWatermark(header) {
		t.Error("a watermark nobody asked for")
	}
	cls := set.DOCX.Classification
	page := set.DOCX.Page
	for _, want := range []string{
		// Full width of the page, at its top edge, as high as the theme says
		"position:absolute;margin-left:0;margin-top:0;width:" + pt(page.Width) + "pt;height:" + pt(cls.Height) + "pt;",
		"mso-position-horizontal-relative:page;mso-position-vertical-relative:page",
		// The level the theme knows gives the background; the text is white
		`fillcolor="#B91C1C"`, `stroked="f"`, `<w:color w:val="FFFFFF"/>`,
		// The value as written, bold, centered, at the theme's size
		`>Szigorúan titkos</w:t>`, `<w:b/>`, `<w:jc w:val="center"/>`, `<w:sz w:val="17"/>`,
		`<w10:wrap anchorx="page" anchory="page"/>`, `<v:textbox inset="0,0,0,0">`,
		// Behind the text, as the PDF draws it
		"z-index:-",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the band lacks %s:\n%s", want, header)
		}
	}
	// A band must not take room from the page: no header text, no margin
	// change, and the paragraph that holds it is a point high
	if strings.Contains(header, "<w:tbl>") || !strings.Contains(header, `w:lineRule="exact"`) {
		t.Errorf("the header holds more than the band:\n%s", header)
	}
	doc := docxPart(t, out, "word/document.xml")
	if !strings.Contains(doc, `w:top="`+strconv.Itoa(twips(page.MarginTop))+`"`) {
		t.Error("the top margin changed")
	}
	if strings.Contains(doc, "titlePg") {
		t.Error("a first page of its own without a cover")
	}
}

func TestDOCXClassificationLevelColors(t *testing.T) {
	for value, background := range map[string]string{
		"Nyilvános": "#2E7D32", "BELSŐ": "#1565C0", " Bizalmas ": "#B45309", "secret": "#B91C1C", //nolint:gocritic // the spaces are the input being tested
	} {
		out, warnings := coverDocx(t, "---\ntitle: T\nclassification: \""+value+"\"\n---\n"+classBody, theme.Default())
		header := headerOf(t, out, "default")
		if !strings.Contains(header, `fillcolor="`+background+`"`) || len(warnings) > 0 {
			t.Errorf("%q: want %s without warnings, got %q\n%s", value, background, warnings, header)
		}
		if !strings.Contains(header, ">"+strings.TrimSpace(value)+"</w:t>") {
			t.Errorf("%q: the band does not say the value as written", value)
		}
	}
}

func TestDOCXNoBandWithoutClassification(t *testing.T) {
	off := theme.Default()
	off.DOCX.Classification.Band = false
	headerTheme := marksTheme()
	for _, tc := range []struct {
		name string
		md   string
		set  theme.Set
	}{
		{"no classification", "---\ntitle: T\n---\n" + classBody, theme.Default()},
		{"no front matter", classBody, theme.Default()},
		{"band off", "---\ntitle: T\nclassification: Titkos\n---\n" + classBody, off},
		{"no classification, with a header", "---\ntitle: T\n---\n" + classBody, headerTheme},
	} {
		out, warnings := coverDocx(t, tc.md, tc.set)
		if len(warnings) > 0 {
			t.Errorf("%s: warnings %q", tc.name, warnings)
		}
		if header := headerOf(t, out, "default"); hasBand(header) || hasWatermark(header) || strings.Contains(header, "xmlns:v") {
			t.Errorf("%s: marks in the header:\n%s", tc.name, header)
		}
		if tc.set.DOCX.Header.Empty() {
			// Not even a header part
			if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, "headerReference") {
				t.Errorf("%s: a header nobody needs", tc.name)
			}
		}
	}
}

func TestDOCXUnknownClassification(t *testing.T) {
	set := theme.Default()
	out, warnings := coverDocx(t, "---\ntitle: T\nclassification: Ultra\n---\n"+classBody, set)
	header := headerOf(t, out, "default")
	neutral := set.DOCX.Classification
	for _, want := range []string{`fillcolor="#` + neutral.Background.Hex() + `"`, `<w:color w:val="` + neutral.Color.Hex() + `"/>`, ">Ultra</w:t>"} {
		if !strings.Contains(header, want) {
			t.Errorf("the band lacks %s", want)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "classification") || !strings.Contains(warnings[0], "Ultra") {
		t.Errorf("warnings = %q, want one that names the classification", warnings)
	}
}

func TestDOCXWatermark(t *testing.T) {
	set := theme.Default()
	out, warnings := coverDocx(t, "---\ntitle: T\nwatermark: TERVEZET\n---\n"+classBody, set)
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	header := headerOf(t, out, "default")
	if !hasWatermark(header) || hasBand(header) {
		t.Fatalf("want a watermark and no band:\n%s", header)
	}
	for _, want := range []string{
		// Word's own text watermark
		`<v:shapetype id="_x0000_t136"`, `string="TERVEZET"`, `font-size:1pt`, "font-weight:bold",
		"rotation:315", "mso-position-horizontal:center", "mso-position-vertical:center",
		"mso-position-horizontal-relative:page", "mso-position-vertical-relative:page",
		`fillcolor="#E6E6E6"`, `stroked="f"`, "z-index:-", `<w10:wrap anchorx="page" anchory="page"/>`,
		`<v:textpath style="font-family:&quot;Georgia&quot;`,
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the watermark lacks %s:\n%s", want, header)
		}
	}
	// The watermark key is not a row of the control block
	if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, "TERVEZET") {
		t.Error("the watermark is in the text")
	}
}

// shapeSize reads the width and height of the watermark's shape in points.
func shapeSize(t *testing.T, header string) (w, h float64) {
	t.Helper()
	m := regexp.MustCompile(`type="#_x0000_t136" style="[^"]*width:([\d.]+)pt;height:([\d.]+)pt;`).FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("no watermark shape:\n%s", header)
	}
	w, _ = strconv.ParseFloat(m[1], 64)
	h, _ = strconv.ParseFloat(m[2], 64)
	return w, h
}

func TestDOCXWatermarkShrinksToFit(t *testing.T) {
	set := theme.Default()
	page := set.DOCX.Page
	limit := math.Min(page.Width, page.Height) * math.Sqrt2 * watermarkFill

	out, _ := coverDocx(t, "---\ntitle: T\nwatermark: A\n---\n"+classBody, set)
	w, h := shapeSize(t, headerOf(t, out, "default"))
	if want := set.DOCX.Watermark.Size * watermarkLineHeight; math.Abs(h-want) > 0.01 {
		t.Errorf("a short text is set at the theme's size: height %.2f, want %.2f", h, want)
	}
	if w > limit {
		t.Errorf("width %.1f over the limit %.1f", w, limit)
	}

	out, _ = coverDocx(t, "---\ntitle: T\nwatermark: "+strings.Repeat("TERVEZET ", 6)+"\n---\n"+classBody, set)
	w, h = shapeSize(t, headerOf(t, out, "default"))
	size := h / watermarkLineHeight
	if size >= set.DOCX.Watermark.Size || w+size > limit+0.5 || w+size < limit-0.5 {
		t.Errorf("a long text is shrunk to the diagonal: size %.1f, width %.1f, limit %.1f", size, w, limit)
	}
}

func TestDOCXWatermarkNotText(t *testing.T) {
	for _, value := range []string{"[a, b]", "42", "true"} {
		out, warnings := coverDocx(t, "---\ntitle: T\nwatermark: "+value+"\n---\n"+classBody, theme.Default())
		if len(warnings) != 1 || !strings.Contains(warnings[0], "watermark") {
			t.Errorf("%s: warnings = %q", value, warnings)
		}
		if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, "headerReference") {
			t.Errorf("%s: a watermark that is not text was drawn", value)
		}
	}
}

func TestDOCXMarksTogether(t *testing.T) {
	// A header with text keeps it, and gets the marks beside it
	out, _ := coverDocx(t, "---\ntitle: The Title\nclassification: Titkos\nwatermark: DRAFT\n---\n"+classBody, marksTheme())
	header := headerOf(t, out, "default")
	for _, want := range []string{">The Title<", "<w:tbl>"} {
		if !strings.Contains(header, want) {
			t.Errorf("the header lost %s", want)
		}
	}
	if !hasBand(header) || !hasWatermark(header) {
		t.Fatalf("the marks are missing:\n%s", header)
	}
	// The band is in front of the watermark, as in the PDF
	z := regexp.MustCompile(`z-index:(-\d+)`).FindAllStringSubmatch(header, -1)
	if len(z) != 2 {
		t.Fatalf("z-indexes: %v", z)
	}
	water, _ := strconv.Atoi(z[0][1])
	band, _ := strconv.Atoi(z[1][1])
	if band <= water {
		t.Errorf("the band (z %d) is not over the watermark (z %d)", band, water)
	}
	// The marks are in the paragraph that ends the header, after the table
	if strings.Index(header, "</w:tbl>") > strings.Index(header, "<w:pict>") {
		t.Error("the marks are before the header table")
	}
	if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, "titlePg") {
		t.Error("a first page of its own without a cover")
	}
}

func TestDOCXMarksOnCover(t *testing.T) {
	const fm = "---\ntitle: The Title\nauthor: An Author\ncover: true\nclassification: Bizalmas\nwatermark: DRAFT\n---\n"
	out, _ := coverDocx(t, fm+classBody, marksTheme())
	doc := docxPart(t, out, "word/document.xml")
	if strings.Count(doc, "<w:titlePg/>") != 1 {
		t.Error("the cover has no page of its own")
	}
	first, def := headerOf(t, out, "first"), headerOf(t, out, "default")
	if first == "" || def == "" {
		t.Fatal("a header is missing")
	}
	for name, part := range map[string]string{"first": first, "default": def} {
		if !hasBand(part) || !hasWatermark(part) {
			t.Errorf("the %s header lacks the marks", name)
		}
		if err := xml.Unmarshal([]byte(part), new(struct{})); err != nil {
			t.Errorf("the %s header is not well-formed: %v", name, err)
		}
	}
	// The cover has the marks but no running text
	if strings.Contains(first, "<w:t>") || strings.Contains(first, "<w:tbl") || strings.Contains(first, ">The Title<") || strings.Contains(first, "fldChar") {
		t.Errorf("the first header has running text:\n%s", first)
	}
	if !strings.Contains(def, ">The Title<") {
		t.Error("the text pages lost their header")
	}
	// Each shape has a number of its own in the document
	ids := regexp.MustCompile(`o:spid="(_x0000_s\d+)"`).FindAllString(first+def, -1)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("shape %s is twice in the document", id)
		}
		seen[id] = true
	}
	if len(ids) != 4 {
		t.Errorf("shapes: %v", ids)
	}
}

func TestDOCXMarksWithoutHeaderText(t *testing.T) {
	const fm = "---\ntitle: The Title\nclassification: Titkos\nwatermark: DRAFT\n"

	// No header and no footer: the header holds the marks and nothing else
	out, _ := coverDocx(t, fm+"---\n"+classBody, theme.Default())
	header := headerOf(t, out, "default")
	if !hasBand(header) || !hasWatermark(header) || strings.Contains(header, "<w:tbl") {
		t.Errorf("want the marks alone:\n%s", header)
	}
	if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, `w:type="first"`) || strings.Contains(doc, "footerReference") {
		t.Errorf("parts nobody needs:\n%s", doc)
	}

	// The same with a cover: the one header serves every page, the cover too
	out, _ = coverDocx(t, fm+"cover: true\n---\n"+classBody, theme.Default())
	doc := docxPart(t, out, "word/document.xml")
	if strings.Contains(doc, "titlePg") || headerOf(t, out, "first") != "" || !hasBand(headerOf(t, out, "default")) {
		t.Errorf("a cover without header text:\n%s", doc)
	}

	// A footer only, and a cover: the first page has a header of its own, with
	// the marks, and a footer without text
	set := theme.Default()
	set.DOCX.Footer.Center = "{page}"
	out, _ = coverDocx(t, fm+"cover: true\n---\n"+classBody, set)
	doc = docxPart(t, out, "word/document.xml")
	first := headerOf(t, out, "first")
	if !strings.Contains(doc, "<w:titlePg/>") || !hasBand(first) || !hasWatermark(first) || !hasBand(headerOf(t, out, "default")) {
		t.Errorf("a cover and a footer only:\n%s\n%s", doc, first)
	}
	if !strings.Contains(doc, `<w:footerReference w:type="first"`) {
		t.Error("the cover has a footer with text")
	}
	if strings.Contains(first, "fldChar") || strings.Contains(first, "<w:tbl") {
		t.Errorf("the first header has running text:\n%s", first)
	}
}

func TestDOCXMarksAreWellFormed(t *testing.T) {
	// Characters that need escaping, in the band, the watermark and the font
	set := marksTheme()
	set.DOCX.Fonts.Body = `Fancy "Sans" & Co`
	out, _ := coverDocx(t, "---\ntitle: T\ncover: true\nclassification: 'A & <B> \"C\"'\nwatermark: 'X & <Y> \"Z\"'\n---\n"+classBody, set)
	for _, kind := range []string{"default", "first"} {
		header := headerOf(t, out, kind)
		if err := xml.Unmarshal([]byte(header), new(struct{})); err != nil {
			t.Errorf("the %s header is not well-formed: %v\n%s", kind, err, header)
		}
		for _, want := range []string{">A &amp; &lt;B&gt; &#34;C&#34;<", `string="X &amp; &lt;Y&gt; &#34;Z&#34;"`} {
			if !strings.Contains(header, want) {
				t.Errorf("the %s header lacks %s", kind, want)
			}
		}
		// The namespaces of the prefixes used are declared on the root
		_, root, _ := strings.Cut(header, "<w:hdr")
		root, _, _ = strings.Cut(root, ">")
		for _, ns := range []string{"xmlns:v=", "xmlns:o=", "xmlns:w10=", "xmlns:w="} {
			if !strings.Contains(root, ns) {
				t.Errorf("the %s header does not declare %s", kind, ns)
			}
		}
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()
}

func TestDOCXBandTextShrinksToFit(t *testing.T) {
	set := theme.Default()
	long := strings.Repeat("Szigorúan titkos ", 12)
	out, _ := coverDocx(t, "---\ntitle: T\nclassification: "+long+"\n---\n"+classBody, set)
	header := headerOf(t, out, "default")
	m := regexp.MustCompile(`<w:sz w:val="(\d+)"/>`).FindStringSubmatch(header)
	if m == nil {
		t.Fatalf("no size:\n%s", header)
	}
	if half, _ := strconv.Atoi(m[1]); half >= 17 {
		t.Errorf("a long classification is set at %d half-points, as a short one is", half)
	}
}
