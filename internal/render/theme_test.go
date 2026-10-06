package render

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// markColors gives every color in a theme its own value and returns the set,
// so the output can be checked for colors that did not come from the theme.
func markColors(th *theme.Theme) map[string]bool {
	used := map[string]bool{}
	next := 0
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if c, ok := v.Addr().Interface().(*theme.Color); ok {
			next++
			*c = theme.Color{R: 0x10, G: uint8(next), B: 0xAB}
			used[c.Hex()] = true
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(th).Elem())
	return used
}

var hexColor = regexp.MustCompile(`#?\b([0-9A-F]{6})\b`)

// Every color in the output comes from the theme: nothing is hardcoded in the
// renderers. The showcase document exercises every element.
func TestAllColorsComeFromTheTheme(t *testing.T) {
	path := filepath.Join(fixturesDir, "showcase.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.ParseFlavor(src, flavor.Default(), filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	set := theme.Default()
	pdfColors := markColors(&set.PDF)
	docxColors := markColors(&set.DOCX)
	dir := t.TempDir()

	// Not from the theme by design: black and white (the states the renderer
	// resets to) and the color a color chip displays.
	allowed := map[string]bool{"000000": true, "FFFFFF": true, "2563EB": true}

	r := NewPdfRenderer()
	r.opts = Options{BaseDir: filepath.Dir(path), Theme: &set}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(dir, "out.pdf")); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(r.trace.String(), "\n") {
		// The quoted text of a line is content, not styling
		styling := line
		if i := strings.Index(line, `"`); i >= 0 {
			styling = line[:i] + line[strings.LastIndex(line, `"`)+1:]
		}
		for _, m := range hexColor.FindAllStringSubmatch(styling, -1) {
			// Text is never drawn in the reset color: every piece of text
			// has a color the theme chose.
			isText := strings.Contains(line, " text ")
			if !pdfColors[m[1]] && (isText || !allowed[m[1]]) {
				t.Errorf("PDF: color #%s is not from the theme: %s", m[1], line)
			}
		}
	}

	out := filepath.Join(dir, "out.docx")
	if err := RenderDocx(doc, out, Options{BaseDir: filepath.Dir(path), Theme: &set}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(docxOutline(t, out), "\n") {
		styling := line
		if i := strings.Index(line, `"`); i >= 0 {
			styling = line[:i]
		}
		for _, m := range regexp.MustCompile(`(?:color|fill)=([0-9A-Fa-f]{6})\b|/([0-9A-Fa-f]{6})\b`).FindAllStringSubmatch(styling, -1) {
			c := strings.ToUpper(m[1] + m[2])
			if !docxColors[c] && !allowed[c] {
				t.Errorf("DOCX: color %s is not from the theme: %s", c, line)
			}
		}
	}
}

// Sizes, spacing and margins follow the theme too.
func TestThemeChangesLayout(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte("# Title\n\nBody text.\n\n- item\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	set := theme.Default()
	set.PDF.Page.MarginLeft = 100
	set.PDF.Page.MarginTop = 90
	set.PDF.Text.Size = 13
	set.PDF.Heading[0].Size = 30
	set.PDF.Heading[0].SpaceBefore = 0
	set.PDF.List.Indent = 40
	set.PDF.Fonts.Heading = "mono"

	r := NewPdfRenderer()
	r.opts = Options{Theme: &set}
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	trace := r.trace.String()
	for _, want := range []string{
		`x=100.0 y=90.0 Courier/30 "Title"`, // margins, heading size and font
		`x=100.0 y=130.0 Arial/13 "Body"`,   // body size
		`x=140.0`,                           // list indent from the left margin
	} {
		if !strings.Contains(trace, want) {
			t.Errorf("missing %s in:\n%s", want, trace)
		}
	}

	set.DOCX.Text.Size = 13
	set.DOCX.Heading[0].Size = 30
	set.DOCX.Heading[0].Color = theme.Hex("AA0000")
	set.DOCX.Fonts.Body = "Arial"
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{Theme: &set}); err != nil {
		t.Fatal(err)
	}
	outline := docxOutline(t, out)
	for _, want := range []string{`color=AA0000`, `sz=60`, `font=Arial`, `sz=26`} {
		if !strings.Contains(outline, want) {
			t.Errorf("missing %s in:\n%s", want, outline)
		}
	}
}
