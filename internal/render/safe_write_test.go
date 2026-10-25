package render

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// leftovers lists what is in dir.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// isolateTemp makes the system's temporary directory a new empty one, which
// it returns. Directories made by t.TempDir afterwards would be inside it, so
// the test makes its own before calling this.
func isolateTemp(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "markout-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	return dir
}

// A render that cannot put its output in place must say so, must not touch
// what is there, and must not leave temporary files in the output directory
// or in the system's temporary directory. A directory in the output's place
// is the cheapest way to make the last step fail after all the work is done.
func TestFailedWriteLeavesNothingBehind(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("# Title\n\nText.\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	sysTmp := isolateTemp(t)

	for name, render := range map[string]func(path string) error{
		"docx": func(path string) error { return RenderDocx(doc, path, Options{}) },
		"pdf":  func(path string) error { return RenderPdf(doc, path, Options{}) },
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(work, name)
			out := filepath.Join(dir, "out."+name)
			if err := os.MkdirAll(filepath.Join(out, "keep"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := render(out); err == nil {
				t.Fatal("the render succeeded with a directory in place of the output")
			}
			if got := leftovers(t, dir); len(got) != 1 || got[0] != "out."+name {
				t.Errorf("the output directory holds %v", got)
			}
			if got := leftovers(t, out); len(got) != 1 || got[0] != "keep" {
				t.Errorf("what was in the output's place changed: %v", got)
			}
			if got := leftovers(t, sysTmp); len(got) != 0 {
				t.Errorf("temporary files are left behind: %v", got)
			}
		})
	}
}

// A render that works replaces an earlier file and leaves no temporary file.
func TestSuccessfulWriteReplacesTheOutput(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("# Title\n\nText.\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sysTmp := isolateTemp(t)

	// A file of the user's that the old intermediate name would have clobbered
	if err := os.WriteFile(filepath.Join(dir, "out.docx.tmp"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.docx")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); len(got) < 4 || string(got[:2]) != "PK" {
		t.Errorf("the output is not a DOCX: %q", got[:min(len(got), 8)])
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "out.docx.tmp")); string(got) != "mine" {
		t.Errorf("the user's file was changed: %q", got)
	}
	if got := leftovers(t, dir); len(got) != 2 {
		t.Errorf("the output directory holds %v", got)
	}
	if got := leftovers(t, sysTmp); len(got) != 0 {
		t.Errorf("temporary files are left behind: %v", got)
	}
}
