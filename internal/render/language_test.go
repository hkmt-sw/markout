package render

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// The language of the document is the default language of the text in a
// DOCX, with the region word processors want; without one nothing is said.
func TestDocxLanguage(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("# Title\n\ntext\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	for tag, want := range map[string]string{"hu": `<w:lang w:val="hu-HU"/>`, "en-GB": `<w:lang w:val="en-GB"/>`, "fil": `<w:lang w:val="fil"/>`, "": ""} {
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := RenderDocx(doc, out, Options{Lang: tag}); err != nil {
			t.Fatal(err)
		}
		styles := docxPart(t, out, "word/styles.xml")
		if want == "" {
			if strings.Contains(styles, "<w:lang") {
				t.Errorf("no language: the styles name one")
			}
			continue
		}
		if !strings.Contains(styles, "<w:docDefaults><w:rPrDefault><w:rPr>"+want+"</w:rPr></w:rPrDefault>") {
			t.Errorf("%s: the styles do not have %s as the default", tag, want)
		}
	}
}

var (
	pdfStartXref = regexp.MustCompile(`startxref\n(\d+)\n%%EOF\s*$`)
	pdfXrefHead  = regexp.MustCompile(`^xref\n0 (\d+)\n`)
)

// The language is in the catalog of a PDF, and the table of objects still
// says where every object is.
func TestPdfLanguage(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("# Title\n\ntext with a formula $a^2$ and 𝑥\n\n## More\n\ntext\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"hu", "en-GB", ""} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		if err := RenderPdf(doc, out, Options{Lang: tag}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		pdf := string(data)
		if want := "/Type /Catalog\n  /Lang (" + tag + ")\n"; tag != "" && !strings.Contains(pdf, want) {
			t.Errorf("%s: the catalog does not name the language", tag)
		}
		if tag == "" && strings.Contains(pdf, "/Lang") {
			t.Error("no language: the PDF names one")
		}
		checkObjectTable(t, tag, pdf)
	}
}

// checkObjectTable fails the test unless every object of a PDF is where its
// table of objects says.
func checkObjectTable(t *testing.T, name, pdf string) {
	t.Helper()
	m := pdfStartXref.FindStringSubmatch(pdf)
	if m == nil {
		t.Fatalf("%s: no startxref", name)
	}
	start, _ := strconv.Atoi(m[1])
	head := pdfXrefHead.FindStringSubmatch(pdf[start:])
	if head == nil {
		t.Fatalf("%s: startxref does not point at the table of objects", name)
	}
	count, _ := strconv.Atoi(head[1])
	entries := pdf[start+len(head[0]):]
	for i := 1; i < count; i++ {
		offset, _ := strconv.Atoi(entries[20*i : 20*i+10])
		if want := strconv.Itoa(i) + " 0 obj"; !strings.HasPrefix(pdf[offset:], want) {
			t.Fatalf("%s: object %d is not where the table says", name, i)
		}
	}
}
