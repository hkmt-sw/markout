package render

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// captureStderr runs fn and returns what it wrote to standard error, which is
// where the WebAssembly runtime of the rasterizer prints a panic.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stderr = old }()
	fn()
	_ = w.Close()
	return <-done
}

// An SVG of extreme proportions is refused before the rasterizer sees it.
func TestRasterizeSVGRefusesExtremeProportions(t *testing.T) {
	for _, viewBox := range []string{"0 0 1 100000", "0 0 100000 1", "0 0 1 1e999", "0 0 Inf 5", "0 0 NaN NaN"} {
		t.Run(viewBox, func(t *testing.T) {
			svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="` + viewBox + `"><rect width="1" height="1"/></svg>`
			var err error
			stderr := captureStderr(t, func() { _, err = rasterizeSVG([]byte(svg), 0) })
			if stderr != "" {
				t.Errorf("the rasterizer printed on standard error:\n%s", stderr)
			}
			if viewBox == "0 0 1 100000" && !errors.Is(err, ErrSVGTooLarge) {
				t.Errorf("error = %v, want ErrSVGTooLarge", err)
			}
		})
	}
}

// A hint too big for the arithmetic is held to the largest width.
func TestRasterizeSVGHugeHint(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`
	data, err := rasterizeSVG([]byte(svg), int(^uint(0)>>1))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("no image")
	}
}

// A document with such an SVG is made, with a placeholder and a warning.
func TestExtremeSVGGivesPlaceholderAndWarning(t *testing.T) {
	dir := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 100000"><rect width="1" height="100000"/></svg>`
	write(t, filepath.Join(dir, "thin.svg"), []byte(svg))
	doc, err := parse.ParseFlavor([]byte("![thin](thin.svg)\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}

	var warnings []string
	opts := Options{BaseDir: dir, Warn: func(m string) { warnings = append(warnings, m) }}
	out := filepath.Join(dir, "out.docx")
	if err := RenderDocx(doc, out, opts); err != nil {
		t.Fatal(err)
	}
	if outline := docxOutline(t, out); strings.Contains(outline, "IMAGE ") || !strings.Contains(outline, "thin") {
		t.Errorf("no placeholder in:\n%s", outline)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "SVG") {
		t.Errorf("warnings = %q, want one about the SVG", warnings)
	}
}
