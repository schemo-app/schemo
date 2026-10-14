package draft

import (
	"context"
	"strings"

	"schemo/internal/domain"
)

type parts struct {
	url     string
	body    string
	form    []domain.FormRow
	headers []domain.HeaderPair
	cookie  string
	auth    domain.Auth
}

func collect(d domain.Draft, cookie string) parts {
	// The path parameters are filled in before anything is resolved: a value may carry a `{{token}}`
	// of its own, and every reader of the address — the send, the preview, the copy — sees this one.
	out := parts{
		url: domain.FillPath(d.URL, d.PathParams), body: d.Body, form: d.Form, auth: d.Auth,
		cookie: cookie,
	}

	for _, row := range d.Headers {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		if out.cookie != "" && strings.EqualFold(row.Name, "Cookie") {
			continue
		}
		out.headers = append(out.headers, domain.HeaderPair{Name: row.Name, Value: row.Value})
	}
	return out
}

func (p parts) texts() []string {
	keys := domain.FieldKeys(p.auth.Type)
	out := make([]string, 0, 4+3*len(p.form)+2*len(p.headers)+len(keys))
	out = append(out, p.url, p.body)
	for _, row := range p.form {
		out = append(out, row.Name, row.Value, row.Src)
	}
	for _, header := range p.headers {
		out = append(out, header.Name, header.Value)
	}
	out = append(out, p.cookie)
	for _, key := range keys {
		out = append(out, p.auth.Answer(key))
	}
	return out
}

func (p parts) putBack(resolved []string) parts {
	at := 0
	p.url = resolved[at]
	at++
	p.body = resolved[at]
	at++

	form := make([]domain.FormRow, len(p.form))
	for i := range p.form {
		form[i] = p.form[i]
		form[i].Name, form[i].Value, form[i].Src = resolved[at], resolved[at+1], resolved[at+2]
		at += 3
	}
	p.form = form

	headers := make([]domain.HeaderPair, len(p.headers))
	for i := range p.headers {
		headers[i] = domain.HeaderPair{Name: resolved[at], Value: resolved[at+1]}
		at += 2
	}
	p.headers = headers
	p.cookie = resolved[at]
	at++

	fields := make(map[string]string, len(p.auth.Fields))
	for _, key := range domain.FieldKeys(p.auth.Type) {
		fields[key] = resolved[at]
		at++
	}
	p.auth = domain.Auth{Type: p.auth.Type, Fields: fields}
	return p
}

func (u *UseCase) Preview(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
) (Preview, error) {
	cookie, err := u.jarCookies(ctx, draft)
	if err != nil {
		return Preview{}, err
	}
	missing, err := u.vars.Missing(ctx, above, partsOf(draft, inherits, cookie).texts(),
		draft.EnvironmentID)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Missing: domain.OrEmpty(missing)}, nil
}

func (u *UseCase) preview(ctx context.Context, draft domain.Draft) (Preview, error) {
	return u.Preview(ctx, draft, nil, nil)
}
