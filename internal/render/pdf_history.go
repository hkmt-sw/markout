package render

import "github.com/hkmt-sw/markout/internal/ast"

// renderVersionHistory draws the version history: a title and a table.
func (r *PdfRenderer) renderVersionHistory(h ast.VersionHistory) {
	title, table := historyTable(h, r.opts.Lang)
	// The title goes with the header of the table and its first row
	row := r.t.Table.LineHeight + 2*r.t.Table.CellPadding
	r.renderListTitle(title, 2*row)
	r.renderTable(table)
}
