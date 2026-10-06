package render

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hkmt-sw/markout/internal/parse"
)

// TestPdfHungarianAccents guards against the PDF renderer regressing to
// system fonts: it must render Hungarian text (including ő/ű) on any OS using
// the embedded Liberation fonts. Runs on the CI Linux runner too.
func TestPdfHungarianAccents(t *testing.T) {
	md := []byte("# Árvíztűrő tükörfúrógép\n\nŐrült űrhajó, félős fésű.\n")
	doc, err := parse.Parse(md)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	out := filepath.Join(t.TempDir(), "hu.pdf")
	if err := RenderPdfToFile(doc, out); err != nil {
		t.Fatalf("render PDF (embedded fonts should work without system fonts): %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatal("output is not a PDF")
	}
	if len(data) < 1024 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(data))
	}
}
