package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The o key starts the opener from a command, not inside Update, so the
// screen does not wait for it; a failure is shown in the status line.
func TestOpenOutputDoesNotBlockAndShowsFailure(t *testing.T) {
	m := newAt(t.TempDir())
	m.lastOutput = filepath.Join(m.cwd, "out.pdf")
	release := make(chan struct{})
	var opened atomic.Int32
	m.open = func(path string) error {
		opened.Add(1)
		<-release
		return nil
	}

	next, cmd := m.Update(key("o"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("o with an output returned no command")
	}
	if opened.Load() != 0 {
		t.Fatal("the opener ran inside Update")
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	close(release)
	select {
	case msg := <-done:
		next, _ = m.Update(msg)
		if got := next.(Model); got.statusKind == statusError {
			t.Errorf("an opener that worked set an error: %q", got.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not finish")
	}

	m.open = func(string) error { return errors.New("no viewer") }
	_, cmd = m.Update(key("o"))
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.statusKind != statusError || !strings.Contains(m.status, "out.pdf") || !strings.Contains(m.status, "no viewer") {
		t.Errorf("status = %q (%v), want the failure to open out.pdf", m.status, m.statusKind)
	}

	// Without an output, or without an opener, the key does nothing
	m.lastOutput = ""
	if _, cmd = m.Update(key("o")); cmd != nil {
		t.Error("o without an output returned a command")
	}
	m.lastOutput = "out.pdf"
	m.open = nil
	if _, cmd = m.Update(key("o")); cmd != nil {
		t.Error("o without an opener returned a command")
	}
}

// openFile returns once the program has started, and says when it cannot.
func TestOpenFileStartsWithoutWaiting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a script on the path")
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n/bin/sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	started := time.Now()
	if err := openFile("x.pdf"); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Error("openFile waited for the program")
	}

	t.Setenv("PATH", t.TempDir())
	if err := openFile("x.pdf"); err == nil {
		t.Error("a missing program was not reported")
	}
}
