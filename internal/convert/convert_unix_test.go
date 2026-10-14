//go:build unix

package convert

import (
	"errors"
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
