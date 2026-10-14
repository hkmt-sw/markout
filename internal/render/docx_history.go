package render

import "github.com/hkmt-sw/markout/internal/ast"

// renderVersionHistory sets the version history: a title and a table.
func (r *DocxRenderer) renderVersionHistory(h ast.VersionHistory) error {
	title, table := historyTable(h, r.opts.Lang)
	if err := r.renderListTitle(title); err != nil {
		return err
	}
	return r.renderTable(table)
}
