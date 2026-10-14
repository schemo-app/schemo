package draft

import (
	"context"

	"schemo/internal/domain"
)

// Replace hands the draft a whole request. Everything the draft held is dropped, including the
// parameters the old URL carried and the choice made in the Auth chip: a seed is a request, not an
// edit to one.
func (u *UseCase) Replace(ctx context.Context, id domain.DraftID, seed Seed) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		d.Method = seed.Method
		d.URL = seed.URL
		d.Body = seed.Body
		d.BodyKind = domain.KindOf(seed.BodyKind)
		d.Form = u.formWithIDs(seed.Form)
		d.BodyFile = seed.BodyFile
		d.Auth = authOf(seed)
		d.Params = u.rowsFromURL(seed.URL, nil)
		d.PathParams = u.pathRowsFromURL(seed.URL, nil)
		d.Headers = u.rowsFromHeaders(seed.Headers)
		return nil
	})
}

// formWithIDs is withRowIDs for a form body: a row the window cannot address is a row it cannot
// edit.
func (u *UseCase) formWithIDs(rows []domain.FormRow) []domain.FormRow {
	out := make([]domain.FormRow, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			row.ID = u.ids()
		}
		out = append(out, row)
	}
	return out
}

// A seed that says nothing about authorization is not one that forgot, it is one with none. It is
// normalized like one the window sent: a seed comes from outside, and a scheme that arrived
// without its starting answers would be drawn with a blank where its first choice belongs.
func authOf(seed Seed) domain.Auth {
	if seed.Auth == nil {
		return domain.NewAuth(domain.AuthNone)
	}
	return seed.Auth.Normalized()
}

// Prepared is the draft the window is editing, ready to go out. inherits is what its «Inherit»
// resolves to and above is what the collections around it answer for its tokens, and the caller is
// the one that knows both: the draft holds what was typed, and where in a tree the request sits is
// not part of that. Both are nil for a draft with nothing above it, which is the command line's.
func (u *UseCase) Prepared(
	ctx context.Context,
	id domain.DraftID,
	inherits *domain.Auth,
	above []domain.Variable,
) (Prepared, error) {
	draft, err := u.Current(ctx, id)
	if err != nil {
		return Prepared{}, err
	}
	// The jar is read once and carried into both: what a request sends is decided by the address it
	// is going to, and asking twice would be asking about the same thing a moment apart.
	cookie, err := u.jarCookies(ctx, draft)
	if err != nil {
		return Prepared{}, err
	}
	return u.prepare(ctx, draft, inherits, above, cookie)
}

// Prepare fills in a request that is not the one being composed — a followed link, a collection run
// — without disturbing any draft. Resolving and masking live here and not with the caller, so there
// is one answer to what a request looks like when it leaves.
func (u *UseCase) Prepare(
	ctx context.Context,
	seed Seed,
	above []domain.Variable,
) (Prepared, error) {
	draft := u.draftOfSeed(seed)
	cookie, err := u.jarCookies(ctx, draft)
	if err != nil {
		return Prepared{}, err
	}
	return u.prepare(ctx, draft, nil, above, cookie)
}

// Mask re-renders a request a script handed back: it has been resolved once already, and what is
// asked of it here is only the copy that is written down, with a secret left as its mask. It is not
// Prepare for that reason — a request that has been filled in has no level over it left to answer,
// and reading its braces again would refuse a request that is already on the wire over text a value
// of its own happened to carry.
//
// The jar is not asked either: the request a script handed back carries the `Cookie` header it went
// out with, and sending that through the jar again would answer a question that has been answered.
func (u *UseCase) Mask(ctx context.Context, seed Seed) (Prepared, error) {
	return u.maskOf(ctx, u.draftOfSeed(seed))
}

// draftOfSeed is a seed seen as a draft: what a request from outside looks like once it is being
// handled like one the window typed.
func (u *UseCase) draftOfSeed(seed Seed) domain.Draft {
	draft := domain.Draft{
		Method:        seed.Method,
		URL:           seed.URL,
		Body:          seed.Body,
		BodyKind:      domain.KindOf(seed.BodyKind),
		Form:          u.formWithIDs(seed.Form),
		BodyFile:      seed.BodyFile,
		Headers:       u.rowsFromHeaders(seed.Headers),
		Auth:          authOf(seed),
		EnvironmentID: seed.EnvironmentID,
	}
	return draft
}

// withRowIDs gives every row an id. A draft that came from somewhere else — a saved request — has
// rows without them, and the window addresses a row by id: one that has none could not be edited.
func (u *UseCase) withRowIDs(d domain.Draft) domain.Draft {
	for i := range d.Params {
		if d.Params[i].ID == "" {
			d.Params[i].ID = u.ids()
		}
	}
	for i := range d.Headers {
		if d.Headers[i].ID == "" {
			d.Headers[i].ID = u.ids()
		}
	}
	for i := range d.PathParams {
		if d.PathParams[i].ID == "" {
			d.PathParams[i].ID = u.ids()
		}
	}
	for i := range d.Form {
		if d.Form[i].ID == "" {
			d.Form[i].ID = u.ids()
		}
	}
	return d
}

// rowsFromHeaders turns the headers of a seed into rows. A `Cookie` header is kept: the jar is
// derived from it, and it is the collect step — not this one — that decides which of the two goes
// out when a seed brought both.
func (u *UseCase) rowsFromHeaders(headers []domain.HeaderPair) []domain.Row {
	rows := []domain.Row{}
	for _, header := range headers {
		rows = append(rows,
			domain.Row{ID: u.ids(), Name: header.Name, Value: header.Value, Enabled: true})
	}
	return rows
}
