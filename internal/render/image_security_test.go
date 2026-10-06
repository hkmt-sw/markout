package render

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Local images must be regular files of a bounded size.
func TestReadLocalImageRefusesUnboundedSources(t *testing.T) {
	dir := t.TempDir()

	ok := filepath.Join(dir, "ok.png")
	if err := os.WriteFile(ok, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := readLocalImage(ok); err != nil || string(data) != "data" {
		t.Fatalf("regular file: %q, %v", data, err)
	}

	if _, err := readLocalImage(dir); err == nil {
		t.Error("a directory was accepted as an image")
	}

	// A sparse file just over the limit: large on paper, tiny on disk.
	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := readLocalImage(big); err == nil {
		t.Error("an oversized file was accepted as an image")
	}

	if runtime.GOOS != "windows" {
		if _, err := readLocalImage("/dev/zero"); err == nil {
			t.Error("a device file was accepted as an image")
		}
	}
}
