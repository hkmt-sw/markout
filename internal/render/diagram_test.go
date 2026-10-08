package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

const chartDoc = "Before.\n\n```mermaid\ngraph TD\n    A[Start] --> B{Choice}\n    B -->|yes| C([Done])\n    B -.->|no| A\n```\n\nAfter.\n"

// A flowchart is drawn in PDF: a shape and its text for every box, a curve
// for every connection, in the theme's colors, and no source.
func TestFlowchartInPDF(t *testing.T) {
	trace, warnings := pdfWarnings(t, chartDoc)
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	colors := theme.Default().PDF.Diagram
	if n := len(traceLines(trace, "shape")); n != 3+3 {
		t.Errorf("%d shapes, want 3 boxes and 3 arrowheads:\n%s", n, trace)
	}
	if !strings.Contains(trace, "fill=#"+colors.Background.Hex()+" stroke=#"+colors.Border.Hex()) {
		t.Errorf("the boxes are not in the theme's colors:\n%s", trace)
	}
	if n := len(traceLines(trace, "curve")); n < 3 {
		t.Errorf("%d curves for 3 connections", n)
	}
	texts := drawnWith(t, trace)
	for _, label := range []string{"Start", "Choice", "Done", "yes", "no"} {
		find(t, texts, label)
	}
	if strings.Contains(trace, "Mermaid Diagram") || strings.Contains(trace, "-->") {
		t.Errorf("the source of the chart is shown:\n%s", trace)
	}
	// The chart is between the paragraphs around it
	before, start, after := find(t, texts, "Before."), find(t, texts, "Start"), find(t, texts, "After.")
	if !(before.y < start.y && start.y < after.y) {
		t.Errorf("Before at y=%.0f, the chart at y=%.0f, After at y=%.0f", before.y, start.y, after.y)
	}
}

// A chart wider than the text is made smaller, and stays inside the margins.
func TestWideFlowchartIsShrunk(t *testing.T) {
	var src strings.Builder
	src.WriteString("```mermaid\ngraph LR\n")
	for i := 0; i < 5; i++ {
		src.WriteString("    n" + string(rune('a'+i)) + "[A box with a long label] --> n" + string(rune('b'+i)) + "\n")
	}
	src.WriteString("```\n")
	trace, warnings := pdfWarnings(t, src.String())
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	checkOnThePage(t, trace)
	if label := find(t, drawnWith(t, trace), "A box with a long label"); label.size >= theme.Default().PDF.Text.Size*chartFontScale {
		t.Errorf("the text is %.1f pt, want it smaller than a chart that fits", label.size)
	}
}

// A chain too long to be read sideways is drawn from the top down.
func TestLongSidewaysFlowchartIsTurned(t *testing.T) {
	var src strings.Builder
	src.WriteString("```mermaid\ngraph LR\n")
	for i := 0; i < 14; i++ {
		src.WriteString("    n" + string(rune('a'+i)) + "[A box with a long label] --> n" + string(rune('b'+i)) + "\n")
	}
	src.WriteString("```\n")
	trace, warnings := pdfWarnings(t, src.String())
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
	checkOnThePage(t, trace)
	var labels []drawnAt
	for _, d := range drawnWith(t, trace) {
		if d.text == "A box with a long label" {
			labels = append(labels, d)
		}
	}
	if len(labels) != 14 || labels[0].x != labels[13].x || labels[0].y >= labels[13].y {
		t.Errorf("%d boxes; the first at %.0f,%.0f and the last at %.0f,%.0f: want them one under the other",
			len(labels), labels[0].x, labels[0].y, labels[len(labels)-1].x, labels[len(labels)-1].y)
	}
}

// A diagram that is not a flowchart, or a flowchart that cannot be read, is
// shown as its source, and the conversion says which.
func TestDiagramsThatAreNotDrawn(t *testing.T) {
	md := "```mermaid\nsequenceDiagram\n    A->>B: hi\n```\n\n```mermaid\npie\n    \"a\": 1\n```\n\n```mermaid\nsequenceDiagram\n    C->>D: ho\n```\n\n" +
		"```mermaid\ngraph TD\n    A[unclosed --> B\n```\n"
	trace, warnings := pdfWarnings(t, md)
	if n := strings.Count(trace, `"Mermaid Diagram"`); n != 4 {
		t.Errorf("%d source panels, want 4", n)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings: %q", warnings)
	}
	if w := warnings[0]; !strings.Contains(w, "3 diagrams") || !strings.Contains(w, "not sequenceDiagram, pie") {
		t.Errorf("kinds: %s", w)
	}
	if w := warnings[1]; !strings.Contains(w, "1 flowchart could not be drawn") || !strings.Contains(w, "not closed") {
		t.Errorf("unread: %s", w)
	}
}

// In DOCX a flowchart is a picture, and one that is not drawn is its source.
func TestFlowchartInDOCX(t *testing.T) {
	doc, err := parse.ParseFlavor([]byte(chartDoc+"\n```mermaid\ngantt\n    title GANTTSOURCE\n```\n"), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.docx")
	var warnings []string
	if err := RenderDocx(doc, out, Options{Warn: func(m string) { warnings = append(warnings, m) }}); err != nil {
		t.Fatal(err)
	}
	document := docxPart(t, out, "word/document.xml")
	if n := strings.Count(document, "<w:drawing>"); n != 1 {
		t.Errorf("%d pictures, want 1", n)
	}
	if strings.Contains(document, "Start") || strings.Contains(document, "--&gt;") {
		t.Error("the source of the flowchart is in the document")
	}
	if !strings.Contains(document, "GANTTSOURCE") {
		t.Error("the source of the diagram that is not drawn is missing")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not gantt") {
		t.Errorf("warnings: %q", warnings)
	}
	checkDocxConsistent(t, out)
}
