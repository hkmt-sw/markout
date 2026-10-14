package render

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hkmt-sw/markout/internal/ast"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/parse"
	"github.com/hkmt-sw/markout/internal/theme"
)

// testHistory is a version history with a version on one entry only.
var testHistory = ast.VersionHistory{Entries: []ast.VersionEntry{
	{Version: "1.1", Date: "2026-03-15", Author: "Béla Nagy", Change: "A mentések megőrzési ideje 90 nap."},
	{Date: "2026-01-10", Author: "Anna Kovács", Change: "Első változat."},
}}

// withHistory parses Markdown and puts the version history after its front
// matter, where the conversion puts it.
func withHistory(t *testing.T, md string, h ast.VersionHistory) *ast.Document {
	t.Helper()
	doc, err := parse.ParseFlavor([]byte(md), flavor.Default(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Elements[0].(ast.FrontMatter); !ok {
		t.Fatal("the document does not begin with front matter")
	}
	doc.Elements = append(doc.Elements[:1], append([]ast.Element{h}, doc.Elements[1:]...)...)
	return doc
}

func TestHistoryTable(t *testing.T) {
	cell := func(row ast.TableRow) []string {
		var out []string
		for _, c := range row.Cells {
			out = append(out, c.Runs[0].Text)
		}
		return out
	}
	noVersions := ast.VersionHistory{Entries: testHistory.Entries[1:]}
	for _, tc := range []struct {
		name     string
		h        ast.VersionHistory
		lang     string
		title    string
		header   []string
		firstRow []string
	}{
		{"English", noVersions, "", "Version history", []string{"Date", "Author", "Change"},
			[]string{"2026-01-10", "Anna Kovács", "Első változat."}},
		{"another language", noVersions, "de", "Version history", []string{"Date", "Author", "Change"},
			[]string{"2026-01-10", "Anna Kovács", "Első változat."}},
		{"Hungarian", noVersions, "hu", "Változástörténet", []string{"Dátum", "Szerző", "Változás"},
			[]string{"2026-01-10", "Anna Kovács", "Első változat."}},
		{"Hungarian region", testHistory, "hu-HU", "Változástörténet", []string{"Verzió", "Dátum", "Szerző", "Változás"},
			[]string{"1.1", "2026-03-15", "Béla Nagy", "A mentések megőrzési ideje 90 nap."}},
		{"with versions", testHistory, "en-GB", "Version history", []string{"Version", "Date", "Author", "Change"},
			[]string{"1.1", "2026-03-15", "Béla Nagy", "A mentések megőrzési ideje 90 nap."}},
	} {
		title, table := historyTable(tc.h, tc.lang)
		if title != tc.title {
			t.Errorf("%s: title %q, want %q", tc.name, title, tc.title)
		}
		if got := cell(table.Header); strings.Join(got, "|") != strings.Join(tc.header, "|") {
			t.Errorf("%s: header %q, want %q", tc.name, got, tc.header)
		}
		if got := cell(table.Rows[0]); strings.Join(got, "|") != strings.Join(tc.firstRow, "|") {
			t.Errorf("%s: first row %q, want %q", tc.name, got, tc.firstRow)
		}
		if len(table.Rows) != len(tc.h.Entries) || len(table.Alignments) != len(tc.header) {
			t.Errorf("%s: %d rows, %d alignments", tc.name, len(table.Rows), len(table.Alignments))
		}
	}
}

// pdfTraceOf renders a document to PDF and returns the trace of what was drawn.
func pdfTraceOf(t *testing.T, doc *ast.Document, opts Options) string {
	t.Helper()
	r := NewPdfRenderer()
	r.opts = opts
	r.trace = &strings.Builder{}
	if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
		t.Fatal(err)
	}
	return r.trace.String()
}

// indexIn is where want is in text, which must have it.
func indexIn(t *testing.T, text, want string) int {
	t.Helper()
	i := strings.Index(text, want)
	if i < 0 {
		t.Fatalf("missing %s in:\n%s", want, text)
	}
	return i
}

func TestPDFVersionHistory(t *testing.T) {
	const front = "---\ntitle: Backup\nlang: hu\nversion: 1.1\nhistory: git\n"
	const body = "---\n\n# Szabály\n\nbody text\n"
	set := theme.Default()
	heading := "Arial-Bold/" + strconv.FormatFloat(set.PDF.Heading[2].Size, 'f', -1, 64)

	t.Run("after the control block", func(t *testing.T) {
		trace := pdfTraceOf(t, withHistory(t, front+body, testHistory), Options{Lang: "hu"})
		// The block, then the title in the size of a third-level heading, then
		// the table, then the text
		block := indexIn(t, trace, `"Backup"`)
		title := indexIn(t, trace, heading+` "Változástörténet"`)
		header := title + indexIn(t, trace[title:], `"Verzió"`) // the block has the word too
		row := indexIn(t, trace, `"A mentések megőrzési ideje 90 nap."`)
		text := indexIn(t, trace, `"body"`)
		if !(block < title && title < header && header < row && row < text) {
			t.Errorf("out of order: block %d, title %d, header %d, row %d, text %d\n%s", block, title, header, row, text, trace)
		}
		for _, want := range []string{`"Dátum"`, `"Szerző"`, `"Változás"`, `"2026-03-15"`, `"Béla Nagy"`, `"Első változat."`} {
			indexIn(t, trace, want)
		}
		// history steers the conversion and is no row of the block
		for _, not := range []string{`"history"`, `"git"`} {
			if strings.Contains(trace, not) {
				t.Errorf("the trace has %s", not)
			}
		}
		if strings.Contains(trace, "p2 ") {
			t.Errorf("a short document has a second page")
		}
	})

	t.Run("not a heading", func(t *testing.T) {
		doc := withHistory(t, front+body, testHistory)
		r := NewPdfRenderer()
		r.opts = Options{Lang: "hu"}
		if err := r.RenderToFile(doc, filepath.Join(t.TempDir(), "out.pdf")); err != nil {
			t.Fatal(err)
		}
		// The document has one heading
		if len(r.outline) != 1 {
			t.Errorf("%d bookmarks, want 1", len(r.outline))
		}
	})

	t.Run("English", func(t *testing.T) {
		trace := pdfTraceOf(t, withHistory(t, "---\ntitle: Backup\n"+body, testHistory), Options{})
		indexIn(t, trace, heading+` "Version history"`)
		indexIn(t, trace, `"Author"`)
	})

	t.Run("on the page after the cover", func(t *testing.T) {
		trace := pdfTraceOf(t, withHistory(t, front+"cover: true\n"+body, testHistory), Options{Lang: "hu"})
		page1, page2 := onPage(trace, 1), onPage(trace, 2)
		if strings.Contains(page1, "Változástörténet") || strings.Contains(page1, "Béla Nagy") {
			t.Errorf("the history is on the cover:\n%s", page1)
		}
		if title, text := indexIn(t, page2, `"Változástörténet"`), indexIn(t, page2, `"body"`); title > text {
			t.Errorf("the title is after the text:\n%s", page2)
		}
		// It starts the page
		if first := strings.SplitN(page2, "\n", 2)[0]; !strings.Contains(first, "Változástörténet") {
			t.Errorf("the title does not start page 2: %s", first)
		}
	})
}

func TestDOCXVersionHistory(t *testing.T) {
	const front = "---\ntitle: Backup\nlang: hu\nversion: 1.1\nhistory: git\n"
	const body = "---\n\n# Szabály\n\nbody text\n"
	render := func(t *testing.T, md string, lang string) string {
		t.Helper()
		out := filepath.Join(t.TempDir(), "out.docx")
		if err := RenderDocx(withHistory(t, md, testHistory), out, Options{Lang: lang}); err != nil {
			t.Fatal(err)
		}
		return docxPart(t, out, "word/document.xml")
	}

	t.Run("after the control block", func(t *testing.T) {
		doc := render(t, front+body, "hu")
		block := indexIn(t, doc, ">Backup<")
		title := indexIn(t, doc, ">Változástörténet<")
		header := title + indexIn(t, doc[title:], ">Verzió<") // the block has the word too
		row := indexIn(t, doc, ">A mentések megőrzési ideje 90 nap.<")
		text := indexIn(t, doc, ">body<")
		if !(block < title && title < header && header < row && row < text) {
			t.Errorf("out of order: block %d, title %d, header %d, row %d, text %d", block, title, header, row, text)
		}
		for _, want := range []string{">Dátum<", ">Szerző<", ">Változás<", ">2026-03-15<", ">Béla Nagy<", ">Első változat.<"} {
			indexIn(t, doc, want)
		}
		for _, not := range []string{">history<", ">git<"} {
			if strings.Contains(doc, not) {
				t.Errorf("the document has %s", not)
			}
		}
		// The title is a plain bold line: no heading style puts it in the
		// navigation pane. The control block and the history are two tables
		// of 2 and 4 columns.
		paragraph := doc[strings.LastIndex(doc[:title], "<w:p>"):title]
		if strings.Contains(paragraph, "pStyle") {
			t.Errorf("the title has a style: %s", paragraph)
		}
		if n := strings.Count(doc, "<w:gridCol "); n != 2+4 {
			t.Errorf("%d table columns, want 6", n)
		}
	})

	t.Run("English", func(t *testing.T) {
		doc := render(t, "---\ntitle: Backup\n"+body, "")
		indexIn(t, doc, ">Version history<")
		indexIn(t, doc, ">Author<")
	})

	t.Run("on the page after the cover", func(t *testing.T) {
		doc := render(t, front+"cover: true\n"+body, "hu")
		cover := indexIn(t, doc, `<w:pStyle w:val="Title">`)
		page := indexIn(t, doc, "<w:pageBreakBefore/>")
		title := indexIn(t, doc, ">Változástörténet<")
		text := indexIn(t, doc, ">body<")
		if !(cover < page && page < title && title < text) {
			t.Errorf("out of order: cover %d, page break %d, title %d, text %d", cover, page, title, text)
		}
	})
}

// A history is no row of the control block whatever its value.
func TestHistoryIsNoControlField(t *testing.T) {
	fm := ast.FrontMatter{Raw: map[string]any{"title": "T", "history": "git", "History": "svn"}}
	for _, f := range controlFields(fm, "", theme.ControlLabels{}) {
		if strings.EqualFold(f.label, "history") {
			t.Errorf("history is a row: %+v", f)
		}
	}
}
