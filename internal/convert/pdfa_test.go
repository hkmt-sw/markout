package convert

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// PDFA reaches the PDF, and a DOCX is made without a word about it.
func TestPDFAOption(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("---\ntitle: T\n---\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	convert := func(out string, opts Options) []byte {
		t.Helper()
		var warnings []string
		opts.Warn = func(w string) { warnings = append(warnings, w) }
		if err := ConvertFileWith(in, filepath.Join(dir, out), FormatUnknown, opts); err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("%s: warnings %q", out, warnings)
		}
		data, _ := os.ReadFile(filepath.Join(dir, out))
		return data
	}

	marker := []byte("/OutputIntents")
	if bytes.Contains(convert("plain.pdf", Options{}), marker) {
		t.Error("PDF/A without being asked")
	}
	if !bytes.Contains(convert("pdfa.pdf", Options{PDFA: true}), marker) {
		t.Error("PDF/A was asked for and is not there")
	}
	if !bytes.HasPrefix(convert("pdfa.docx", Options{PDFA: true}), []byte("PK")) {
		t.Error("the DOCX is not a DOCX")
	}
}
