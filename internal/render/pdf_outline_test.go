package render

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

const outlineDoc = `# Title

[[_TOC_]]

Go to [the steps](#steps), to [Árvíz](#árvíztűrő-tükör) or [nowhere](#nope),
or [out](https://example.com).

## Steps

### Detail one

### Detail two

## Árvíztűrő tükör

# Appendix

## Last part
`

// pdfBookmark is an entry of a PDF's outline, read back from the file.
type pdfBookmark struct {
	title                           string
	parent, prev, next, first, last int
}

var (
	pdfObject = regexp.MustCompile(`(?s)(\d+) 0 obj\n<<(.*?)>>\nendobj`)
	pdfRef    = func(name string) *regexp.Regexp { return regexp.MustCompile(`/` + name + ` (\d+) 0 R`) }
	pdfTitle  = regexp.MustCompile(`/Title <FEFF([0-9A-Fa-f]*)>`)
)

func refIn(dict, name string) int {
	if m := pdfRef(name).FindStringSubmatch(dict); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// readOutline returns the outline's own object and its entries by object
// number.
func readOutline(t *testing.T, pdf []byte) (root string, entries map[int]pdfBookmark) {
	t.Helper()
	entries = map[int]pdfBookmark{}
	for _, m := range pdfObject.FindAllSubmatch(pdf, -1) {
		num, _ := strconv.Atoi(string(m[1]))
		dict := string(m[2])
		if strings.Contains(dict, "/Type /Outlines") {
			if num != outlineRootObject {
				t.Errorf("the outline is object %d, not %d as nestOutline assumes", num, outlineRootObject)
			}
			root = dict
			continue
		}
		title := pdfTitle.FindStringSubmatch(dict)
		if title == nil {
			continue
		}
		var units []uint16
		for i := 0; i+4 <= len(title[1]); i += 4 {
			u, _ := strconv.ParseUint(title[1][i:i+4], 16, 16)
			units = append(units, uint16(u))
		}
		entries[num] = pdfBookmark{
			title:  string(utf16.Decode(units)),
			parent: refIn(dict, "Parent"), prev: refIn(dict, "Prev"), next: refIn(dict, "Next"),
			first: refIn(dict, "First"), last: refIn(dict, "Last"),
		}
	}
	return root, entries
}

// outlineTree walks the outline the way a viewer does, from the first entry
// of each level to the next, and writes it as indented titles. It checks
// that the links between the entries agree with each other on the way.
func outlineTree(t *testing.T, entries map[int]pdfBookmark, parent, first, last int, indent string) string {
	t.Helper()
	var out strings.Builder
	prev, steps := 0, 0
	for at := first; at != 0; at = entries[at].next {
		e, ok := entries[at]
		if !ok || steps > len(entries) {
			t.Fatalf("the outline leads to object %d, which is not a bookmark", at)
		}
		steps++
		if e.parent != parent {
			t.Errorf("%q has parent %d, want %d", e.title, e.parent, parent)
		}
		if e.prev != prev {
			t.Errorf("%q has %d before it, want %d", e.title, e.prev, prev)
		}
		out.WriteString(indent + e.title + "\n")
		if e.first != 0 {
			out.WriteString(outlineTree(t, entries, at, e.first, e.last, indent+"  "))
		}
		prev = at
	}
	if prev != last {
		t.Errorf("under object %d the last bookmark is %d, but %d is named as the last", parent, prev, last)
	}
	return out.String()
}

func renderPDF(t *testing.T, md string) (file []byte, trace string) {
	t.Helper()
	gitlab, _ := flavor.ByID("gitlab")
	doc, err := parse.ParseFlavor([]byte(md), gitlab, "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.pdf")
	r := NewPdfRenderer()
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, out); err != nil {
		t.Fatal(err)
	}
	file, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return file, r.trace.String()
}

// The headings are the PDF's bookmarks, nested by level.
func TestPDFBookmarks(t *testing.T) {
	pdf, _ := renderPDF(t, outlineDoc)
	root, entries := readOutline(t, pdf)
	if root == "" {
		t.Fatal("the PDF has no outline")
	}
	got := outlineTree(t, entries, outlineRootObject, refIn(root, "First"), refIn(root, "Last"), "")
	want := "Title\n  Steps\n    Detail one\n    Detail two\n  Árvíztűrő tükör\nAppendix\n  Last part\n"
	if got != want {
		t.Errorf("outline:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(root, "/Count 2 ") && !strings.Contains(root, "/Count 2\n") {
		t.Errorf("the outline should count its 2 top-level bookmarks: %s", root)
	}

	// Changing the outline's object must not move anything in the file
	at := bytes.LastIndex(pdf, []byte("startxref"))
	fields := strings.Fields(string(pdf[at:]))
	offset, _ := strconv.Atoi(fields[1])
	if offset >= len(pdf) || !bytes.HasPrefix(pdf[offset:], []byte("xref")) {
		t.Errorf("the cross-reference table is not where the file says it is")
	}
	for _, m := range pdfObject.FindAllSubmatchIndex(pdf, -1) {
		num := string(pdf[m[2]:m[3]])
		entry := regexp.MustCompile(`(?m)^` + strconv.Itoa(m[0] + 10000000000)[1:] + ` 00000 n`)
		if num == strconv.Itoa(outlineRootObject) && !entry.Match(pdf[offset:]) {
			t.Errorf("the outline's object is not at the offset the cross-reference table has")
		}
	}
}

// A document without headings has no outline.
func TestPDFWithoutHeadingsHasNoOutline(t *testing.T) {
	pdf, _ := renderPDF(t, "Just text.\n")
	if bytes.Contains(pdf, []byte("/PageMode /UseOutlines")) {
		t.Error("a PDF without headings should not ask for the bookmarks pane")
	}
}

// Links to headings, and the entries of a table of contents, jump within the
// document; links elsewhere stay links to an address.
func TestPDFLinksToHeadings(t *testing.T) {
	pdf, trace := renderPDF(t, outlineDoc)
	for what, want := range map[string]string{
		"heading anchor":      " steps\n",
		"link by ID":          "goto  x=",
		"link by GitHub slug": " rvztr-tkr\n",
	} {
		if !strings.Contains(trace, want) {
			t.Errorf("%s: missing %q in:\n%s", what, want, trace)
		}
	}
	// 7 table of contents entries (the heading before it included), each a
	// link twice, as its title and as its page number, and two links in the
	// text; the link to a heading that does not exist is none
	internal := regexp.MustCompile(`/Subtype /Link [^\n]*/Dest \[\d+ 0 R /XYZ`).FindAll(pdf, -1)
	if len(internal) != 16 {
		t.Errorf("%d links within the document, want 16", len(internal))
	}
	if n := bytes.Count(pdf, []byte("/URI (https://example.com)")); n != 1 {
		t.Errorf("%d links to example.com, want 1", n)
	}
	if bytes.Contains(pdf, []byte("/URI (#")) {
		t.Error("a link to a heading is written as a link to an address")
	}
}

func TestFixOutlineRoot(t *testing.T) {
	const obj = "3 0 obj\n<<\n\t/Type /Outlines\n\t/Count 12\n\t/First 31 0 R\n\t/Last 120 0 R\n>>\nendobj\nrest"
	got := string(fixOutlineRoot([]byte(obj), 3, 97))
	want := "3 0 obj\n<<\n\t/Type /Outlines\n\t/Count 3 \n\t/First 31 0 R\n\t/Last 97 0 R \n>>\nendobj\nrest"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if len(got) != len(obj) {
		t.Errorf("the file changed length: %d to %d", len(obj), len(got))
	}
	// A number that would not fit is left alone
	if got := string(fixOutlineRoot([]byte(obj), 3, 1000)); got != obj {
		t.Errorf("object changed: %q", got)
	}
}
