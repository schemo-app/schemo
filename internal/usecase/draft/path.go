package draft

import "schemo/internal/domain"

// followURL makes the rows an address implies agree with it: the query rows and the path rows. A
// row that stays keeps its id, and a path row its value, so an open editor keeps its target.
func (u *UseCase) followURL(d *domain.Draft) {
	d.Params = u.rowsFromURL(d.URL, d.Params)
	d.PathParams = u.pathRowsFromURL(d.URL, d.PathParams)
}

// pathRowsFromURL is one row for each `:name` of the address, in the order it writes them. The
// value and the description stay with the name: a row the address no longer mentions is gone with
// them, since the name is the address's and a row without it has nothing to be a value of.
func (u *UseCase) pathRowsFromURL(address string, existing []domain.Row) []domain.Row {
	names := domain.PathParams(address)
	rows := make([]domain.Row, 0, len(names))
	for _, name := range names {
		row := domain.Row{Name: name, Enabled: true}
		for _, old := range existing {
			if old.Name == name {
				row = old
				break
			}
		}
		if row.ID == "" {
			row.ID = u.ids()
		}
		rows = append(rows, row)
	}
	return rows
}

// applyPathPatch changes what a path row owns, its value and its description. Its name is the
// address's, and a path row is always on: the patch's other fields have nothing to say here.
func applyPathPatch(row *domain.Row, patch RowPatch) {
	if patch.Value != nil {
		row.Value = *patch.Value
	}
	if patch.Description != nil {
		row.Description = *patch.Description
	}
}
