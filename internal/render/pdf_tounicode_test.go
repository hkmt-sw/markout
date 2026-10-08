package render

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// checkPDFObjects fails if the file's table of objects does not point at
// its objects: what happens when something in the file is changed without
// the table being brought up to date.
func checkPDFObjects(t *testing.T, pdf []byte) {
	t.Helper()
	at := bytes.LastIndex(pdf, []byte("startxref"))
	if at < 0 {
		t.Fatal("no startxref")
	}
	fields := strings.Fields(string(pdf[at:]))
	start, _ := strconv.Atoi(fields[1])
	if start >= len(pdf) || !bytes.HasPrefix(pdf[start:], []byte("xref\n0 ")) {
		t.Fatalf("startxref says %d, where there is no table", start)
	}
	lines := bytes.SplitN(pdf[start:], []byte("\n"), 3)
	count, _ := strconv.Atoi(strings.Fields(string(lines[1]))[1])
	entries := lines[2]
	wrong := 0
	for i := 1; i < count; i++ {
		offset, err := strconv.Atoi(string(entries[20*i : 20*i+10]))
		if err != nil || offset >= len(pdf) || !bytes.HasPrefix(pdf[offset:], []byte(strconv.Itoa(i)+" 0 obj")) {
			if wrong++; wrong <= 3 {
				t.Errorf("object %d is not at the offset %d the table has", i, offset)
			}
		}
	}
	// Every stream is as long as it says
	for _, m := range regexp.MustCompile(`<<\n/Length (\d+)\n>>\nstream\n`).FindAllSubmatchIndex(pdf, -1) {
		length, _ := strconv.Atoi(string(pdf[m[2]:m[3]]))
		if end := m[1] + length; end > len(pdf) || !bytes.HasPrefix(bytes.TrimLeft(pdf[end:], "\n"), []byte("endstream")) {
			t.Errorf("the stream at %d is not %d bytes long", m[1], length)
		}
	}
}

// toUnicodeOf returns the characters the PDF says its glyphs stand for.
func toUnicodeOf(pdf []byte) string {
	var chars []string
	for _, m := range regexp.MustCompile(`<[0-9A-F]{4}><[0-9A-F]{4}><([0-9A-F]+)>`).FindAllSubmatch(pdf, -1) {
		chars = append(chars, string(m[1]))
	}
	return " " + strings.Join(chars, " ") + " "
}

// The text of a formula can be copied and searched: its italic letters are
// the letters they stand for, and a character beyond the first 65536 is
// written as the two halves it has in UTF-16.
func TestFormulaTextCanBeCopied(t *testing.T) {
	pdf, _ := renderPDF(t, "Energy $E = mc^2$, angle $\\alpha$, sets $\\mathbb{R}$ and $\\mathbb{A}$, bold $\\mathbf{v}$.\n")
	checkPDFObjects(t, pdf)
	chars := toUnicodeOf(pdf)
	for what, want := range map[string]string{
		"E": " 0045 ", "m": " 006D ", "c": " 0063 ", "alpha": " 03B1 ", "v": " 0076 ",
		"the real numbers":           " 211D ",
		"double-struck A, as a pair": " D835DD38 ",
	} {
		if !strings.Contains(chars, want) {
			t.Errorf("%s: no glyph stands for%s; the glyphs stand for:%s", what, want, chars)
		}
	}
	if m := regexp.MustCompile(` [0-9A-F]{5} `).FindString(chars); m != "" {
		t.Errorf("a character is written as the number%s, which readers take for another character", m)
	}
}

// A document without formulas is left as gopdf wrote it.
func TestFixToUnicodeLeavesOtherFilesAlone(t *testing.T) {
	pdf, _ := renderPDF(t, "# Title\n\nPlain text, árvíztűrő.\n")
	checkPDFObjects(t, pdf)
	if !bytes.Equal(fixToUnicode(pdf), pdf) {
		t.Error("a file with nothing to fix was changed")
	}
	for _, other := range [][]byte{nil, []byte("not a pdf"), []byte("%PDF-1.7\n<<\n/Length 5\n>>\nstream\nhello")} {
		if !bytes.Equal(fixToUnicode(other), other) {
			t.Errorf("%q was changed", other)
		}
	}
}
