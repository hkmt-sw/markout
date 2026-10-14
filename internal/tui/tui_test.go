package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/settings"
	"github.com/hkmt-sw/markout/internal/theme"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// cursorOn moves the cursor onto the named entry; fails if not found.
func cursorOn(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, e := range m.entries {
		if e.name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("entry %q not found in %v", name, m.entries)
}

func TestQuitOnQ(t *testing.T) {
	m := newAt(t.TempDir())
	updated, cmd := m.Update(key("q"))
	if !updated.(Model).quit {
		t.Fatal("expected quit flag after 'q'")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestLoadEntriesSortsDirsThenMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)

	// ".." first (temp dir has a parent), then the directory, then files.
	if m.entries[0].name != ".." || !m.entries[0].isUp {
		t.Fatalf("expected '..' first, got %+v", m.entries[0])
	}
	if m.entries[1].name != "sub" || !m.entries[1].isDir {
		t.Fatalf("expected 'sub' dir second, got %+v", m.entries[1])
	}
	// a.md is selectable, b.txt is not.
	var mdSel, txtSel bool
	var sawTxt bool
	for _, e := range m.entries {
		if e.name == "a.md" {
			mdSel = e.selectable
		}
		if e.name == "b.txt" {
			sawTxt = true
			txtSel = e.selectable
		}
	}
	if !mdSel {
		t.Fatal("a.md should be selectable")
	}
	if !sawTxt || txtSel {
		t.Fatal("b.txt should be present but not selectable")
	}
}

func TestFormatToggleChangesDerivedOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newAt(dir)
	cursorOn(t, &m, "doc.md")

	// PDF is the default format.
	if got := filepath.Base(m.outputPath()); got != "doc.pdf" {
		t.Fatalf("expected doc.pdf, got %q", got)
	}
	next, _ := m.Update(key("f3"))
	nm := next.(Model)
	if nm.format != formatDOCX {
		t.Fatalf("expected DOCX after f3, got %v", nm.format)
	}
	if got := filepath.Base(nm.outputPath()); got != "doc.docx" {
		t.Fatalf("expected doc.docx, got %q", got)
	}
}

func TestEnterOnMarkdownConverts(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "sample.md")
	if err := os.WriteFile(in, []byte("# Hello\n\nWorld.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)
	cursorOn(t, &m, "sample.md")
	// DOCX keeps this flow test fast; PDF rendering is covered in the render
	// package. PDF-as-default is covered by the output-path test above.
	m.format = formatDOCX

	next, cmd := m.Update(key("enter"))
	nm := next.(Model)
	if !nm.converting {
		t.Fatal("expected converting state after Enter on a .md file")
	}
	if cmd == nil {
		t.Fatal("expected a conversion command")
	}

	done, ok := findConvertDone(cmd())
	if !ok {
		t.Fatal("expected convertDoneMsg from command batch")
	}
	if done.err != nil {
		t.Fatalf("conversion failed: %v", done.err)
	}
	if _, err := os.Stat(done.output); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if !strings.HasSuffix(done.output, ".docx") {
		t.Fatalf("expected .docx output, got %q", done.output)
	}
}

func TestEnterOnDirectoryNavigates(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)
	cursorOn(t, &m, "sub")
	next, _ := m.Update(key("enter"))
	nm := next.(Model)
	if filepath.Base(nm.cwd) != "sub" {
		t.Fatalf("expected to navigate into 'sub', cwd=%q", nm.cwd)
	}
	// inner.md should now be listed.
	found := false
	for _, e := range nm.entries {
		if e.name == "inner.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected inner.md after navigating into sub")
	}
}

func TestSearchJumpsToMatch(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"alpha.md", "beta.md", "gamma.md", "report.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newAt(dir)

	// Start search with Ctrl+S.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if !m.searching {
		t.Fatal("expected search mode after Ctrl+S")
	}

	// Type "gam" → cursor should land on gamma.md.
	for _, r := range "gam" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	if e, _ := m.current(); e.name != "gamma.md" {
		t.Fatalf("expected cursor on gamma.md, got %q", e.name)
	}
	if m.searchMiss {
		t.Fatal("did not expect a miss for 'gam'")
	}

	// A non-matching query flags a miss.
	next, _ = m.Update(key("z"))
	m = next.(Model)
	if !m.searchMiss {
		t.Fatal("expected miss for 'gamz'")
	}

	// Esc cancels search.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.searching || m.searchQuery != "" {
		t.Fatal("expected search cleared after Esc")
	}
}

func TestSearchNextWraps(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a_doc.md", "b_doc.md", "c_note.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newAt(dir)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	for _, r := range "doc" {
		next, _ = m.Update(key(string(r)))
		m = next.(Model)
	}
	first, _ := m.current()
	if first.name != "a_doc.md" {
		t.Fatalf("expected first match a_doc.md, got %q", first.name)
	}
	// Ctrl+S → next match.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if e, _ := m.current(); e.name != "b_doc.md" {
		t.Fatalf("expected next match b_doc.md, got %q", e.name)
	}
}

func TestOverwritePromptThenCancel(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	out := filepath.Join(dir, "doc.pdf") // PDF is the default format
	if err := os.WriteFile(in, []byte("# Hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("OLD"), 0o644); err != nil { // existing output
		t.Fatal(err)
	}

	m := newAt(dir)
	cursorOn(t, &m, "doc.md")

	// Enter must NOT convert immediately; it asks to confirm.
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if !m.confirmOverwrite {
		t.Fatal("expected overwrite confirmation prompt")
	}
	if m.converting || cmd != nil {
		t.Fatal("must not start converting before confirmation")
	}

	// 'n' cancels and leaves the existing file untouched.
	next, _ = m.Update(key("n"))
	m = next.(Model)
	if m.confirmOverwrite {
		t.Fatal("expected prompt dismissed after 'n'")
	}
	if b, _ := os.ReadFile(out); string(b) != "OLD" {
		t.Fatalf("file should be untouched after cancel, got %q", b)
	}
}

func TestOverwritePromptThenConfirm(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	out := filepath.Join(dir, "doc.docx") // DOCX keeps the test fast
	if err := os.WriteFile(in, []byte("# Hi\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)
	cursorOn(t, &m, "doc.md")
	m.format = formatDOCX
	next, _ := m.Update(key("enter"))
	m = next.(Model)

	// 'y' proceeds with the conversion.
	next, cmd := m.Update(key("y"))
	m = next.(Model)
	if !m.converting || cmd == nil {
		t.Fatal("expected conversion to start after 'y'")
	}
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("expected successful conversion, ok=%v err=%v", ok, done.err)
	}
	// The file was overwritten with a real document (no longer "OLD").
	if b, _ := os.ReadFile(out); string(b) == "OLD" {
		t.Fatal("expected output file to be overwritten")
	}
}

func TestOverwriteButtonsNavigable(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	out := filepath.Join(dir, "doc.docx") // DOCX keeps the test fast
	if err := os.WriteFile(in, []byte("# Hi\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)
	cursorOn(t, &m, "doc.md")
	m.format = formatDOCX
	next, _ := m.Update(key("enter"))
	m = next.(Model)

	// Default focus is Cancel; Enter must NOT overwrite.
	if m.confirmChoice != 1 {
		t.Fatalf("expected default focus on Cancel, got %d", m.confirmChoice)
	}
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if m.converting || cmd != nil {
		t.Fatal("Enter on Cancel must not convert")
	}
	if string(must(os.ReadFile(out))) != "OLD" {
		t.Fatal("file should be untouched")
	}

	// Re-open, move focus to Overwrite, then Enter converts.
	next, _ = m.Update(key("enter")) // cursor still on doc.md → prompt again
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.confirmChoice != 0 {
		t.Fatalf("expected focus on Overwrite after →, got %d", m.confirmChoice)
	}
	next, cmd = m.Update(key("enter"))
	m = next.(Model)
	if !m.converting || cmd == nil {
		t.Fatal("Enter on Overwrite must start conversion")
	}
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("expected successful conversion, ok=%v err=%v", ok, done.err)
	}
}

func must(b []byte, _ error) []byte { return b }

func findConvertDone(msg tea.Msg) (convertDoneMsg, bool) {
	switch v := msg.(type) {
	case convertDoneMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if d, ok := findConvertDone(c()); ok {
				return d, true
			}
		}
	}
	return convertDoneMsg{}, false
}

func TestFlavorPickerSelectsAndSaves(t *testing.T) {
	m := newAt(t.TempDir())
	var saved []settings.Settings
	m.saveSettings = func(s settings.Settings) error {
		saved = append(saved, s)
		return nil
	}
	if m.flavor.ID != flavor.DefaultID {
		t.Fatalf("expected the default flavor at start, got %q", m.flavor.ID)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(Model)
	if !m.pickFlavor {
		t.Fatal("expected F2 to open the flavor dialog")
	}
	if !strings.Contains(m.View(), "Settings") || !strings.Contains(m.View(), "Tab") {
		t.Fatal("expected the dialog to be drawn")
	}

	// Move down twice and confirm: the third flavor in the list is chosen.
	for range 2 {
		next, _ = m.Update(key("down"))
		m = next.(Model)
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)

	want := flavor.All()[2]
	if m.pickFlavor {
		t.Fatal("expected Enter to close the dialog")
	}
	if m.flavor.ID != want.ID {
		t.Fatalf("flavor = %q, want %q", m.flavor.ID, want.ID)
	}
	if len(saved) != 1 || saved[0].Flavor != want.ID {
		t.Fatalf("saved settings = %+v, want one save of %q", saved, want.ID)
	}
	if !strings.Contains(m.View(), want.Name) {
		t.Fatal("expected the Convert box to show the chosen flavor")
	}
}

func TestFlavorPickerEscKeepsFlavor(t *testing.T) {
	m := newAt(t.TempDir())
	m.saveSettings = func(settings.Settings) error {
		t.Fatal("canceling must not save")
		return nil
	}

	next, _ := m.Update(key("s"))
	m = next.(Model)
	next, _ = m.Update(key("down"))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.pickFlavor || m.flavor.ID != flavor.DefaultID {
		t.Fatalf("after Esc: open=%v flavor=%q", m.pickFlavor, m.flavor.ID)
	}
}

// The chosen flavor must reach the converter: GitLab's inline diff markers
// survive as text under CommonMark and are consumed under GitLab.
func TestConversionUsesSelectedFlavor(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(src, []byte("# Title\n\n{+ added +}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for id, wantMarkers := range map[string]bool{"commonmark": true, "gitlab": false} {
		m := newAt(dir)
		m.flavor, _ = flavor.ByID(id)
		m.format = formatDOCX
		cursorOn(t, &m, "doc.md")
		out := filepath.Join(dir, id+".docx")
		m.outputOverride = out

		_, cmd := m.Update(key("enter"))
		if cmd == nil {
			t.Fatalf("%s: expected a conversion command", id)
		}
		done, ok := findConvertDone(cmd())
		if !ok || done.err != nil {
			t.Fatalf("%s: conversion did not succeed: %+v", id, done)
		}

		body := docxBody(t, out)
		if !strings.Contains(body, "added") {
			t.Fatalf("%s: text missing from the output", id)
		}
		if got := strings.Contains(body, "{+"); got != wantMarkers {
			t.Errorf("%s: diff markers present = %v, want %v", id, got, wantMarkers)
		}
	}
}

// docxBody returns the main document XML of a DOCX file, followed by its styles:
// how a heading looks is in its style.
func docxBody(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	parts := map[string]string{}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" && f.Name != "word/styles.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(data)
	}
	if parts["word/document.xml"] == "" {
		t.Fatal("word/document.xml not found")
	}
	return parts["word/document.xml"] + parts["word/styles.xml"]
}

// A file name must not be able to write terminal escape sequences.
func TestFileNamesAreSanitizedForDisplay(t *testing.T) {
	if got := printable("a\x1b[2Jb\tc\u200bd.md"); got != "a?[2Jb?c?d.md" {
		t.Fatalf("printable = %q", got)
	}
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid in Windows file names")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "evil\x1b[2J.md"), []byte("x"), 0o644); err != nil {
		t.Skip("file system rejects control characters in names")
	}
	m := newAt(dir)
	cursorOn(t, &m, "evil\x1b[2J.md")
	if view := m.View(); strings.Contains(view, "\x1b[2J") {
		t.Fatal("the escape sequence from the file name reached the screen")
	}
}

// remoteDoc writes a document that references an image on a counting test
// server and returns the model with the cursor on it.
func remoteDoc(t *testing.T) (Model, *atomic.Int32, string) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	hits := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Doc\n\n![pic]("+srv.URL+"/p.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newAt(dir)
	m.format = formatDOCX
	cursorOn(t, &m, "doc.md")
	return m, hits, strings.TrimPrefix(srv.URL, "http://")
}

func TestRemoteImagesDialogSkipsByDefault(t *testing.T) {
	m, hits, host := remoteDoc(t)

	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if !m.confirmRemote || cmd != nil {
		t.Fatal("expected the remote images dialog instead of a conversion")
	}
	view := m.View()
	for _, want := range []string{host, "1 image", "local network", "at your own risk", "Load images", "Skip images"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog does not show %q", want)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("showing the dialog contacted the server")
	}

	// Enter on the default button converts without downloading.
	next, cmd = m.Update(key("enter"))
	m = next.(Model)
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("conversion did not succeed: %+v", done)
	}
	if m.confirmRemote {
		t.Fatal("dialog still open after answering")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("skipping still made %d requests", n)
	}
}

func TestRemoteImagesDialogLoadAndCancel(t *testing.T) {
	m, hits, _ := remoteDoc(t)

	// Cancel: nothing is converted or fetched.
	next, _ := m.Update(key("enter"))
	next, cmd := next.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || next.(Model).confirmRemote || next.(Model).converting {
		t.Fatal("Esc should close the dialog without converting")
	}
	if _, err := os.Stat(filepath.Join(m.cwd, "doc.docx")); err == nil {
		t.Fatal("canceling still wrote the output")
	}

	// Load: the image is downloaded.
	next, _ = m.Update(key("enter"))
	_, cmd = next.(Model).Update(key("l"))
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("conversion did not succeed: %+v", done)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("loading made %d requests, want 1", n)
	}
}

// updateModel is a model set up like a release build with a stubbed check.
func updateModel(t *testing.T, version string) (Model, *int, *[]settings.Settings) {
	t.Helper()
	m := newAt(t.TempDir())
	m.version = version
	calls := 0
	m.latest = func(context.Context) (string, error) {
		calls++
		return "v9.0.0", nil
	}
	var saved []settings.Settings
	m.saveSettings = func(s settings.Settings) error {
		saved = append(saved, s)
		return nil
	}
	return m, &calls, &saved
}

func TestUpdateCheckRunsOnlyWhenDue(t *testing.T) {
	m, _, _ := updateModel(t, "v1.0.0")
	if m.Init() == nil {
		t.Fatal("a release build with no earlier check should check")
	}

	m.settings.UpdateCheckedAt = time.Now().Add(-time.Hour)
	if m.Init() != nil {
		t.Error("checked again within a day")
	}
	m.settings.UpdateCheckedAt = time.Now().Add(-25 * time.Hour)
	if m.Init() == nil {
		t.Error("did not check after a day")
	}

	m.settings.NoUpdateCheck = true
	if m.Init() != nil {
		t.Error("checked although the check is turned off")
	}

	for _, dev := range []string{"dev", "v1.0.0-3-gabc123-dirty", ""} {
		m, _, _ := updateModel(t, dev)
		if m.Init() != nil {
			t.Errorf("development build %q checked for updates", dev)
		}
	}

	// The model tests use everywhere else must never reach the network.
	if newAt(t.TempDir()).Init() != nil {
		t.Error("a model without a version checked for updates")
	}
}

func TestUpdateNoticeAndToggle(t *testing.T) {
	m, calls, saved := updateModel(t, "v1.0.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = next.(Model)

	next, _ = m.Update(m.Init()())
	m = next.(Model)
	if *calls != 1 {
		t.Fatalf("check ran %d times, want 1", *calls)
	}
	if !strings.Contains(m.View(), "v9.0.0 is available") {
		t.Fatal("the newer release is not announced")
	}
	if len(*saved) != 1 || (*saved)[0].LatestVersion != "v9.0.0" || (*saved)[0].UpdateCheckedAt.IsZero() {
		t.Fatalf("check result not saved: %+v", *saved)
	}
	if m.Init() != nil {
		t.Error("would check again right after checking")
	}

	// Too narrow for the notice: the border must stay intact.
	narrow, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	top := strings.Split(narrow.(Model).View(), "\n")[0]
	if strings.Contains(top, "available") || lipgloss.Width(top) != 40 {
		t.Errorf("narrow top border = %q (width %d)", top, lipgloss.Width(top))
	}
	if top := strings.Split(m.View(), "\n")[0]; lipgloss.Width(top) != 100 {
		t.Errorf("top border with notice is %d wide, want 100", lipgloss.Width(top))
	}

	// u in the settings dialog turns the check off, saves, and hides the notice.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(key("u"))
	m = next.(Model)
	if !m.pickFlavor || !m.settings.NoUpdateCheck {
		t.Fatal("u should toggle the update check and keep the dialog open")
	}
	if !strings.Contains(m.View(), "Updates: off") || m.updateNotice() != "" {
		t.Error("dialog or notice does not reflect the check being off")
	}
	if last := (*saved)[len(*saved)-1]; !last.NoUpdateCheck {
		t.Errorf("toggle not saved: %+v", last)
	}

	// Turning it back on shows the remembered release again without a request.
	next, _ = m.Update(key("u"))
	m = next.(Model)
	if !strings.Contains(m.updateNotice(), "v9.0.0 is available") || *calls != 1 {
		t.Errorf("re-enabling: notice=%q, requests=%d", m.updateNotice(), *calls)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(next.(Model).View(), "v9.0.0 is available") {
		t.Error("the notice is not drawn after closing the dialog")
	}
}

func TestUpdateCheckFailureIsSilent(t *testing.T) {
	m, _, saved := updateModel(t, "v1.0.0")
	m.latest = func(context.Context) (string, error) { return "", errors.New("offline") }
	next, _ := m.Update(m.Init()())
	m = next.(Model)
	if m.updateTo != "" || len(*saved) != 0 || m.statusKind == statusError {
		t.Fatalf("a failed check should change nothing: updateTo=%q saved=%d", m.updateTo, len(*saved))
	}
	if m.Init() == nil {
		t.Error("a failed check should be retried on the next start")
	}

	// Same version or older: nothing to announce.
	m, _, _ = updateModel(t, "v9.0.0")
	next, _ = m.Update(m.Init()())
	if next.(Model).updateTo != "" {
		t.Error("announced the version that is already running")
	}
}

// themeModel has a themes directory with one good and one broken theme.
func themeModel(t *testing.T) (Model, *[]settings.Settings) {
	t.Helper()
	themes := t.TempDir()
	if err := os.WriteFile(filepath.Join(themes, "crimson.toml"), []byte("description = \"Red headings\"\n\n[heading]\ncolor = \"#AA0000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "broken.toml"), []byte("[text]\nsizee = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newAt(dir)
	m.themes = theme.Loader{UserDir: themes}
	m.settings.Flavor = flavor.DefaultID
	m.format = formatDOCX
	var saved []settings.Settings
	m.saveSettings = func(s settings.Settings) error {
		saved = append(saved, s)
		return nil
	}
	cursorOn(t, &m, "doc.md")
	return m, &saved
}

func TestThemeTabSelectsSavesAndStylesTheOutput(t *testing.T) {
	m, saved := themeModel(t)
	if !strings.Contains(m.View(), "Theme:   default") {
		t.Fatal("the Convert box should show the default theme")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"default", "crimson", "broken", "Your own themes"} {
		if !strings.Contains(view, want) {
			t.Errorf("the Theme tab does not show %q", want)
		}
	}

	// The list is default, broken, crimson: move to crimson and select it.
	for m.themeList[m.themeCursor].Name != "crimson" {
		next, _ = m.Update(key("down"))
		m = next.(Model)
	}
	if !strings.Contains(m.View(), "Red headings") {
		t.Error("the description of the highlighted theme is not shown")
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.pickFlavor || m.themeRef != "crimson" {
		t.Fatalf("after Enter: open=%v theme=%q", m.pickFlavor, m.themeRef)
	}
	if len(*saved) != 1 || (*saved)[0].Theme != "crimson" || (*saved)[0].Flavor != flavor.DefaultID {
		t.Fatalf("saved %+v, want the theme without losing the flavor", *saved)
	}
	if !strings.Contains(m.View(), "Theme:   crimson") {
		t.Error("the Convert box does not show the chosen theme")
	}

	// The conversion uses it: the heading is red in the document.
	out := filepath.Join(m.cwd, "doc.docx")
	_, cmd := m.Update(key("enter"))
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("conversion did not succeed: %+v", done)
	}
	if !strings.Contains(docxBody(t, out), "AA0000") {
		t.Error("the heading color from the theme is not in the output")
	}
}

func TestBrokenThemeIsShownButNotSelected(t *testing.T) {
	m, saved := themeModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	for m.themeList[m.themeCursor].Name != "broken" {
		next, _ = m.Update(key("down"))
		m = next.(Model)
	}
	if !strings.Contains(m.View(), "cannot be used") {
		t.Error("the broken theme's problem is not shown in the list")
	}
	next, _ = m.Update(key("enter"))
	m = next.(Model)
	if m.themeRef != "" || len(*saved) != 0 || m.statusKind != statusError || !strings.Contains(m.status, "sizee") {
		t.Fatalf("a broken theme was selected: theme=%q saved=%d status=%q", m.themeRef, len(*saved), m.status)
	}
}

// A theme that stops loading (the file was edited or removed) is reported at
// conversion time instead of silently falling back.
func TestMissingThemeFailsTheConversion(t *testing.T) {
	m, _ := themeModel(t)
	m.themeRef = "gone"
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil || m.converting || m.statusKind != statusError || !strings.Contains(m.status, "gone") {
		t.Fatalf("converting=%v status=%q", m.converting, m.status)
	}
}

// A conversion that left something out says so next to the saved file.
func TestConversionWarningIsShown(t *testing.T) {
	m := newAt(t.TempDir())
	next, _ := m.Update(convertDoneMsg{
		output:   filepath.Join("some", "dir", "out.pdf"),
		warnings: []string{"2 characters are not in the PDF fonts", "right-to-left text"},
	})
	got := next.(Model)
	if got.statusKind != statusWarning {
		t.Errorf("statusKind = %v, want statusWarning", got.statusKind)
	}
	for _, want := range []string{"saved → out.pdf", "2 characters are not in the PDF fonts", "(+1 more)"} {
		if !strings.Contains(got.status, want) {
			t.Errorf("status %q does not contain %q", got.status, want)
		}
	}
}

func TestLanguageTabShowsTheLanguages(t *testing.T) {
	m, _ := themeModel(t)
	if !strings.Contains(m.View(), "Language: none") {
		t.Fatal("the Convert box should say that there is no language")
	}
	m = onLanguageTab(t, m)
	view := m.View()
	for _, want := range []string{"None", "Hungarian (hu)", "English (United Kingdom) (en-GB)"} {
		if !strings.Contains(view, want) {
			t.Errorf("the Language tab does not show %q", want)
		}
	}
	// Round to the first tab again, and back
	if next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}); next.(Model).settingsTab != tabFlavor {
		t.Error("Tab on the last tab does not go to the first")
	}
	if next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab}); next.(Model).settingsTab != tabTheme {
		t.Error("Shift+Tab does not go to the tab before")
	}
}

// onLanguageTab opens the settings and goes to their third tab.
func onLanguageTab(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	for range 2 {
		next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	m = next.(Model)
	if m.settingsTab != tabLanguage {
		t.Fatalf("tab %d after two Tabs, want the Language tab", m.settingsTab)
	}
	return m
}

func TestLanguageTabSelectsSavesAndNamesTheLanguage(t *testing.T) {
	m, saved := themeModel(t)
	m = onLanguageTab(t, m)

	next, _ := m.Update(key("down"))
	next, _ = next.(Model).Update(key("enter"))
	m = next.(Model)
	if m.pickFlavor || m.lang != "hu" {
		t.Fatalf("after Enter: open=%v language=%q", m.pickFlavor, m.lang)
	}
	if len(*saved) != 1 || (*saved)[0].Lang != "hu" || (*saved)[0].Flavor != flavor.DefaultID {
		t.Fatalf("saved %+v, want the language without losing the flavor", *saved)
	}
	if !strings.Contains(m.View(), "Language: Hungarian") {
		t.Error("the Convert box does not show the chosen language")
	}

	// The conversion uses it, and the dialog opens on it the next time
	out := filepath.Join(m.cwd, "doc.docx")
	_, cmd := m.Update(key("enter"))
	done, ok := findConvertDone(cmd())
	if !ok || done.err != nil {
		t.Fatalf("conversion did not succeed: %+v", done)
	}
	if !strings.Contains(docxStyles(t, out), `<w:lang w:val="hu-HU"/>`) {
		t.Error("the language is not in the output")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	if next.(Model).langCursor != 1 {
		t.Error("the dialog does not open on the language in use")
	}

	// None takes it away again
	m = next.(Model)
	m.settingsTab = tabLanguage
	next, _ = m.Update(key("up"))
	next, _ = next.(Model).Update(key("enter"))
	if m = next.(Model); m.lang != "" || (*saved)[len(*saved)-1].Lang != "" {
		t.Errorf("after None: language %q, saved %q", m.lang, (*saved)[len(*saved)-1].Lang)
	}
}

// docxStyles is word/styles.xml of a DOCX file.
func docxStyles(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != "word/styles.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return ""
}

// a in the settings dialog turns PDF/A on and off at once, and saves it; the
// Convert box says so while the format is PDF.
func TestPDFAToggle(t *testing.T) {
	m, _, saved := updateModel(t, "v1.0.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(Model)
	if !strings.Contains(m.View(), "PDF/A: off") {
		t.Fatal("the dialog does not say PDF/A is off")
	}

	next, _ = m.Update(key("a"))
	m = next.(Model)
	if !m.pickFlavor || !m.settings.PDFA {
		t.Fatal("a should turn PDF/A on and keep the dialog open")
	}
	if !strings.Contains(m.View(), "PDF/A: on") {
		t.Error("the dialog does not say PDF/A is on")
	}
	if last := (*saved)[len(*saved)-1]; !last.PDFA {
		t.Errorf("not saved: %+v", last)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if !strings.Contains(m.formatLine(), "PDF/A") {
		t.Error("the Convert box does not say PDF/A")
	}
	m.format = formatDOCX
	if strings.Contains(m.formatLine(), "PDF/A") {
		t.Error("the Convert box says PDF/A for a DOCX")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(key("a"))
	if m = next.(Model); m.settings.PDFA || (*saved)[len(*saved)-1].PDFA {
		t.Error("a did not turn PDF/A off again")
	}
}

// A second conversion would write the same output file as the one running,
// whichever way it is started.
func TestNoSecondConversionWhileConverting(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(in, []byte("# Hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newAt(dir)
	m.format = formatDOCX
	cursorOn(t, &m, "doc.md")

	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if !m.converting || cmd == nil {
		t.Fatal("the first Enter did not start a conversion")
	}
	status := m.status

	next, cmd = m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		t.Error("Enter started a second conversion")
	}
	if !m.converting || m.status != status {
		t.Errorf("the running conversion was disturbed: converting=%v, status=%q", m.converting, m.status)
	}

	// With the output in place, Enter would open the overwrite dialog; that
	// must not happen while converting either
	out := filepath.Join(dir, "doc.docx")
	if err := os.WriteFile(out, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(key("enter"))
	if next.(Model).confirmOverwrite {
		t.Error("the overwrite dialog opened during a conversion")
	}

	// The answers of the dialogs are no way around it
	overwrite := m
	overwrite.confirmOverwrite, overwrite.pendingOut = true, out
	remote := m
	remote.confirmRemote, remote.pendingIn, remote.pendingOut = true, in, out
	for _, tc := range []struct {
		dialog Model
		key    string
	}{{overwrite, "y"}, {remote, "l"}, {remote, "s"}} {
		next, cmd := tc.dialog.Update(key(tc.key))
		if cmd != nil {
			t.Errorf("%q in a dialog started a second conversion", tc.key)
		}
		if got := next.(Model); got.confirmOverwrite || got.confirmRemote {
			t.Errorf("%q left a dialog open", tc.key)
		}
	}
}

// A settings file that could not be loaded must survive the automatic save of
// the update check; the user's first change moves it aside.
func TestBrokenSettingsFileIsNotOverwritten(t *testing.T) {
	m, _, saved := updateModel(t, "v1.0.0")
	m.settingsErr = errors.New("cannot parse the settings file")
	asides := 0
	m.setAside = func() (string, error) {
		asides++
		return "/conf/config.json.bak", nil
	}

	// The update check does not save, but its result is announced
	next, _ := m.Update(m.Init()())
	m = next.(Model)
	if len(*saved) != 0 || asides != 0 {
		t.Fatalf("the update check wrote the settings: saved=%d, set aside=%d", len(*saved), asides)
	}
	if m.updateTo != "v9.0.0" {
		t.Errorf("the newer release is not announced: %q", m.updateTo)
	}

	// An explicit change moves the file aside once, says so, and saves
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(key("a"))
	m = next.(Model)
	if asides != 1 || len(*saved) != 1 || !(*saved)[0].PDFA {
		t.Fatalf("after the change: set aside=%d, saved=%+v", asides, *saved)
	}
	if !strings.Contains(m.status, "config.json.bak") || m.statusKind != statusWarning {
		t.Errorf("status = %q (%d), want it to name the backup", m.status, m.statusKind)
	}
	m.Update(key("a"))
	if asides != 1 || len(*saved) != 2 {
		t.Errorf("a second change: set aside=%d, saved=%d", asides, len(*saved))
	}
}

// If the broken file cannot be kept, nothing is saved over it.
func TestBrokenSettingsFileIsKeptOrNothingIsSaved(t *testing.T) {
	m, _, saved := updateModel(t, "v1.0.0")
	m.settingsErr = errors.New("cannot parse the settings file")
	m.setAside = func() (string, error) { return "", errors.New("read-only") }

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next, _ = next.(Model).Update(key("a"))
	m = next.(Model)
	if len(*saved) != 0 {
		t.Fatal("the settings were saved over a file that could not be kept")
	}
	if !strings.Contains(m.status, "not saved") || m.statusKind != statusError || m.settingsErr == nil {
		t.Errorf("status = %q (%d), settingsErr = %v", m.status, m.statusKind, m.settingsErr)
	}
}

func TestSettingsProblemIsShownAtStart(t *testing.T) {
	m := New(Config{SettingsErr: errors.New("cannot parse the settings file x")})
	if !strings.Contains(m.status, "cannot parse the settings file x") || m.statusKind != statusWarning {
		t.Errorf("status = %q (%d)", m.status, m.statusKind)
	}
	if m.settingsErr == nil {
		t.Error("the model does not remember that the settings file is broken")
	}
}

// A save that fails after the update check is told, not dropped.
func TestUpdateCheckSaveFailureIsShown(t *testing.T) {
	m, _, _ := updateModel(t, "v1.0.0")
	m.saveSettings = func(settings.Settings) error { return errors.New("disk full") }
	next, _ := m.Update(m.Init()())
	m = next.(Model)
	if !strings.Contains(m.status, "disk full") {
		t.Errorf("status = %q, want the save error", m.status)
	}
}

func TestMarkdownFilesBlueDirectoriesBold(t *testing.T) {
	if mdStyle.GetForeground() != colDir {
		t.Fatal("Markdown files should be drawn in colDir")
	}
	if dirStyle.GetForeground() != colText || !dirStyle.GetBold() {
		t.Fatal("directories should be bold in the plain text color")
	}
}

func TestMarkdownOnlyToggleKeepsCursor(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644)
	has := func(m Model, name string) bool {
		for _, e := range m.entries {
			if e.name == name {
				return true
			}
		}
		return false
	}

	m := newAt(dir)
	cursorOn(t, &m, "a.md")
	next, _ := m.Update(key("m"))
	m = next.(Model)
	if !m.mdOnly || has(m, "b.txt") || !has(m, "sub") || !has(m, "..") {
		t.Fatalf("expected only dirs and Markdown, got %+v", m.entries)
	}
	if e, _ := m.current(); e.name != "a.md" {
		t.Fatalf("cursor should stay on a.md, is on %q", e.name)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF5})
	m = next.(Model)
	if m.mdOnly || !has(m, "b.txt") {
		t.Fatal("F5 should list all files again")
	}

	// b.txt is not listed in the Markdown view, so the cursor goes to the top.
	cursorOn(t, &m, "b.txt")
	next, _ = m.Update(key("m"))
	if next.(Model).cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", next.(Model).cursor)
	}
}

func TestKeyBarShowsMarkdownFilter(t *testing.T) {
	m := newAt(t.TempDir())
	if bar := m.keyBar(); !strings.Contains(bar, "MD only") {
		t.Fatalf("expected 'MD only' in %q", bar)
	}
	m.mdOnly = true
	if bar := m.keyBar(); !strings.Contains(bar, "All files") {
		t.Fatalf("expected 'All files' in %q", bar)
	}
}

func TestGoUpPutsCursorOnDirectoryLeft(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		os.Mkdir(filepath.Join(dir, n), 0o755)
	}

	m := newAt(filepath.Join(dir, "c"))
	next, _ := m.Update(key("h"))
	if e, _ := next.(Model).current(); e.name != "c" {
		t.Fatalf("cursor should be on 'c' after going up, is on %q", e.name)
	}

	// Enter on ".." does the same.
	m = newAt(filepath.Join(dir, "b"))
	cursorOn(t, &m, "..")
	next, _ = m.Update(key("enter"))
	if e, _ := next.(Model).current(); e.name != "b" {
		t.Fatalf("cursor should be on 'b' after Enter on '..', is on %q", e.name)
	}
}

func TestMacPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("a directory cannot be made unreadable here")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	os.Mkdir(dir, 0o755)
	os.Chmod(dir, 0)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	goos = "darwin"
	t.Cleanup(func() { goos = runtime.GOOS })

	m := newAt(dir)
	var opened string
	m.open = func(path string) error { opened = path; return nil }

	view := m.View()
	for _, want := range []string{"cannot read directory", "antivirus", "Privacy & Security", "p opens the settings"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in the view:\n%s", want, view)
		}
	}
	if !strings.Contains(m.keyBar(), "p Settings") {
		t.Fatalf("expected the p key in %q", m.keyBar())
	}

	_, cmd := m.Update(key("p"))
	if cmd == nil {
		t.Fatal("expected a command from 'p'")
	}
	if msg := cmd().(openedMsg); msg.err != nil || opened != macPrivacyURL {
		t.Fatalf("expected %q to be opened, got %q (%v)", macPrivacyURL, opened, msg.err)
	}

	// Elsewhere there is only the error, and p does nothing.
	goos = "linux"
	if v := m.View(); strings.Contains(v, "terminal app") || strings.Contains(m.keyBar(), "p Settings") {
		t.Fatalf("no macOS hint expected on linux:\n%s", v)
	}
	if _, cmd := m.Update(key("p")); cmd != nil {
		t.Fatal("'p' should do nothing outside the macOS case")
	}
}

func TestMacWriteDenied(t *testing.T) {
	denied := convertDoneMsg{
		output: filepath.Join("some", "dir", "out.pdf"),
		err:    fmt.Errorf("render PDF: %w", fmt.Errorf("open x.tmp: %w", fs.ErrPermission)),
	}
	t.Cleanup(func() { goos = runtime.GOOS })

	goos = "darwin"
	m := newAt(t.TempDir())
	var opened string
	m.open = func(path string) error { opened = path; return nil }
	updated, _ := m.Update(denied)
	m = updated.(Model)
	if want := "macOS or antivirus blocked writing to dir"; !strings.Contains(m.status, want) || !strings.Contains(m.status, "F4") || m.statusKind != statusError {
		t.Fatalf("unexpected status %q", m.status)
	}
	if !strings.Contains(m.keyBar(), "p Settings") {
		t.Fatalf("expected the p key in %q", m.keyBar())
	}
	_, cmd := m.Update(key("p"))
	if cmd == nil {
		t.Fatal("expected a command from 'p'")
	}
	if msg := cmd().(openedMsg); msg.err != nil || opened != macPrivacyURL {
		t.Fatalf("expected %q to be opened, got %q (%v)", macPrivacyURL, opened, msg.err)
	}

	// The hint goes with the message: moving the cursor takes both away.
	m.entries = []entry{{name: "a.md"}, {name: "b.md"}}
	m.moveCursor(1)
	if m.writeDenied || strings.Contains(m.keyBar(), "p Settings") {
		t.Fatal("p should not be offered after the cursor moved")
	}

	// So does a new directory, and a new conversion.
	m.writeDenied = true
	m.loadEntries()
	if m.writeDenied {
		t.Fatal("p should not be offered after the directory changed")
	}
	m.writeDenied = true
	updated, _ = m.runConvert("in.md", "out.pdf", false)
	if updated.(Model).writeDenied {
		t.Fatal("p should not be offered while a conversion runs")
	}

	// Elsewhere there is only the error, and p does nothing.
	goos = "linux"
	m = newAt(t.TempDir())
	updated, _ = m.Update(denied)
	m = updated.(Model)
	if m.status != denied.err.Error() || strings.Contains(m.keyBar(), "p Settings") {
		t.Fatalf("no macOS hint expected on linux: %q", m.status)
	}
	if _, cmd := m.Update(key("p")); cmd != nil {
		t.Fatal("'p' should do nothing outside the macOS case")
	}
}

func TestKeyBarFitsEightyColumns(t *testing.T) {
	m := newAt(t.TempDir())
	m.width = 80
	m.lastOutput = "out.pdf"
	if bar := m.keyBar(); lipgloss.Width(bar) > m.innerWidth() || !strings.Contains(bar, "Quit") {
		t.Fatalf("key bar does not fit %d columns: %q", m.innerWidth(), bar)
	}
}
