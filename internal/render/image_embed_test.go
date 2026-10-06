package render

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
)

// writeTestImages creates a PNG, a JPEG and an SVG with text in dir.
func writeTestImages(t *testing.T, dir string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y * 2), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "pic.png"), buf.Bytes())

	buf.Reset()
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "photo.jpg"), buf.Bytes())

	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="150" viewBox="0 0 300 150">
  <rect width="300" height="150" fill="#eef"/>
  <text x="20" y="80" font-family="Helvetica" font-size="24">Diagram</text>
</svg>`
	write(t, filepath.Join(dir, "diagram.svg"), []byte(svg))
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func imageDoc() *ast.Document {
	doc := ast.NewDocument()
	doc.AppendElement(ast.NewHeading(1, ast.NewInlineRun("Images")))
	doc.AppendElement(ast.NewImage("A PNG", "pic.png", ""))
	doc.AppendElement(ast.NewImage("A JPEG", "photo.jpg", ""))
	doc.AppendElement(ast.NewImage("An SVG", "diagram.svg", ""))
	narrow := ast.NewImage("Narrow", "pic.png", "")
	narrow.Width = 100
	doc.AppendElement(narrow)
	doc.AppendElement(ast.NewImage("Missing", "nope.png", ""))
	return doc
}

func TestImagesAreEmbeddedInPDF(t *testing.T) {
	dir := t.TempDir()
	writeTestImages(t, dir)

	r := NewPdfRenderer()
	r.opts = Options{BaseDir: dir}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(imageDoc(), filepath.Join(dir, "out.pdf")); err != nil {
		t.Fatal(err)
	}
	trace := r.trace.String()

	// PNG and JPEG are 200x100 px = 150x75 pt at 96 DPI; the SVG is 300x150 px
	// = 225x112.5 pt; the width hint of 100 px gives 75x37.5 pt.
	for _, want := range []string{"w=150.0 h=75.0", "w=225.0 h=112.5", "w=75.0 h=37.5"} {
		if !strings.Contains(trace, "image") || !strings.Contains(trace, want) {
			t.Errorf("no image drawn at %s in:\n%s", want, trace)
		}
	}
	if n := strings.Count(trace, " image "); n != 4 {
		t.Errorf("%d images drawn, want 4", n)
	}
	// The image that cannot be loaded falls back to its alt text and path.
	if !strings.Contains(trace, `"[IMG] Missing"`) || !strings.Contains(trace, `"nope.png"`) {
		t.Errorf("missing image has no placeholder in:\n%s", trace)
	}
}

func TestImagesAreEmbeddedInDOCX(t *testing.T) {
	dir := t.TempDir()
	writeTestImages(t, dir)

	out := filepath.Join(dir, "out.docx")
	if err := RenderDocx(imageDoc(), out, Options{BaseDir: dir}); err != nil {
		t.Fatal(err)
	}
	outline := docxOutline(t, out)

	// DOCX sizes are in EMU: 9525 per pixel.
	for _, want := range []string{"IMAGE cx=1905000 cy=952500", "IMAGE cx=2857500 cy=1428750", "IMAGE cx=952500 cy=476250"} {
		if !strings.Contains(outline, want) {
			t.Errorf("missing %q in:\n%s", want, outline)
		}
	}
	if n := strings.Count(outline, "IMAGE "); n != 4 {
		t.Errorf("%d images embedded, want 4", n)
	}
	for _, caption := range []string{`"A PNG"`, `"A JPEG"`, `"An SVG"`} {
		if !strings.Contains(outline, caption) {
			t.Errorf("caption %s missing", caption)
		}
	}
	if !strings.Contains(outline, "Missing") {
		t.Errorf("missing image has no placeholder in:\n%s", outline)
	}
}

// An SVG is rasterized with its text, at twice the display size.
func TestRasterizeSVG(t *testing.T) {
	dir := t.TempDir()
	writeTestImages(t, dir)
	data, displayWidth, err := loadRasterImage("diagram.svg", Options{BaseDir: dir}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if displayWidth != 300 {
		t.Errorf("display width = %d, want the SVG's own 300", displayWidth)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" {
		t.Fatalf("rasterized SVG is %q, %v", format, err)
	}
	if cfg.Width != 600 || cfg.Height != 300 {
		t.Errorf("rasterized to %dx%d, want 600x300", cfg.Width, cfg.Height)
	}

	// The text must have been drawn: some pixels differ from the background.
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bg := img.At(590, 290)
	dark := 0
	for y := 100; y < 200; y++ {
		for x := 30; x < 300; x++ {
			if img.At(x, y) != bg {
				dark++
			}
		}
	}
	if dark < 200 {
		t.Errorf("only %d pixels differ from the background: the SVG text was not rendered", dark)
	}
}
