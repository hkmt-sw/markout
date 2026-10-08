package render

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

func renderDocx(t *testing.T, md string, fl flavor.Flavor) string {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), fl, "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	return out
}

const structureDoc = `# Title

[[_TOC_]]

Go to [the steps](#steps), to [Árvíz](#árvíztűrő-tükör) or [nowhere](#nope).

## Steps

1. one
   - bullet
2. two
   1. sub
3. three

Another list:

1. alpha

- [ ] task

### With ` + "`code`" + `

## Árvíztűrő tükör

## Steps
`

// Headings are in Word's heading styles and carry no formatting of their
// own; lists are Word lists; links and the table of contents point at
// bookmarks on the headings.
func TestDOCXHasWordStructure(t *testing.T) {
	gitlab, _ := flavor.ByID("gitlab")
	outline := docxOutline(t, renderDocx(t, structureDoc, gitlab))

	for what, want := range map[string]string{
		"heading 1":            "P{bookmark=_title pStyle=Heading1}\n  R \"Title\"",
		"heading 2":            "P{bookmark=_steps pStyle=Heading2}\n  R \"Steps\"",
		"code in a heading":    `R{color=CF222E font=Consolas} "code"`,
		"heading style":        "STYLE Heading2{b color=auto font=Georgia keepLines keepNext outline=1",
		"body style":           "STYLE Normal{color=1A1A2E font=Georgia sz=22}",
		"body text":            `R "Go to "`,
		"link to a heading":    `link=#_steps rStyle=Hyperlink u=single} "the steps"`,
		"link by GitHub slug":  `link=#_rvztr-tkr`,
		"numbered item":        "P{level=0 list=2 ",
		"bullet under it":      "P{level=1 list=1 ",
		"numbered under it":    "P{level=1 list=3 ",
		"second numbered list": "P{level=0 list=4 ",
		"lists restart":        "LIST 4 definition=1 restarts",
		"task":                 `R "☐ "`,
		"TOC entry":            "P{pStyle=TOC1}\n  FIELD begin",
		"TOC field":            `TOC \\o \"1-6\" \\h \\z \\u`,
		"TOC entry link":       `link=#_steps rStyle=Hyperlink} "Steps"`,
		"TOC nested entry":     "P{pStyle=TOC3}",
		"same title twice":     "bookmark=_steps_1",
	} {
		want = strings.ReplaceAll(want, "_rvztr-tkr", "_rvztr_tkr")
		if !strings.Contains(outline, want) {
			t.Errorf("%s: missing %q", what, want)
		}
	}
	for what, unwanted := range map[string]string{
		"typed number": `"1. "`,
		"typed bullet": `"• "`,
		"TOC marks":    "markout-toc",
	} {
		if strings.Contains(outline, unwanted) {
			t.Errorf("%s: %q should not be in the document", what, unwanted)
		}
	}
	if t.Failed() {
		t.Logf("outline:\n%s", outline)
	}
}

// The parts written by hand are well-formed, and what the document refers
// to exists: every link target is a bookmark, every list is defined, and the
// field around the table of contents is closed.
func TestDOCXStructureIsConsistent(t *testing.T) {
	showcase, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "showcase.md"))
	if err != nil {
		t.Fatal(err)
	}
	gitlab, _ := flavor.ByID("gitlab")
	for name, md := range map[string]string{"structure": structureDoc, "showcase": string(showcase)} {
		t.Run(name, func(t *testing.T) {
			out := renderDocx(t, md, gitlab)
			document := docxPart(t, out, "word/document.xml")
			numbering := docxPart(t, out, "word/numbering.xml")
			for part, content := range map[string]string{
				"document.xml": document, "numbering.xml": numbering, "styles.xml": docxPart(t, out, "word/styles.xml"),
			} {
				if err := xml.Unmarshal([]byte(content), new(struct{})); err != nil {
					t.Errorf("%s is not well-formed: %v", part, err)
				}
			}

			all := func(pattern, in string) []string {
				var out []string
				for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(in, -1) {
					out = append(out, m[1])
				}
				return out
			}
			names, ids := map[string]bool{}, map[string]bool{}
			for _, n := range all(`<w:bookmarkStart[^>]*w:name="([^"]+)"`, document) {
				if names[n] {
					t.Errorf("bookmark %s is there twice", n)
				}
				if len([]rune(n)) > maxBookmarkName {
					t.Errorf("bookmark name %s is longer than Word allows", n)
				}
				names[n] = true
			}
			for _, id := range all(`<w:bookmarkStart[^>]*w:id="([^"]+)"`, document) {
				if ids[id] {
					t.Errorf("bookmark id %s is used twice", id)
				}
				ids[id] = true
			}
			for _, anchor := range all(`w:anchor="([^"]+)"`, document) {
				if !names[anchor] && anchor != "nope" {
					t.Errorf("a link goes to %s, which is not a bookmark", anchor)
				}
			}
			defined := map[string]bool{}
			for _, id := range all(`<w:num w:numId="(\d+)"`, numbering) {
				defined[id] = true
			}
			for _, id := range all(`<w:numId w:val="(\d+)"`, document) {
				if !defined[id] {
					t.Errorf("list %s is used but not defined", id)
				}
			}
			begin := strings.Count(document, `w:fldCharType="begin"`)
			if end := strings.Count(document, `w:fldCharType="end"`); begin != end {
				t.Errorf("%d fields begin and %d end", begin, end)
			}
		})
	}
}

func TestReplaceStyles(t *testing.T) {
	doc := `<w:styles><w:style w:type="paragraph" w:styleId="Normal" w:default="true"><w:name w:val="Normal"></w:name></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Other"><w:name w:val="Other"></w:name></w:style></w:styles>`
	got := string(replaceStyles([]byte(doc), map[string]string{"Normal": "<NORMAL/>", "New": "<NEW/>"}))
	want := `<w:styles><NORMAL/><w:style w:type="paragraph" w:styleId="Other"><w:name w:val="Other"></w:name></w:style><NEW/></w:styles>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
