package render

import (
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// pdfBytes converts Markdown to PDF and returns the file and the warnings.
func pdfBytes(t *testing.T, md string, opts Options) ([]byte, []string) {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	opts.Warn = func(w string) { warnings = append(warnings, w) }
	out := filepath.Join(t.TempDir(), "out.pdf")
	if err := RenderPdf(doc, out, opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data, warnings
}

// sample is a small document with what the repairs are for: text in a font, a
// link to the outside and one within the document.
const sample = "---\ntitle: A <Policy> & \"more\"\nauthor: Márton Tamás\ndate: 2026-09-20\nlang: hu\n---\n\n" +
	"# Heading\n\nSome text with [a link](https://example.com) and [another](#heading). Árvíztűrő.\n"

// checkXref fails unless the table of objects of the PDF says where each
// object is, and the file ends the way a PDF does.
func checkXref(t *testing.T, pdf []byte) {
	t.Helper()
	head := xrefHead.FindSubmatchIndex(pdf)
	tail := xrefTail.FindSubmatchIndex(pdf)
	if head == nil || tail == nil {
		t.Fatal("the PDF has no table of objects")
	}
	count, _ := strconv.Atoi(string(pdf[head[2]:head[3]]))
	for i := 1; i < count; i++ {
		entry := pdf[head[1]+20*i : head[1]+20*i+20]
		offset, err := strconv.Atoi(string(entry[:10]))
		if err != nil || !bytes.HasPrefix(pdf[offset:], []byte(strconv.Itoa(i)+" 0 obj\n")) {
			t.Fatalf("object %d is not where the table says (%s)", i, entry[:10])
		}
	}
	start, _ := strconv.Atoi(string(pdf[tail[2]:tail[3]]))
	if !bytes.HasPrefix(pdf[start:], []byte("xref\n")) {
		t.Fatalf("startxref does not point at the table of objects")
	}
}

// streamsWithoutEOL counts the streams that end without an end of line
// before "endstream".
func streamsWithoutEOL(t *testing.T, pdf []byte) int {
	t.Helper()
	edits, err := endstreamEdits(pdf)
	if err != nil {
		t.Fatal(err)
	}
	return len(edits)
}

func TestPDFAFor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       map[string]any
		requested bool
		pdfa      bool
		warning   string
	}{
		{"nothing said, not asked", map[string]any{"title": "T"}, false, false, ""},
		{"nothing said, asked", map[string]any{"title": "T"}, true, true, ""},
		{"the document asks", map[string]any{"pdfa": true}, false, true, ""},
		{"the document refuses", map[string]any{"pdfa": false}, true, false, ""},
		{"key in other case", map[string]any{"PDFA": true}, false, true, ""},
		{"true, requested false", map[string]any{"pdfa": true}, false, true, ""},
		{"false, requested false", map[string]any{"pdfa": false}, false, false, ""},
		{"string \"true\", requested false", map[string]any{"pdfa": "true"}, false, true, ""},
		{"string \"False\", requested false", map[string]any{"pdfa": "False"}, false, false, ""},
		{"\"yes\", requested false", map[string]any{"pdfa": "yes"}, false, true, ""},
		{"\"YES\", requested false", map[string]any{"pdfa": "YES"}, false, true, ""},
		{"\" no \", requested false", map[string]any{"pdfa": " no "}, false, false, ""},
		{"\"maybe\", requested false", map[string]any{"pdfa": "maybe"}, false, false, "pdfa in the front matter must be true, false, yes or no, not \"maybe\"; it was ignored."},
		{"number, requested false", map[string]any{"pdfa": int64(1)}, false, false, "pdfa in the front matter must be true, false, yes or no, not \"1\"; it was ignored."},
		{"missing key, requested false", map[string]any{"title": "T"}, false, false, ""},
		{"true, requested true", map[string]any{"pdfa": true}, true, true, ""},
		{"false, requested true", map[string]any{"pdfa": false}, true, false, ""},
		{"string \"true\", requested true", map[string]any{"pdfa": "true"}, true, true, ""},
		{"string \"False\", requested true", map[string]any{"pdfa": "False"}, true, false, ""},
		{"\"yes\", requested true", map[string]any{"pdfa": "yes"}, true, true, ""},
		{"\"YES\", requested true", map[string]any{"pdfa": "YES"}, true, true, ""},
		{"\" no \", requested true", map[string]any{"pdfa": " no "}, true, false, ""},
		{"\"maybe\", requested true", map[string]any{"pdfa": "maybe"}, true, true, "pdfa in the front matter must be true, false, yes or no, not \"maybe\"; it was ignored."},
		{"number, requested true", map[string]any{"pdfa": int64(1)}, true, true, "pdfa in the front matter must be true, false, yes or no, not \"1\"; it was ignored."},
		{"missing key, requested true", map[string]any{"title": "T"}, true, true, ""},
	} {
		pdfa, warning := pdfaFor(ast.FrontMatter{Raw: tc.raw}, tc.requested)
		if pdfa != tc.pdfa || warning != tc.warning {
			t.Errorf("%s: got %v, %q; want %v, %q", tc.name, pdfa, warning, tc.pdfa, tc.warning)
		}
	}
}

// The first repair: the fonts say how character codes map to glyphs.
func TestCIDToGIDMapRepair(t *testing.T) {
	plain, _ := pdfBytes(t, sample, Options{})
	fonts := bytes.Count(plain, []byte("/Subtype /CIDFontType2"))
	if fonts == 0 || bytes.Contains(plain, []byte("/CIDToGIDMap")) {
		t.Fatalf("the plain PDF has %d fonts and CIDToGIDMap should be in none", fonts)
	}
	fixed := editPDF(plain, cidToGIDMapEdits(plain))
	if got := bytes.Count(fixed, []byte("/CIDToGIDMap /Identity\n/Subtype /CIDFontType2")); got != fonts {
		t.Errorf("%d of %d fonts have CIDToGIDMap", got, fonts)
	}
	checkXref(t, fixed)
}

// The second: every stream has an end of line before endstream.
func TestEndstreamRepair(t *testing.T) {
	plain, _ := pdfBytes(t, sample, Options{})
	if streamsWithoutEOL(t, plain) == 0 {
		t.Fatal("gopdf writes every stream with an end of line now; the repair is not needed")
	}
	edits, _ := endstreamEdits(plain)
	fixed := editPDF(plain, edits)
	if n := streamsWithoutEOL(t, fixed); n != 0 {
		t.Errorf("%d streams still have no end of line", n)
	}
	checkXref(t, fixed)

	// A stream that is not as long as it says cannot be repaired
	length := regexp.MustCompile(`/Length (\d+)`).FindSubmatchIndex(plain)
	n, _ := strconv.Atoi(string(plain[length[2]:length[3]]))
	broken := append(append(append([]byte{}, plain[:length[2]]...), strconv.Itoa(n+5)...), plain[length[3]:]...)
	if _, err := endstreamEdits(broken); err == nil {
		t.Error("a stream of the wrong length was accepted")
	}
}

// The third: links print.
func TestLinkFlagRepair(t *testing.T) {
	plain, _ := pdfBytes(t, sample, Options{})
	links := bytes.Count(plain, []byte("/Subtype /Link"))
	if links != 2 || bytes.Contains(plain, []byte("/F 4")) {
		t.Fatalf("the plain PDF has %d links and flags", links)
	}
	fixed := editPDF(plain, linkFlagEdits(plain))
	if got := bytes.Count(fixed, []byte("/Subtype /Link /F 4")); got != links {
		t.Errorf("%d of %d links have the flag", got, links)
	}
	// Once is enough
	if again := linkFlagEdits(fixed); len(again) != 0 {
		t.Errorf("flagged the links again: %d edits", len(again))
	}
	checkXref(t, fixed)
}

// info reads the Info dictionary and the XMP stream of a PDF/A file.
func readMetadata(t *testing.T, pdf []byte) (info map[string]string, xmp string) {
	t.Helper()
	obj := regexp.MustCompile(`/Info (\d+) 0 R`).FindSubmatch(pdf[bytes.LastIndex(pdf, []byte("trailer")):])
	if obj == nil {
		t.Fatal("the trailer has no Info")
	}
	dict := regexp.MustCompile(`(?s)\n` + string(obj[1]) + ` 0 obj\n<<\n(.*?)>>\nendobj`).FindSubmatch(pdf)
	if dict == nil {
		t.Fatal("the Info dictionary is not there")
	}
	info = map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(dict[1])), "\n") {
		key, value, _ := strings.Cut(line, " ")
		key = strings.TrimPrefix(key, "/")
		switch {
		case strings.HasPrefix(value, "<FEFF"):
			raw, err := hex.DecodeString(strings.Trim(value, "<>")[4:])
			if err != nil {
				t.Fatal(err)
			}
			units := make([]uint16, len(raw)/2)
			for i := range units {
				units[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
			}
			info[key] = string(utf16.Decode(units))
		default:
			info[key] = strings.Trim(value, "()")
		}
	}
	m := regexp.MustCompile(`(?s)/Type /Metadata /Subtype /XML /Length (\d+) >>\nstream\n`).FindSubmatchIndex(pdf)
	if m == nil {
		t.Fatal("no XMP stream")
	}
	length, _ := strconv.Atoi(string(pdf[m[2]:m[3]]))
	return info, string(pdf[m[1] : m[1]+length])
}

// The fourth, fifth and sixth: the file has an identifier, metadata that
// says what the Info dictionary says, and an output intent.
func TestPDFAMetadata(t *testing.T) {
	pdf, warnings := pdfBytes(t, sample, Options{PDFA: true, Lang: "hu"})
	if len(warnings) != 0 {
		t.Errorf("warnings: %v", warnings)
	}
	checkXref(t, pdf)

	// The identifier: two equal strings of 16 bytes
	id := regexp.MustCompile(`/ID \[<([0-9A-F]{32})> <([0-9A-F]{32})>\]`).FindSubmatch(pdf)
	if id == nil || !bytes.Equal(id[1], id[2]) {
		t.Errorf("the identifier is %q", id)
	}

	// The metadata: well formed, and the same as the Info dictionary
	info, xmp := readMetadata(t, pdf)
	if !strings.HasPrefix(xmp, "<?xpacket begin=\""+byteOrderMark+"\"") || !strings.HasSuffix(xmp, "<?xpacket end=\"w\"?>") {
		t.Errorf("the XMP packet is %.60q ... %.30q", xmp, xmp[max(0, len(xmp)-30):])
	}
	var tree struct {
		Part        string   `xml:"RDF>Description>part"`
		Conformance string   `xml:"RDF>Description>conformance"`
		Title       string   `xml:"RDF>Description>title>Alt>li"`
		Creator     string   `xml:"RDF>Description>creator>Seq>li"`
		Language    []string `xml:"RDF>Description>language>Bag>li"`
		Producer    string   `xml:"RDF>Description>Producer"`
		Created     string   `xml:"RDF>Description>CreateDate"`
		Modified    string   `xml:"RDF>Description>ModifyDate"`
	}
	if err := xml.Unmarshal([]byte(xmp[strings.Index(xmp, "<x:xmpmeta"):strings.Index(xmp, "<?xpacket end")]), &tree); err != nil {
		t.Fatalf("the XMP is not XML: %v\n%s", err, xmp)
	}
	if tree.Part != "2" || tree.Conformance != "U" {
		t.Errorf("pdfaid is %q %q, want 2 U", tree.Part, tree.Conformance)
	}
	const title = `A <Policy> & "more"`
	if tree.Title != title || info["Title"] != title {
		t.Errorf("title: XMP %q, Info %q; want %q", tree.Title, info["Title"], title)
	}
	if tree.Creator != "Márton Tamás" || info["Author"] != tree.Creator {
		t.Errorf("author: XMP %q, Info %q", tree.Creator, info["Author"])
	}
	if len(tree.Language) != 1 || tree.Language[0] != "hu" {
		t.Errorf("language: %v", tree.Language)
	}
	if tree.Producer != info["Producer"] || tree.Producer != "markout" {
		t.Errorf("producer: XMP %q, Info %q", tree.Producer, info["Producer"])
	}
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if info["CreationDate"] != pdfDate(day) || info["ModDate"] != info["CreationDate"] {
		t.Errorf("Info dates: %q %q", info["CreationDate"], info["ModDate"])
	}
	if tree.Created != "2026-09-20T00:00:00Z" || tree.Modified != tree.Created {
		t.Errorf("XMP dates: %q %q", tree.Created, tree.Modified)
	}

	// The output intent names a profile of three components
	if !regexp.MustCompile(`/Metadata \d+ 0 R\n`).Match(pdf) ||
		!bytes.Contains(pdf, []byte("/S /GTS_PDFA1")) ||
		!bytes.Contains(pdf, []byte("/OutputConditionIdentifier (sRGB IEC61966-2.1)")) {
		t.Error("the catalog does not have the metadata and the output intent")
	}
	profile := regexp.MustCompile(`/DestOutputProfile (\d+) 0 R`).FindSubmatch(pdf)
	if profile == nil {
		t.Fatal("the output intent has no profile")
	}
	body := regexp.MustCompile(`\n` + string(profile[1]) + ` 0 obj\n<< /N 3 /Length (\d+) >>\nstream\n`).FindSubmatchIndex(pdf)
	if body == nil {
		t.Fatal("the profile is not a stream with /N 3")
	}
	length, _ := strconv.Atoi(string(pdf[body[2]:body[3]]))
	if !bytes.Equal(pdf[body[1]:body[1]+length], srgbProfile) {
		t.Error("the profile in the file is not the embedded one")
	}
}

// What the Info dictionary has is left out when the document has no such
// thing, in the XMP as well.
func TestPDFAMetadataWithoutFrontMatter(t *testing.T) {
	pdf, _ := pdfBytes(t, "# The Title\n\ntext\n", Options{PDFA: true})
	info, xmp := readMetadata(t, pdf)
	if info["Title"] != "The Title" || info["Author"] != "" || info["CreationDate"] != "" {
		t.Errorf("Info: %v", info)
	}
	for _, absent := range []string{"dc:creator", "dc:language", "CreateDate", "ModifyDate"} {
		if strings.Contains(xmp, absent) {
			t.Errorf("the XMP has %s", absent)
		}
	}
	if !strings.Contains(xmp, "The Title") || !strings.Contains(xmp, "pdfaid:part") {
		t.Errorf("the XMP is missing the title or pdfaid:\n%s", xmp)
	}
}

// Text that XML cannot hold is left out of both, and a date with a zone keeps
// it.
func TestMetaText(t *testing.T) {
	if got := metaText("A\x00 title\x0b\nin  two\tlines￿"); got != "A title in two lines" {
		t.Errorf("metaText = %q", got)
	}
	m := metaOf(docInfo{title: "T", date: "2026-10-14T12:30:00+02:00"}, "")
	if got := pdfDate(m.date); got != "D:20261014123000+02'00'" {
		t.Errorf("pdfDate = %q", got)
	}
	if got := metaOf(docInfo{date: "next Tuesday"}, "").date; !got.IsZero() {
		t.Errorf("a date no one can read was read as %v", got)
	}
}

// The same document gives the same file, and another gives another
// identifier.
func TestPDFAIsDeterministic(t *testing.T) {
	a, _ := pdfBytes(t, sample, Options{PDFA: true})
	b, _ := pdfBytes(t, sample, Options{PDFA: true})
	if !bytes.Equal(a, b) {
		t.Error("converting twice gave two files")
	}
	c, _ := pdfBytes(t, sample+"\nMore.\n", Options{PDFA: true})
	idOf := func(pdf []byte) string { return string(regexp.MustCompile(`/ID \[<(\w+)>`).FindSubmatch(pdf)[1]) }
	if idOf(a) == idOf(c) {
		t.Error("two documents have the same identifier")
	}
}

// The six things are in a PDF/A file and in no other.
func TestPDFAOnRequestOnly(t *testing.T) {
	marks := []string{"/ID [", "/Type /Metadata", "/OutputIntents", "/CIDToGIDMap", "/Subtype /Link /F 4", "pdfaid"}
	has := func(pdf []byte) (found []string) {
		for _, m := range marks {
			if bytes.Contains(pdf, []byte(m)) {
				found = append(found, m)
			}
		}
		return found
	}

	if found := has(mustPDF(t, sample, Options{})); len(found) != 0 {
		t.Errorf("a plain PDF has %v", found)
	}
	for _, tc := range []struct {
		name string
		md   string
		opts Options
		pdfa bool
	}{
		{"asked of the conversion", sample, Options{PDFA: true}, true},
		{"asked by the document", strings.Replace(sample, "lang: hu", "lang: hu\npdfa: true", 1), Options{}, true},
		{"refused by the document", strings.Replace(sample, "lang: hu", "lang: hu\npdfa: false", 1), Options{PDFA: true}, false},
	} {
		found := has(mustPDF(t, tc.md, tc.opts))
		if tc.pdfa && len(found) != len(marks) {
			t.Errorf("%s: only %v", tc.name, found)
		}
		if !tc.pdfa && len(found) != 0 {
			t.Errorf("%s: has %v", tc.name, found)
		}
	}
}

func mustPDF(t *testing.T, md string, opts Options) []byte {
	t.Helper()
	data, _ := pdfBytes(t, md, opts)
	return data
}

// A pdfa value that is not a yes or no is ignored, with a warning,
// and is never a row of the control block.
func TestPDFAKeyIsChecked(t *testing.T) {
	const md = "---\ntitle: T\npdfa: maybe\n---\n\ntext\n"
	pdf, warnings := pdfBytes(t, md, Options{})
	if bytes.Contains(pdf, []byte("/OutputIntents")) {
		t.Error("made PDF/A although the key is not true")
	}
	if len(warnings) != 1 || warnings[0] != "pdfa in the front matter must be true, false, yes or no, not \"maybe\"; it was ignored." {
		t.Errorf("warnings: %q", warnings)
	}
	if _, warnings := pdfBytes(t, md, Options{PDFA: true}); len(warnings) != 1 {
		t.Errorf("warnings with PDF/A asked: %q", warnings)
	}
	if trace, _ := coverRender(t, "---\ntitle: T\npdfa: true\n---\n\ntext\n", theme.Default()); strings.Contains(trace, "pdfa") {
		t.Error("the control block shows pdfa")
	}
}

// DOCX has no PDF/A: the key is not an error, a warning or a row.
func TestPDFAKeyInDocx(t *testing.T) {
	for _, value := range []string{"true", "maybe"} {
		doc, err := parse.ParseFlavor([]byte("---\ntitle: T\npdfa: "+value+"\n---\n\ntext\n"), flavor.Default(), "")
		if err != nil {
			t.Fatal(err)
		}
		var warnings []string
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := RenderDocx(doc, out, Options{PDFA: true, Warn: func(w string) { warnings = append(warnings, w) }}); err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("pdfa: %s: warnings %q", value, warnings)
		}
		if text := docxOutline(t, out); strings.Contains(strings.ToLower(text), "pdfa") {
			t.Errorf("pdfa: %s: the DOCX shows the key:\n%s", value, text)
		}
	}
}

// A file that is not what gopdf writes is an error, not a file that only
// looks like PDF/A.
func TestMakePDFAChecksTheFile(t *testing.T) {
	for _, junk := range []string{"", "%PDF-1.7\n", "not a pdf at all"} {
		if _, err := makePDFA([]byte(junk), pdfaMeta{}); err == nil {
			t.Errorf("accepted %q", junk)
		}
	}
	plain, _ := pdfBytes(t, sample, Options{})
	withInfo := bytes.Replace(plain, []byte("/Root 1 0 R\n"), []byte("/Root 1 0 R\n/Info 99 0 R\n"), 1)
	if _, err := makePDFA(withInfo, pdfaMeta{}); err == nil {
		t.Error("accepted a trailer with an Info of its own")
	}
}

// With veraPDF on the PATH, the files it validates are PDF/A-2u, with the
// cover page, the classification band, the watermark and the running header
// and footer too.
func TestPDFAValidates(t *testing.T) {
	verapdf, err := exec.LookPath("verapdf")
	if err != nil {
		t.Skip("veraPDF is not installed")
	}
	full := "---\ntitle: Mentési eljárásrend\nsubtitle: Üzemeltetés\nauthor: Tamás\nversion: 1.2\ndate: 2026-09-20\n" +
		"classification: Bizalmas\nwatermark: TERVEZET\ncover: true\nlang: hu\n---\n\n" +
		"# Bevezetés\n\nÁrvíztűrő [link](https://example.com) és [belső](#bevezetes).\n\n" +
		"$E = mc^2$\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfunc main() {}\n```\n"
	set := theme.Default()
	set.PDF.Header.Center, set.PDF.Footer.Center = "{title}", "{page} / {pages}"

	dir := t.TempDir()
	var files []string
	for name, c := range map[string]struct {
		md   string
		opts Options
	}{
		"small":  {sample, Options{PDFA: true, Lang: "hu"}},
		"plain":  {"# Only a heading\n", Options{PDFA: true}},
		"full":   {full, Options{PDFA: true, Lang: "hu"}},
		"themed": {full, Options{PDFA: true, Lang: "hu", Theme: &set}},
	} {
		path := filepath.Join(dir, name+".pdf")
		if err := os.WriteFile(path, mustPDF(t, c.md, c.opts), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	out, err := exec.Command(verapdf, append([]string{"--flavour", "2u", "--format", "text"}, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("veraPDF: %v\n%s", err, out)
	}
	if got := strings.Count(string(out), "PASS "); got != len(files) {
		t.Errorf("%d of %d files conform:\n%s", got, len(files), out)
	}
}
