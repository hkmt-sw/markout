package convert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// A directory is not a document, and is not read as one.
func TestConvertRefusesADirectoryAsInput(t *testing.T) {
	dir := t.TempDir()
	if err := ConvertFile(dir, filepath.Join(dir, "out.pdf"), FormatUnknown); !errors.Is(err, ErrNotRegular) {
		t.Errorf("a directory: got %v, want ErrNotRegular", err)
	}
}

func TestConvertRefusesInputOverTheSizeLimit(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "big.md")
	if err := os.WriteFile(in, []byte(strings.Repeat("a", MaxFileSize+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ConvertFile(in, filepath.Join(dir, "out.pdf"), FormatUnknown); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("a file over the limit: got %v, want ErrFileTooLarge", err)
	}

	// Exactly the limit is allowed.
	if err := os.WriteFile(in, []byte(strings.Repeat("a", MaxFileSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDocument(in, flavor.Default()); err != nil {
		t.Errorf("a file of exactly the limit: %v", err)
	}
}
