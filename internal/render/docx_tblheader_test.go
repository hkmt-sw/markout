package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
)

// headerRows tells, for every table of a document body, in which of its rows
// Word is to repeat the header: the rows with tblHeader, as numbers.
func headerRows(body string) [][]int {
	var out [][]int
	for _, table := range strings.Split(body, "<w:tbl>")[1:] {
		table, _, _ = strings.Cut(table, "</w:tbl>")
		rows := []int{}
		for i, row := range strings.Split(table, "<w:tr>")[1:] {
			if strings.Contains(row, "<w:tblHeader/>") {
				rows = append(rows, i)
			}
		}
		out = append(out, rows)
	}
	return out
}

// Word repeats a row with tblHeader at the top of each page the table runs on
// to: only the header row of a table that has one gets it, and no table drawn
// for its looks.
func TestDOCXRepeatsOnlyHeaderRows(t *testing.T) {
	const md = "---\ntitle: Backup\nversion: 1.1\n---\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n\n" +
		"> [!NOTE]\n> A note.\n\n" +
		"```\ncode\n```\n\n" +
		"| c | d |\n|---|---|\n| 5 | 6 |\n"
	doc := withHistory(t, md, testHistory)

	// A table without a header row, after the history
	cell := func(s string) ast.TableCell { return ast.TableCell{Runs: []ast.InlineRun{{Text: s}}} }
	headerless := ast.Table{Rows: []ast.TableRow{
		{Cells: []ast.TableCell{cell("x"), cell("y")}},
		{Cells: []ast.TableCell{cell("z"), cell("w")}},
	}}
	doc.Elements = append(doc.Elements[:2], append([]ast.Element{headerless}, doc.Elements[2:]...)...)

	out := filepath.Join(t.TempDir(), "out.docx")
	if err := RenderDocx(doc, out, Options{}); err != nil {
		t.Fatal(err)
	}
	body := docxPart(t, out, "word/document.xml")

	// In order: the control block, the history, the table without a header,
	// the one with, the callout, the code block, a second table with
	want := [][]int{{}, {0}, {}, {0}, {}, {}, {0}}
	got := headerRows(body)
	if len(got) != len(want) {
		t.Fatalf("%d tables, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) || (len(want[i]) > 0 && got[i][0] != want[i][0]) {
			t.Errorf("table %d repeats rows %v, want %v", i, got[i], want[i])
		}
	}
	if rows := strings.Count(body, "<w:tr>"); strings.Count(body, "<w:cantSplit/>") != rows {
		t.Errorf("%d rows that cannot split, want all %d", strings.Count(body, "<w:cantSplit/>"), rows)
	}
}

// With the tables not counted right, no row is made a header.
func TestAddTableRowPropertiesWithoutHeaders(t *testing.T) {
	body := []byte("<w:tbl><w:tblPr/><w:tr><w:tc/></w:tr></w:tbl>")
	if got := string(addTableRowProperties(body, []bool{true, true})); strings.Contains(got, "tblHeader") {
		t.Errorf("a header row where the tables do not match: %s", got)
	}
	if got := string(addTableRowProperties(body, []bool{true})); !strings.Contains(got, "<w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>") {
		t.Errorf("no header row: %s", got)
	}
}
