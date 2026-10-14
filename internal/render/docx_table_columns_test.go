package render

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
)

// wideTable is a Markdown table of cols columns and two rows.
func wideTable(cols int) string {
	row := func(cell func(int) string) string {
		var b strings.Builder
		for j := 0; j < cols; j++ {
			b.WriteString("| " + cell(j) + " ")
		}
		return b.String() + "|\n"
	}
	return row(func(j int) string { return fmt.Sprintf("h%d", j) }) +
		row(func(int) string { return "---" }) +
		row(func(j int) string { return fmt.Sprintf("a%d", j) }) +
		row(func(j int) string { return fmt.Sprintf("b%d", j) })
}

// The DOCX library takes at most 63 columns. A wider table is cut to them
// with a warning, where it used to fail the whole conversion; the PDF has all
// the columns.
func TestWideTableKeepsFirst63ColumnsInDOCX(t *testing.T) {
	const cols = 70
	doc, err := parse.ParseFlavor([]byte("Before.\n\n"+wideTable(cols)+"\nAfter.\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}

	var warnings []string
	opts := Options{Warn: func(m string) { warnings = append(warnings, m) }}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, opts); err != nil {
		t.Fatal(err)
	}

	outline := docxOutline(t, out)
	if n := strings.Count(outline, "COL w="); n != 63 {
		t.Errorf("table has %d columns, want 63", n)
	}
	// Three rows of 63 cells.
	if n := strings.Count(outline, "CELL"); n != 3*63 {
		t.Errorf("table has %d cells, want %d", n, 3*63)
	}
	for _, want := range []string{`"h62"`, `"b62"`, "Before.", "After."} {
		if !strings.Contains(outline, want) {
			t.Errorf("%s missing from the DOCX", want)
		}
	}
	if strings.Contains(outline, `"h63"`) {
		t.Error("column 64 is in the DOCX")
	}
	if want := "a table has 70 columns; DOCX shows the first 63"; len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %q, want %q", warnings, want)
	}

	// A table of 63 columns is whole, and there is nothing to warn about.
	warnings = nil
	doc, err = parse.ParseFlavor([]byte(wideTable(63)), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenderDocx(doc, out, opts); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q for 63 columns", warnings)
	}
}

func TestWideTableIsWholeInPDF(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(wideTable(70)), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPdfRenderer()
	r.opts = Options{Warn: func(m string) { t.Errorf("unexpected warning %q", m) }}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
}
