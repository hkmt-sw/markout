package render

import (
	"strings"

	"github.com/hkmt-sw/markout/internal/ast"
)

// The version history is a title and a table of the commits the document
// came from; see internal/history. Its labels follow the language of the
// document, as those of the control block do.

// historyLabels are the words of the version history in English and in
// Hungarian.
var historyLabels = struct{ title, version, date, author, change [2]string }{
	title:   [2]string{"Version history", "Változástörténet"},
	version: [2]string{"Version", "Verzió"},
	date:    [2]string{"Date", "Dátum"},
	author:  [2]string{"Author", "Szerző"},
	change:  [2]string{"Change", "Változás"},
}

// historyTable is the title of the version history and its table: a Version
// column only if some entry names a version, then Date, Author and Change.
func historyTable(h ast.VersionHistory, language string) (title string, table ast.Table) {
	pick := 0
	if language == "hu" || strings.HasPrefix(language, "hu-") {
		pick = 1
	}
	l := historyLabels
	headers := []string{l.date[pick], l.author[pick], l.change[pick]}
	if h.HasVersions() {
		headers = append([]string{l.version[pick]}, headers...)
	}
	for _, text := range headers {
		table.Header.Cells = append(table.Header.Cells, ast.NewTableCell(ast.NewInlineRun(text)))
		table.Alignments = append(table.Alignments, ast.AlignLeft)
	}
	for _, e := range h.Entries {
		// A name is kept on one line: its parts are joined by no-break spaces
		values := []string{e.Date, strings.Join(strings.Fields(e.Author), "\u00a0"), e.Change}
		if h.HasVersions() {
			values = append([]string{e.Version}, values...)
		}
		var row ast.TableRow
		for _, text := range values {
			row.Cells = append(row.Cells, ast.NewTableCell(ast.NewInlineRun(text)))
		}
		table.Rows = append(table.Rows, row)
	}
	return l.title[pick], table
}
