package draft

import (
	"context"

	"schemo/internal/domain"
)

// jarCookies is the cookie header this request goes out with: what the jar holds for its address,
// and nothing else — a request has no cookies of its own, and the one place they come from is the
// jar.
//
// The address is resolved first, because a request's host is usually a `{{token}}` — `{{baseUrl}}`
// is what a collection is for — and a jar looked up under the name of a variable would answer
// nothing and say nothing about why. What resolves it is the environment and the globals, which is
// the same answer the window's preview is given: the levels a collection answers for are the
// caller's to know, and the one place that has them both is the send.
func (u *UseCase) jarCookies(ctx context.Context, draft domain.Draft) (string, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}

	raw := collect(draft, "").url
	if answers, err := u.vars.SubstituteTexts(ctx, nil, []string{raw}, false,
		draft.EnvironmentID); err == nil && len(answers) == 1 {
		raw = answers[0]
	}
	return u.jar.CookieHeader(ctx, workspace, raw)
}
