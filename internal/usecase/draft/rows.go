package draft

// The row lists — parameters, headers, the form rows of a form body and the values of the path
// parameters — edited a row at a time. A row is addressed by its id, never by its index. A path
// row can only be patched: the address decides which ones exist. Cookies are not among them: what a
// request owns of a cookie is a yes or a no, which `SetCookieOff` is.

import (
	"context"
	"slices"

	"schemo/internal/domain"
)

// AddRow appends an empty row to one of the lists, and names the row it added: the window addresses
// a row by its id, and one worked out from a before-and-after is a guess about what else changed.
func (u *UseCase) AddRow(
	ctx context.Context,
	id domain.DraftID,
	kind domain.RowKind,
) (Added, error) {
	var added string
	state, err := u.result(ctx, id, func(d *domain.Draft) error {
		switch kind {
		case domain.RowParams:
			added = u.ids()
			d.Params = append(d.Params, domain.Row{ID: added, Enabled: true})
		case domain.RowHeaders:
			added = u.ids()
			d.Headers = append(d.Headers, domain.Row{ID: added, Enabled: true})
		case domain.RowForm:
			added = u.ids()
			d.Form = append(d.Form, domain.FormRow{ID: added, Enabled: true})
		default:
			return unknownKind(kind)
		}
		return nil
	})
	return Added{State: state, ID: added}, err
}

// RemoveRow does not treat a missing row as an error: a double click, or a patch arriving after
// its row is gone, has asked for exactly what it got.
//
// The row is rowID and the draft is id, because a row id and a draft id are different things in the
// same call: every other method on this use case names the draft the way this one does.
func (u *UseCase) RemoveRow(
	ctx context.Context,
	id domain.DraftID,
	kind domain.RowKind,
	rowID string,
) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		switch kind {
		case domain.RowParams:
			d.Params = without(d.Params, rowID)
			syncURL(d)
		case domain.RowHeaders:
			d.Headers = without(d.Headers, rowID)
		case domain.RowForm:
			d.Form = withoutForm(d.Form, rowID)
		default:
			return unknownKind(kind)
		}
		return nil
	})
}

// PatchRow writes the list back into the URL for a parameter edit: the URL is what goes out, and
// the rows are only how it is edited.
func (u *UseCase) PatchRow(
	ctx context.Context,
	id domain.DraftID,
	kind domain.RowKind,
	rowID string,
	patch RowPatch,
) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		switch kind {
		case domain.RowParams:
			at := findRow(d.Params, rowID)
			if at < 0 {
				return notFound(kind, rowID)
			}
			applyPatch(&d.Params[at], patch)
			syncURL(d)
		case domain.RowHeaders:
			at := findRow(d.Headers, rowID)
			if at < 0 {
				return notFound(kind, rowID)
			}
			applyPatch(&d.Headers[at], patch)
		case domain.RowForm:
			at := findFormRow(d.Form, rowID)
			if at < 0 {
				return notFound(kind, rowID)
			}
			applyFormPatch(&d.Form[at], patch)
		case domain.RowPathParams:
			at := findRow(d.PathParams, rowID)
			if at < 0 {
				return notFound(kind, rowID)
			}
			applyPathPatch(&d.PathParams[at], patch)
		default:
			return unknownKind(kind)
		}
		return nil
	})
}

// MoveRow puts a row at a position in its list. A parameter list that moved is written back into
// the URL — the address is what is sent, and the rows are only how it is edited.
func (u *UseCase) MoveRow(
	ctx context.Context,
	id domain.DraftID,
	kind domain.RowKind,
	rowID string,
	position int64,
) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		switch kind {
		case domain.RowParams:
			moved, ok := moved(d.Params, rowID, position, idOfRow)
			if !ok {
				return notFound(kind, rowID)
			}
			d.Params = moved
			syncURL(d)
		case domain.RowHeaders:
			moved, ok := moved(d.Headers, rowID, position, idOfRow)
			if !ok {
				return notFound(kind, rowID)
			}
			d.Headers = moved
		case domain.RowForm:
			moved, ok := moved(d.Form, rowID, position, idOfFormRow)
			if !ok {
				return notFound(kind, rowID)
			}
			d.Form = moved
		default:
			return unknownKind(kind)
		}
		return nil
	})
}

// moved is the list with the row named put at a position: taken out first, so the position counts
// in the list it lands in, and one past the end is the end.
func moved[T any](rows []T, id string, position int64, idOf func(T) string) ([]T, bool) {
	at := slices.IndexFunc(rows, func(row T) bool { return idOf(row) == id })
	if at < 0 {
		return nil, false
	}
	rest := slices.Delete(slices.Clone(rows), at, at+1)
	return slices.Insert(rest, min(max(int(position), 0), len(rest)), rows[at]), true
}

func idOfRow(row domain.Row) string         { return row.ID }
func idOfFormRow(row domain.FormRow) string { return row.ID }

func applyPatch(row *domain.Row, patch RowPatch) {
	if patch.Name != nil {
		row.Name = *patch.Name
	}
	if patch.Value != nil {
		row.Value = *patch.Value
	}
	if patch.Enabled != nil {
		row.Enabled = *patch.Enabled
	}
	if patch.Description != nil {
		row.Description = *patch.Description
	}
}

// without returns the rows without the one named — a new slice, so that a change that cannot be
// saved can still be rolled back to the one before it.
func without(rows []domain.Row, id string) []domain.Row {
	out := make([]domain.Row, 0, len(rows))
	for _, row := range rows {
		if row.ID != id {
			out = append(out, row)
		}
	}
	return out
}

func withoutForm(rows []domain.FormRow, id string) []domain.FormRow {
	out := make([]domain.FormRow, 0, len(rows))
	for _, row := range rows {
		if row.ID != id {
			out = append(out, row)
		}
	}
	return out
}

func findFormRow(rows []domain.FormRow, id string) int {
	for i, row := range rows {
		if row.ID == id {
			return i
		}
	}
	return -1
}

// applyFormPatch changes what the patch names and leaves the rest. Src and File are separate from
// Value on purpose: the paperclip switches a row between a text and a file, and switching back must
// find the text where it was left.
func applyFormPatch(row *domain.FormRow, patch RowPatch) {
	if patch.Name != nil {
		row.Name = *patch.Name
	}
	if patch.Value != nil {
		row.Value = *patch.Value
	}
	if patch.Enabled != nil {
		row.Enabled = *patch.Enabled
	}
	if patch.Src != nil {
		row.Src = *patch.Src
	}
	if patch.File != nil {
		row.File = *patch.File
	}
}

func findRow(rows []domain.Row, id string) int {
	for i, row := range rows {
		if row.ID == id {
			return i
		}
	}
	return -1
}

func unknownKind(kind domain.RowKind) error {
	return domain.Refuse(domain.CodeUnknownList, domain.ErrNotAllowed,
		domain.Args{"list": string(kind)})
}

func notFound(kind domain.RowKind, id string) error {
	return domain.Refuse(domain.CodeUnknownList, domain.ErrNotFound,
		domain.Args{"list": string(kind), "row": id})
}
