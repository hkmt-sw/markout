package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// checkDir fails the test if the directory holds anything but the named
// entries, which is how leftover temporary files show up.
func checkDir(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the directory holds %v, want %v", got, want)
	}
}

func TestWriteFileCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	for _, content := range []string{"first", "second, which is longer"} {
		if err := WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Fatalf("the file holds %q, want %q", got, content)
		}
	}
	checkDir(t, dir, "out.txt")
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits beyond read-only")
	}
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the file has mode %o, want 600", got)
	}
}

// A failure while writing must leave the old file as it was and no
// temporary file behind.
func TestFailedFillKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err := Write(path, 0o644, func(w io.Writer) error {
		if _, err := w.Write([]byte("half of the new")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Write() = %v, want the error of the fill function", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Errorf("the file holds %q, want the old content", got)
	}
	checkDir(t, dir, "out.txt")
}

// A failure to put the file in place (here a directory is in the way) is
// reported, and the temporary file is removed.
func TestFailedRenameLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("WriteFile() replaced a directory without an error")
	}
	checkDir(t, dir, "out")
}

func TestMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "out.txt")
	if err := WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("WriteFile() into a missing directory gave no error")
	}
}

func TestSymbolicLinkIsKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("the target holds %q, want the new content", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a file: %v, %v", info, err)
	}
}
