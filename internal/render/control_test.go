package render

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// The control block shows the keys it knows first, in its order and under
// labels in the document's language, then the other text values under their
// own names.
func TestControlFields(t *testing.T) {
	fm := ast.FrontMatter{
		Raw: map[string]any{
			"title":          "Backup",
			"classification": "internal",
			"Approved_By":    "The Director",
			"version":        1.0,
			"date":           time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
			"review-date":    time.Date(2027, 10, 14, 9, 30, 0, 0, time.UTC),
			"scope":          "All systems",
			"audience":       "Staff",
			"tags":           []any{"a", "b"},
			"draft":          false,
			"weight":         int64(3),
			"status":         "",
		},
		Written: map[string]string{"version": "1.0", "weight": "3"},
	}
	join := func(fields []controlField) string {
		var parts []string
		for _, f := range fields {
			parts = append(parts, f.label+"="+f.value)
		}
		return strings.Join(parts, "; ")
	}

	want := "Title=Backup; Version=1.0; Approved by=The Director; Date=2026-10-14; Review date=2027-10-14 09:30; Classification=internal; audience=Staff; scope=All systems"
	if got := join(controlFields(fm, "en-GB", theme.ControlLabels{})); got != want {
		t.Errorf("English:\n got %s\nwant %s", got, want)
	}
	if got := join(controlFields(fm, "", theme.ControlLabels{})); got != want {
		t.Errorf("no language:\n got %s\nwant %s", got, want)
	}

	hu := controlFields(fm, "hu", theme.ControlLabels{})
	if got := join(hu[:3]); got != "Cím=Backup; Verzió=1.0; Jóváhagyta=The Director" {
		t.Errorf("Hungarian: %s", got)
	}

	own := controlFields(fm, "hu", theme.ControlLabels{Version: "Kiadás", ApprovedBy: "Approver"})
	if got := join(own[:3]); got != "Cím=Backup; Kiadás=1.0; Approver=The Director" {
		t.Errorf("labels from the theme: %s", got)
	}
}

// In PDF the labels are in a column of their own, with a line between the
// fields; in DOCX the block is a table of two columns, a row a field.
func TestControlBlock(t *testing.T) {
	md := "---\ntitle: Backup\nversion: 1.0\nowner: IT\n---\n\ntext\n"
	trace := pdfTraceWith(t, md, theme.Default())
	var labelX, valueX []string
	lines := 0
	for _, line := range strings.Split(trace, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		switch {
		case strings.Contains(line, `Bold/11 "Title"`), strings.Contains(line, `Bold/11 "Version"`), strings.Contains(line, `Bold/11 "Owner"`):
			labelX = append(labelX, fields[2])
		case strings.Contains(line, `"Backup"`), strings.Contains(line, `"1.0"`), strings.Contains(line, `"IT"`):
			valueX = append(valueX, fields[2])
		case fields[1] == "line":
			lines++
		}
	}
	if len(labelX) != 3 || len(valueX) != 3 {
		t.Fatalf("labels at %v, values at %v:\n%s", labelX, valueX, trace)
	}
	for i := 1; i < 3; i++ {
		if labelX[i] != labelX[0] || valueX[i] != valueX[0] {
			t.Errorf("the columns do not line up: labels at %v, values at %v", labelX, valueX)
		}
	}
	if lines != 2 {
		t.Errorf("%d lines between the fields, want 2", lines)
	}

	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	body := docxPart(t, out, "word/document.xml")
	if n := strings.Count(body, "<w:tr>") + strings.Count(body, "<w:tr "); n != 3 {
		t.Errorf("%d table rows, want 3", n)
	}
	if n := strings.Count(body, "<w:gridCol "); n != 2 {
		t.Errorf("%d table columns, want 2", n)
	}
	for _, want := range []string{">Title<", ">Backup<", ">Version<", ">1.0<", ">Owner<", ">IT<"} {
		if !strings.Contains(body, want) {
			t.Errorf("the DOCX lacks %s", want)
		}
	}
}
