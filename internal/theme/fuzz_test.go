package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fuzzTimeLimit is how long loading one theme may take.
const fuzzTimeLimit = 2 * time.Second

// FuzzTheme loads arbitrary text as a theme file. It must not panic and must
// not hang; an error is the right answer for most inputs. A theme that does
// load must also export.
//
//	go test -run='^$' -fuzz=FuzzTheme -fuzztime=60s ./internal/theme
func FuzzTheme(f *testing.F) {
	for _, seed := range []string{
		"",
		"extends = \"default\"\n\n[page]\nsize = \"A4\"\nmargin = \"25mm\"\n\n[heading.h1]\nsize = 28\ncolor = \"#1A1A2E\"\n",
		"extends = \"modern\"\ndescription = \"x\"\n\n[pdf.page]\nmargin = \"1in\"\n\n[docx.body]\nsize = 11\n",
		"extends = \"\"\n",
		"extends = 5\n",
		"extends = \"/nonexistent/loop.toml\"\n",
		"[fonts.family.brand]\nregular = \"brand.ttf\"\nbold = \"brand-bold.ttf\"\nname = \"Brand\"\n",
		"[classification.secret]\nlabel = \"Secret\"\ncolor = \"#FF0000\"\n",
		"[page]\nmargin = \"1e400mm\"\nsize = \"-1\"\n",
		"name = 1\ndescription = [1]\npdf = 3\n",
		"[heading]\nh1 = 1\n[heading.h7]\nsize = 1\n",
	} {
		f.Add(seed)
	}
	builtin, _ := filepath.Glob(filepath.Join("builtin", "*.toml"))
	for _, file := range builtin {
		if data, err := os.ReadFile(file); err == nil {
			f.Add(string(data))
		}
	}
	f.Add(Export(Default()))

	dir := f.TempDir()
	path := filepath.Join(dir, "fuzz.toml")

	f.Fuzz(func(t *testing.T, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		err := runBounded(fuzzTimeLimit, func() {
			set, err := Loader{UserDir: dir}.Load(path)
			if err != nil {
				return
			}
			_ = Export(set)
		})
		if err != nil {
			t.Fatalf("%v\ninput: %q", err, text)
		}
	})
}

// runBounded runs fn on its own goroutine and reports a panic or a run that
// takes longer than limit as an error.
func runBounded(limit time.Duration, fn func()) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("panic: %v", p)
			}
		}()
		fn()
		done <- nil
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("still running after %v", limit)
	}
}
