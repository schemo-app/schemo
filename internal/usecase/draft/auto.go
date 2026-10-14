package draft

import (
	"context"
	"net/url"
	"strconv"

	"schemo/internal/domain"
)

// Auto is the rows the application itself puts on the wire: the address's Host, the name it calls
// itself, the compression it asks for, the jar's cookies, and the body's length. The editor draws
// them beside the rows a person wrote — `domain.Requests.ShowAuto` is the window's choice of
// whether to — and the values are the wire's own: the address as the engine completes it, the
// cookie header as the jar answers it, the length of the body as a send renders it.
func (u *UseCase) Auto(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
) ([]domain.ProjectedRow, error) {
	cookie, err := u.jarCookies(ctx, draft)
	if err != nil {
		return nil, err
	}

	raw := partsOf(draft, inherits, cookie)
	hidden, err := u.vars.SubstituteTexts(ctx, above, raw.texts(), true, draft.EnvironmentID)
	if err != nil {
		return nil, err
	}
	masked := raw.putBack(hidden)

	rows := []domain.ProjectedRow{}
	add := func(name, value string) {
		rows = append(rows, domain.ProjectedRow{Target: domain.RowHeaders, Name: name, Value: value})
	}
	if host := hostOf(u.net.CompleteURL(masked.url)); host != "" {
		add("Host", host)
	}
	agent, compression := u.net.WireDefaults()
	if agent != "" {
		add("User-Agent", agent)
	}
	if compression {
		add("Accept-Encoding", "gzip")
	}
	if cookie != "" {
		add("Cookie", cookie)
	}
	if length, ok := sentLength(ctx, u, raw, above, draft); ok {
		add("Content-Length", strconv.Itoa(length))
	}
	return rows, nil
}

// sentLength is the length of the body as it goes out, for the bodies whose whole text is known
// here: a binary body and a form carrying files are read only by a send, and a row saying otherwise
// would be a guess.
func sentLength(
	ctx context.Context,
	u *UseCase,
	raw parts,
	above []domain.Variable,
	draft domain.Draft,
) (int, bool) {
	kind := domain.KindOf(draft.BodyKind)
	if kind == domain.BodyBinary || formCarriesFile(raw.form) {
		return 0, false
	}
	live, err := u.vars.SubstituteTexts(ctx, above, raw.texts(), false, draft.EnvironmentID)
	if err != nil {
		return 0, false
	}
	sent := raw.putBack(live)
	// A form without a file is text as it stands: rendering it opens nothing, and the boundary it
	// mints is as long as the one a send would write.
	body, _, err := u.renderBody(kind, sent.body, draft.BodyFile, sent.form, "", false)
	if err != nil || body == "" {
		return 0, false
	}
	return len(body), true
}

// hostOf is the host an address names, which is empty for one that names none.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
