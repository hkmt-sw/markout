package tui

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/settings"
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
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644)

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
	os.WriteFile(filepath.Join(dir, "doc.md"), []byte("x"), 0o644)
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
	os.WriteFile(in, []byte("# Hello\n\nWorld.\n"), 0o644)

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
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "inner.md"), []byte("x"), 0o644)

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
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
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
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
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
	os.WriteFile(in, []byte("# Hi\n"), 0o644)
	os.WriteFile(out, []byte("OLD"), 0o644) // existing output

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
	os.WriteFile(in, []byte("# Hi\n\nBody.\n"), 0o644)
	os.WriteFile(out, []byte("OLD"), 0o644)

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
	os.WriteFile(in, []byte("# Hi\n\nBody.\n"), 0o644)
	os.WriteFile(out, []byte("OLD"), 0o644)

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
	if !strings.Contains(m.View(), "Markdown flavor") {
		t.Fatal("expected the dialog to be drawn")
	}

	// Move down twice and confirm: the third flavor in the list is chosen.
	for i := 0; i < 2; i++ {
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
		t.Fatal("cancelling must not save")
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
	os.WriteFile(src, []byte("# Title\n\n{+ added +}\n"), 0o644)

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

// docxBody returns the main document XML of a DOCX file.
func docxBody(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatal("word/document.xml not found")
	return ""
}
