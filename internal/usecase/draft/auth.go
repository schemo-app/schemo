package draft

import (
	"context"

	"schemo/internal/domain"
)

// SetAuth normalizes the answers as they arrive: a token left over from the scheme before it
// cannot travel as an answer nobody asked for.
func (u *UseCase) SetAuth(ctx context.Context, id domain.DraftID, auth domain.Auth) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		d.Auth = auth.Normalized()
		return nil
	})
}

// ObtainAuth changes nothing the user answered: what is dropped is what those answers come to now,
// not the answers themselves.
func (u *UseCase) ObtainAuth(ctx context.Context, id domain.DraftID) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		return u.auth.Obtain(ctx, d.Auth)
	})
}

// ForgetAuth changes nothing the user answered: what is dropped is what those answers come to now,
// not the answers themselves.
func (u *UseCase) ForgetAuth(ctx context.Context, id domain.DraftID) (State, error) {
	return u.result(ctx, id, func(d *domain.Draft) error {
		u.auth.Forget(d.Auth)
		return nil
	})
}

// Held takes the authorization in rather than walking for it here: a request inside a collection
// takes it from the tree, and a draft cannot walk one.
func (u *UseCase) Held(draft domain.Draft, inherits *domain.Auth) *domain.AuthToken {
	auth := authToApply(draft.Auth, inherits)
	scheme, ok := domain.SchemeFor(auth.Type)
	if !ok || !scheme.Fetches {
		return nil
	}
	token := u.auth.Held(auth)
	return &token
}

// Project is where whoever can walk a tree passes its answer; a request that stands on its own
// passes nothing, and `inherits` going unused is that case rather than a mistake.
func (u *UseCase) Project(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
) ([]domain.ProjectedRow, error) {
	return u.projected(ctx, draft, inherits)
}

// A row the authorization adds is drawn beside the ones a person wrote — that is where it ends up —
// and a written row under that name drops it, as sending does. Fields stay as typed, not resolved.
func (u *UseCase) projected(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
) ([]domain.ProjectedRow, error) {
	// What goes out is what this compares against: a row the user switched off competes with nothing,
	// so dropping the projection over it would hide a credential on its way; cookie text is left out.
	sent := collect(draft, "")

	auth := authToApply(draft.Auth, inherits)
	out, err := u.auth.Project(auth, authRequest(draft.Method, sent.url, nil, draft.Body))
	if err != nil {
		return nil, err
	}

	rows := []domain.ProjectedRow{}
	for _, pair := range out.Headers {
		if !hasHeader(sent.headers, pair.Name) {
			rows = append(rows, domain.ProjectedRow{
				Target: domain.RowHeaders, Name: pair.Name, Value: pair.Value, From: auth.Type,
			})
		}
	}
	for _, pair := range out.Query {
		if !hasParam(draft.URL, pair.Name) {
			rows = append(rows, domain.ProjectedRow{
				Target: domain.RowParams, Name: pair.Name, Value: pair.Value, From: auth.Type,
			})
		}
	}
	return rows, nil
}
