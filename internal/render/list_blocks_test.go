package render

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

const itemBlocksDoc = "1. Step one:\n\n   ```\n   STEPCODE\n   ```\n\n   More text.\n\n   | a | b |\n   |---|---|\n   | CELL | 2 |\n\n   > - inner\n\n2. Step two.\n"

var traceX = regexp.MustCompile(`x=([\d.]+)`)

// xOf returns where the trace line holding text starts.
func xOf(t *testing.T, trace, text string) float64 {
	t.Helper()
	for _, line := range strings.Split(trace, "\n") {
		if strings.Contains(line, text) {
			x, _ := strconv.ParseFloat(traceX.FindStringSubmatch(line)[1], 64)
			return x
		}
	}
	t.Fatalf("%s is not drawn:\n%s", text, trace)
	return 0
}

// The blocks of a list item are drawn under its text, and the list goes on
// counting after them.
func TestListItemBlocksInPDF(t *testing.T) {
	trace := pdfTrace(t, itemBlocksDoc)
	text := xOf(t, trace, `"Step one"`)
	for _, inside := range []string{`"STEPCODE"`, `"More"`, `"CELL"`} {
		if x := xOf(t, trace, inside); x < text {
			t.Errorf("%s starts at x=%.1f, left of the item's text (%.1f)", inside, x, text)
		}
	}
	if !strings.Contains(trace, `"2. "`) {
		t.Errorf("the second item should be numbered 2:\n%s", trace)
	}
}

func TestListItemBlocksInDOCX(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(itemBlocksDoc), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	outline := docxOutline(t, out)
	for _, want := range []string{
		`"STEPCODE"`, `"CELL"`, `"More`,
		"P{level=0 list=2", // Word numbers the steps
		"INDENT 760",       // the code block and the table are moved in
		"P{indent=760",     // and so is the paragraph
	} {
		if !strings.Contains(outline, want) {
			t.Errorf("missing %s in:\n%s", want, outline)
		}
	}
	if n := strings.Count(outline, "INDENT 760"); n != 2 {
		t.Errorf("%d tables are moved in, want 2", n)
	}
}

func TestIndentTables(t *testing.T) {
	doc := `<w:tbl><w:tblPr><w:tblW w:w="1"></w:tblW><w:tblLook w:val="04A0"></w:tblLook></w:tblPr></w:tbl>` +
		`<w:tbl><w:tblPr><w:tblW w:w="2"></w:tblW></w:tblPr></w:tbl>` +
		`<w:tbl><w:tblPr><w:tblW w:w="3"></w:tblW></w:tblPr></w:tbl>`
	got := string(indentTables([]byte(doc), []int{300, 0, 500}))
	want := `<w:tbl><w:tblPr><w:tblW w:w="1"></w:tblW><w:tblInd w:w="300" w:type="dxa"/><w:tblLook w:val="04A0"></w:tblLook></w:tblPr></w:tbl>` +
		`<w:tbl><w:tblPr><w:tblW w:w="2"></w:tblW></w:tblPr></w:tbl>` +
		`<w:tbl><w:tblPr><w:tblW w:w="3"></w:tblW><w:tblInd w:w="500" w:type="dxa"/></w:tblPr></w:tbl>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// A count that does not match the document leaves it alone
	if got := string(indentTables([]byte(doc), []int{300})); got != doc {
		t.Errorf("document changed: %s", got)
	}
}

// A nested list is bulleted or numbered as it is written, whatever the list
// around it is, and its numbering starts again under each item.
func TestNestedListsKeepTheirKind(t *testing.T) {
	trace := pdfTrace(t, "1. one\n   - bullet\n2. two\n   1. first\n   2. second\n3. three\n   1. again\n")
	var prefixes []string
	for _, d := range drawnTexts(t, trace) {
		if strings.HasSuffix(d.text, " ") {
			prefixes = append(prefixes, strings.TrimSpace(d.text))
		}
	}
	want := "1. ◦ 2. a. b. 3. a."
	if got := strings.Join(prefixes, " "); got != want {
		t.Errorf("list markers are %q, want %q", got, want)
	}
}
