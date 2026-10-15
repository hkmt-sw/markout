package render

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// coverDocx converts Markdown to DOCX and returns the file and the warnings.
func coverDocx(t *testing.T, md string, set theme.Set) (string, []string) {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{Theme: &set, Warn: func(w string) { warnings = append(warnings, w) }}); err != nil {
		t.Fatal(err)
	}
	return out, warnings
}

// themedDocxCover is the default theme with covers on for every document.
func themedDocxCover() theme.Set {
	set := theme.Default()
	set.DOCX.Cover.Enabled = true
	return set
}

// styledCover is a theme with a cover in sizes and colors of its own.
func styledCover() theme.Set {
	set := theme.Default()
	set.DOCX.Cover = theme.Cover{
		TitleSize: 30, TitleColor: theme.Color{R: 0x11, G: 0x22, B: 0x33},
		SubtitleSize: 18, SubtitleColor: theme.Color{R: 0x44, G: 0x55, B: 0x66}, Top: 100,
	}
	return set
}

// hasCover says whether a document has a cover page: its title is in the
// Title style.
func hasCover(body string) bool {
	return strings.Contains(body, `<w:pStyle w:val="Title">`)
}

func TestDOCXCoverWhenAsked(t *testing.T) {
	const body = "\n# The Heading\n\nbody text\n"
	for _, tc := range []struct {
		name  string
		md    string
		set   theme.Set
		cover bool
		warns bool
	}{
		{"front matter asks", "---\ntitle: The Title\ncover: true\n---\n" + body, theme.Default(), true, false},
		{"theme asks", "---\ntitle: The Title\n---\n" + body, themedDocxCover(), true, false},
		{"front matter refuses", "---\ntitle: The Title\ncover: false\n---\n" + body, themedDocxCover(), false, false},
		{"nobody asks", "---\ntitle: The Title\n---\n" + body, theme.Default(), false, false},
		{"no front matter", body, themedDocxCover(), false, false},
		{"only a language", "---\nlang: hu\n---\n" + body, themedDocxCover(), false, false},
		{"yes asks", "---\ntitle: The Title\ncover: yes\n---\n" + body, theme.Default(), true, false},
		{"no refuses", "---\ntitle: The Title\ncover: \"No\"\n---\n" + body, themedDocxCover(), false, false},
		{"another word asks nothing", "---\ntitle: The Title\ncover: maybe\n---\n" + body, theme.Default(), false, true},
		{"another word does not refuse", "---\ntitle: The Title\ncover: \"maybe\"\n---\n" + body, themedDocxCover(), true, true},
	} {
		out, warnings := coverDocx(t, tc.md, tc.set)
		doc := docxPart(t, out, "word/document.xml")
		if got := hasCover(doc); got != tc.cover {
			t.Errorf("%s: cover = %v, want %v", tc.name, got, tc.cover)
		}
		if got := strings.Contains(doc, "<w:pageBreakBefore/>"); got != tc.cover {
			t.Errorf("%s: page break = %v, want %v", tc.name, got, tc.cover)
		}
		if got := len(warnings) > 0; got != tc.warns {
			t.Errorf("%s: warnings %q", tc.name, warnings)
		}
		if tc.warns && !strings.Contains(warnings[0], "cover") {
			t.Errorf("%s: the warning does not name the key: %q", tc.name, warnings[0])
		}
	}
}

func TestDOCXCoverPage(t *testing.T) {
	md := "---\ntitle: The Title\nsubtitle: The Subtitle\nversion: 1.0\nstatus: Final\nscope: Everything\ncover: true\n---\n\n# The Heading\n\nbody text\n"
	out, warnings := coverDocx(t, md, styledCover())
	if len(warnings) > 0 {
		t.Errorf("warnings: %q", warnings)
	}
	doc := docxPart(t, out, "word/document.xml")

	// In order: title, subtitle, the block, the break, the text
	at := map[string]int{}
	for name, mark := range map[string]string{
		"title": ">The Title<", "subtitle": ">The Subtitle<", "table": "<w:tbl>", "end": "</w:tbl>",
		"break": "<w:pageBreakBefore/>", "heading": `<w:pStyle w:val="Heading1">`,
	} {
		at[name] = strings.Index(doc, mark)
		if at[name] < 0 {
			t.Fatalf("no %s (%s) in\n%s", name, mark, doc)
		}
	}
	for _, order := range [][2]string{{"title", "subtitle"}, {"subtitle", "table"}, {"table", "end"}, {"end", "break"}, {"break", "heading"}} {
		if at[order[0]] > at[order[1]] {
			t.Errorf("%s comes after %s", order[0], order[1])
		}
	}

	// The title and the subtitle are in Word's styles, with space above the
	// first by Cover.Top (100 points), 8 points between them, and no
	// formatting of their own
	cover := doc[:at["break"]]
	for _, want := range []string{
		`<w:pStyle w:val="Title"></w:pStyle>`, `<w:spacing w:before="2000" w:after="0">`,
		`<w:pStyle w:val="Subtitle"></w:pStyle>`, `<w:spacing w:before="160" w:after="0">`,
	} {
		if !strings.Contains(cover, want) {
			t.Errorf("the cover lacks %s", want)
		}
	}
	title := cover[strings.Index(cover, `w:val="Title"`):at["subtitle"]]
	if strings.Contains(title, "<w:b") || strings.Contains(title, "<w:sz") || strings.Contains(title, "<w:color") {
		t.Errorf("the title has formatting of its own: %s", title)
	}

	styles := docxPart(t, out, "word/styles.xml")
	style := func(id string) string {
		m := regexp.MustCompile(`(?s)<w:style [^>]*w:styleId="` + id + `".*?</w:style>`).FindString(styles)
		if m == "" {
			t.Fatalf("no style %s", id)
		}
		return m
	}
	heading := theme.Default().DOCX.Fonts.Heading
	for id, wants := range map[string][]string{
		"Title":    {`w:val="Title"`, `w:ascii="` + heading + `"`, `<w:b/>`, `<w:color w:val="112233"/>`, `<w:sz w:val="60"/>`, `<w:basedOn w:val="Normal"/>`},
		"Subtitle": {`w:val="Subtitle"`, `w:ascii="` + heading + `"`, `<w:color w:val="445566"/>`, `<w:sz w:val="36"/>`},
	} {
		def := style(id)
		for _, want := range wants {
			if !strings.Contains(def, want) {
				t.Errorf("style %s lacks %s: %s", id, want, def)
			}
		}
		if strings.Contains(def, "outlineLvl") {
			t.Errorf("style %s is in the outline", id)
		}
	}
	if strings.Contains(style("Subtitle"), "<w:b/>") {
		t.Error("the subtitle is bold")
	}

	// The block has neither of them, nor the cover key; the rest is there
	block := doc[at["table"]:at["end"]]
	for _, not := range []string{">Title<", ">The Title<", "ubtitle", "cover", ">true<"} {
		if strings.Contains(block, not) {
			t.Errorf("the block has %s", not)
		}
	}
	for _, want := range []string{">Version<", ">1.0<", ">Status<", ">Final<", ">scope<", ">Everything<"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block lacks %s", want)
		}
	}

	// The cover is not a heading: no bookmark, no outline entry; the text
	// after it is
	if strings.Contains(cover, "Heading") || strings.Contains(cover, "bookmark") || strings.Contains(cover, "outline") {
		t.Errorf("the cover is a heading:\n%s", cover)
	}
	if strings.Count(doc, "<w:bookmarkStart") != 1 {
		t.Errorf("%d bookmarks, want the one of the heading", strings.Count(doc, "<w:bookmarkStart"))
	}
}

// What starts the page after the cover is a paragraph of its own, whatever
// comes next, and holds nothing.
func TestDOCXCoverBreakIsAnEmptyParagraph(t *testing.T) {
	for name, next := range map[string]string{
		"a table":   "| a | b |\n| - | - |\n| 1 | 2 |\n",
		"a heading": "# Next\n",
		"a list":    "- one\n",
	} {
		out, _ := coverDocx(t, "---\ntitle: T\nversion: 1\ncover: true\n---\n\n"+next, theme.Default())
		doc := docxPart(t, out, "word/document.xml")
		if strings.Contains(doc, coverBreakMark) {
			t.Errorf("%s: the mark is left in the document", name)
		}
		m := regexp.MustCompile(`(?s)<w:p>\s*<w:pPr><w:pageBreakBefore/>(.*?)</w:p>`).FindStringSubmatch(doc)
		if m == nil {
			t.Fatalf("%s: no paragraph that starts a page", name)
		}
		if strings.Contains(m[1], "<w:t") || strings.Contains(m[1], "<w:r>") {
			t.Errorf("%s: the paragraph that starts the page holds something: %s", name, m[0])
		}
		if strings.Count(doc, "pageBreakBefore") != 1 || strings.Contains(doc, `w:type="page"`) {
			t.Errorf("%s: more than one break", name)
		}
	}

	// Nothing follows the cover: no page after it
	out, _ := coverDocx(t, "---\ntitle: T\nversion: 1\ncover: true\n---\n", theme.Default())
	if doc := docxPart(t, out, "word/document.xml"); !hasCover(doc) || strings.Contains(doc, "pageBreakBefore") {
		t.Errorf("a page after the cover:\n%s", doc)
	}
}

// The first heading is the title of a document without one in its front
// matter; with no title and no subtitle the block starts below the top.
func TestDOCXCoverTitleFromHeading(t *testing.T) {
	out, _ := coverDocx(t, "---\ncover: true\nversion: 3\n---\n\n# A Heading Title\n\ntext\n", theme.Default())
	doc := docxPart(t, out, "word/document.xml")
	cover := doc[:strings.Index(doc, "<w:pageBreakBefore/>")]
	if !strings.Contains(cover, `<w:pStyle w:val="Title">`) || !strings.Contains(cover, ">A Heading Title<") {
		t.Errorf("the heading is not the title:\n%s", cover)
	}
	if !strings.Contains(doc, `<w:pStyle w:val="Heading1">`) {
		t.Error("the heading is not in the text")
	}

	out, _ = coverDocx(t, "---\ncover: true\nversion: 3\n---\n\ntext\n", theme.Default())
	doc = docxPart(t, out, "word/document.xml")
	if hasCover(doc) || strings.Contains(doc, "Subtitle") {
		t.Error("a title where there is none")
	}
	if !strings.Contains(doc, `w:line="3200" w:lineRule="exact"`) {
		t.Errorf("the block does not start %d points down:\n%s", 160, doc)
	}
}

// Without the cover the document is as it was: no break, no first page of its own.
func TestDOCXNoCoverNoCoverParts(t *testing.T) {
	out, _ := coverDocx(t, "---\ntitle: T\nsubtitle: S\nversion: 1\n---\n\ntext\n", runningTheme())
	doc := docxPart(t, out, "word/document.xml")
	for _, not := range []string{"titlePg", "pageBreakBefore", `w:type="first"`, `w:val="Title"`} {
		if strings.Contains(doc, not) {
			t.Errorf("document.xml has %s", not)
		}
	}
	// The subtitle is a row of the block, as any other key
	if !strings.Contains(doc, ">subtitle<") || !strings.Contains(doc, ">S<") {
		t.Error("the subtitle is not a row")
	}
}

func TestDOCXCoverHasNoHeaderOrFooter(t *testing.T) {
	set := runningTheme()
	out, _ := coverDocx(t, "---\ntitle: The Title\nauthor: An Author\ncover: true\n---\n\n# H\n\ntext\n", set)
	doc := docxPart(t, out, "word/document.xml")

	// The first page of the section has parts of its own; the other pages
	// keep the theme's
	rels := docxPart(t, out, "word/_rels/document.xml.rels")
	types := docxPart(t, out, "[Content_Types].xml")
	targets := map[string]string{}
	for _, kind := range []string{"header", "footer"} {
		for _, typ := range []string{"default", "first"} {
			m := regexp.MustCompile(`<w:` + kind + `Reference w:type="` + typ + `" r:id="(\w+)">`).FindStringSubmatch(doc)
			if m == nil {
				t.Fatalf("no %s of the %s page referenced:\n%s", kind, typ, doc)
			}
			rel := regexp.MustCompile(`Id="` + m[1] + `"[^>]*Target="([^"]+)"`).FindStringSubmatch(rels)
			if rel == nil {
				t.Fatalf("relationship %s is not in the package", m[1])
			}
			if !strings.Contains(types, `PartName="/word/`+rel[1]+`"`) {
				t.Errorf("%s has no content type", rel[1])
			}
			targets[kind+typ] = docxPart(t, out, "word/"+rel[1])
		}
	}
	for _, kind := range []string{"header", "footer"} {
		first := targets[kind+"first"]
		if strings.Contains(first, "<w:t") || strings.Contains(first, "<w:tbl") || strings.Contains(first, "fldChar") || !strings.Contains(first, "<w:p>") {
			t.Errorf("the %s of the cover is not an empty paragraph:\n%s", kind, first)
		}
	}
	if !strings.Contains(targets["headerdefault"], ">The Title<") || !strings.Contains(targets["footerdefault"], ">An Author<") {
		t.Error("the text pages lost their header or footer")
	}

	// titlePg is among the properties of the section in the schema's order:
	// after the margins, before the end
	sect := doc[strings.LastIndex(doc, "<w:sectPr"):]
	order := []string{"<w:headerReference", "<w:footerReference", "<w:pgSz", "<w:pgMar", "<w:titlePg/>", "</w:sectPr>"}
	last := -1
	for _, el := range order {
		i := strings.Index(sect, el)
		if i < 0 || i < last {
			t.Fatalf("%s is out of place in\n%s", el, sect)
		}
		last = i
	}
	if strings.Count(doc, "titlePg") != 1 {
		t.Error("titlePg is not written once")
	}

	// Without a header and a footer in the theme there is nothing to hide
	out, _ = coverDocx(t, "---\ntitle: The Title\ncover: true\n---\n\ntext\n", theme.Default())
	if doc := docxPart(t, out, "word/document.xml"); strings.Contains(doc, "titlePg") || strings.Contains(doc, "Reference") {
		t.Error("a cover without header or footer asks for a first page of its own")
	}

	// A theme with only a footer
	set = theme.Default()
	set.DOCX.Footer.Center = "{page}"
	out, _ = coverDocx(t, "---\ntitle: The Title\ncover: true\n---\n\ntext\n", set)
	doc = docxPart(t, out, "word/document.xml")
	if !strings.Contains(doc, "titlePg") || !strings.Contains(doc, `<w:footerReference w:type="first"`) || strings.Contains(doc, "headerReference") {
		t.Errorf("a theme with a footer only:\n%s", doc)
	}
}

func TestDOCXVersionAndClassificationInFooter(t *testing.T) {
	set := theme.Default()
	set.DOCX.Header.Right = "{classification}"
	set.DOCX.Footer.Left = "{title} v{version}"
	set.DOCX.Footer.Right = "[{classification}] {page}/{pages}"
	out, _ := coverDocx(t, "---\ntitle: Policy\nversion: 1.2\nclassification: Belső\n---\n\ntext\n", set)

	footer := docxPart(t, out, "word/footer1.xml")
	for _, want := range []string{">Policy v1.2<", ">[Belső] <", ">PAGE<", ">NUMPAGES<"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the footer lacks %s:\n%s", want, footer)
		}
	}
	if !strings.Contains(docxPart(t, out, "word/header1.xml"), ">Belső<") {
		t.Error("the header lacks the classification")
	}
	for _, part := range []string{footer, docxPart(t, out, "word/header1.xml")} {
		if strings.Contains(part, "{") {
			t.Errorf("a placeholder is left: %s", part)
		}
	}
}
