package render

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// A width hint too big for an int must not turn into a negative or absurd
// size, whichever way the width is written.
func TestHugeWidthHintGivesSaneImages(t *testing.T) {
	dir := t.TempDir()
	writeTestImages(t, dir)

	sources := map[string]string{
		"attribute list": "![a](pic.png){width=9999999999999999}\n",
		"obsidian":       "![a|9999999999999999](pic.png)\n",
		"percent":        "![a](pic.png){width=9999999999999999%}\n",
		"html":           "<img src=\"pic.png\" width=\"9999999999999999\">\n",
		"html percent":   "<img src=\"pic.png\" width=\"9999999999999999%\">\n",
		"svg":            "![a](diagram.svg){width=9999999999999999}\n",
	}
	extent := regexp.MustCompile(`IMAGE cx=(\d+) cy=(\d+)`)
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			doc, err := parse.ParseFlavor([]byte(src), flavor.Default(), "")
			if err != nil {
				t.Fatal(err)
			}

			docx := filepath.Join(dir, "out.docx")
			if err := RenderDocx(doc, docx, Options{BaseDir: dir}); err != nil {
				t.Fatal(err)
			}
			m := extent.FindStringSubmatch(docxOutline(t, docx))
			if m == nil {
				t.Fatalf("no image in the DOCX:\n%s", docxOutline(t, docx))
			}
			cx, _ := strconv.Atoi(m[1])
			cy, _ := strconv.Atoi(m[2])
			// The text is at most 16 cm wide, which is 6e5 EMU a centimeter.
			if cx <= 0 || cy <= 0 || cx > 10_000_000 || cy > 10_000_000 {
				t.Errorf("image extent %d x %d EMU is not a sane size", cx, cy)
			}

			r := NewPdfRenderer()
			r.opts = Options{BaseDir: dir}
			r.trace = &strings.Builder{}
			if err := r.RenderToFile(doc, filepath.Join(dir, "out.pdf")); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(r.trace.String(), " image ") {
				t.Errorf("no image drawn in the PDF:\n%s", r.trace)
			}
		})
	}
}

func TestScaleDimension(t *testing.T) {
	tests := []struct{ size, num, den, want int }{
		{100, 50, 200, 25},
		{1, 1, 1000, 1},                // never below one
		{1 << 31, 1 << 31, 1, 1 << 62}, // no overflow in 64 bits
	}
	for _, tt := range tests {
		if got := scaleDimension(tt.size, tt.num, tt.den); got != tt.want {
			t.Errorf("scaleDimension(%d, %d, %d) = %d, want %d", tt.size, tt.num, tt.den, got, tt.want)
		}
	}
}
