//go:build unix

package convert

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A file that is not a regular one is refused without being read: a device
// or a pipe reports no size, and reading it could take all the memory or
// never end. This is in a file of its own because named pipes are made with
// a call that Windows does not have.
func TestConvertRefusesInputThatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "in.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot make a named pipe: %v", err)
	}

	// A refusal that works returns at once; one that does not would block
	// on the pipe, so the call is given a time to finish in.
	done := make(chan error, 1)
	go func() { done <- ConvertFile(fifo, filepath.Join(dir, "out.pdf"), FormatUnknown) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Errorf("a named pipe: got %v, want ErrNotRegular", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("converting a named pipe did not return")
	}

	if _, err := RemoteImageHosts(fifo, Options{}); !errors.Is(err, ErrNotRegular) {
		t.Errorf("RemoteImageHosts on a named pipe: got %v, want ErrNotRegular", err)
	}
}

// A conversion that cannot write its output reports a permission error that
// errors.Is still recognizes, so that the caller can tell it from a failure of
// the document.
func TestConvertUnwritableOutputKeepsPermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("# Title\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	for _, name := range []string{"out.pdf", "out.docx"} {
		err := ConvertFile(in, filepath.Join(locked, name), FormatUnknown)
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%s: got %v, want an error that is fs.ErrPermission", name, err)
		}
	}
}
