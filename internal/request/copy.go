package request

// Writing a command out of the draft, where internal/convert/command reads one back. A copy is as close to
// a send as it gets without being one: the same texts, the same authorization, the real values.

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// CopySaved writes a saved request as a command, with secret values or references as asked.
//
//wails:ignore
func (s *Drafts) CopySaved(
	ctx context.Context,
	from domain.ResolvedRequest,
	format command.Format,
	secrets bool,
) (Copy, error) {
	current := FromNode(from.Node)
	current.EnvironmentID = cmp.Or(current.EnvironmentID, from.EnvironmentID)
	cookie, err := s.jarCookies(ctx, current, from.Variables)
	if err != nil {
		return Copy{}, err
	}
	return s.renderCommand(ctx, current, from.Auth, from.Variables, format, cookie, secrets)
}

// renderCommand is the copy itself, with the jar already read.
func (s *Drafts) renderCommand(
	ctx context.Context,
	current domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
	format command.Format,
	cookie string,
	secrets bool,
) (Copy, error) {
	raw := partsOf(current, inherits, cookie)
	var live []string
	var err error
	if secrets {
		// One substitution and nothing masked: a command is the one thing that carries the real values.
		live, err = s.vars.SubstituteTexts(ctx, above, raw.texts(), false, current.EnvironmentID)
	} else {
		live, err = s.vars.ReferenceTexts(ctx, above, raw.texts(), current.EnvironmentID)
	}
	if err != nil {
		return Copy{}, err
	}
	resolved := raw.putBack(live)

	// The names a send would refuse over are the ones a copy reports.
	omitted, err := s.unresolved(ctx, current, above, raw, format)
	if err != nil {
		return Copy{}, err
	}

	rules := protocols[current.Protocol]
	if format != command.FormatURL && rules.commandError != nil {
		return Copy{}, rules.commandError
	}
	kind := requestBodyKind(current)
	var body, boundary string
	if format != command.FormatURL {
		if current.Protocol == "" {
			body, boundary, err = s.renderCopyBody(kind, resolved.body, current.BodyFile, resolved.form)
		} else {
			body, boundary, err = s.renderRequestBody(current, resolved, kind, "", !secrets)
		}
	}
	if err != nil {
		return Copy{}, err
	}

	// Project, never Materialize: a copy answers with what a scheme already holds and goes for no
	// token. A link carries no credential at all, so nothing is reported about one.
	projected, err := s.auth.Project(resolved.auth,
		authRequest(current.Protocol.HTTPMethod(current.Method), rules.httpURL(resolved.url), resolved.headers, body))
	if err != nil {
		return Copy{}, err
	}
	if format != command.FormatURL {
		if token := tokenOmission(resolved.auth, s.auth.Held(resolved.auth)); token.Kind != "" {
			omitted = append(omitted, token)
		}
		// A Digest credential cannot be written into a command; a row the user wrote is more precise.
		if projected.Digest != nil && !hasHeader(resolved.headers, "Authorization") {
			omitted = append(omitted, command.Omission{Kind: command.OmissionDigest})
		}
	}

	url, headers := withAuth(resolved.url, resolved.headers, projected)
	headers = copyHeaders(current, kind, boundary, withCookie(headers, resolved.cookie))

	out := command.Request{
		Method:   current.Protocol.HTTPMethod(current.Method),
		URL:      url,
		Headers:  headers,
		Body:     body,
		BodyKind: kind,
		BodyFile: current.BodyFile,
	}
	if kind == domain.BodyForm && formCarriesFile(resolved.form) {
		// The rows go over: only the renderer knows what its notation can do with a file.
		out.Form = resolved.form
	}

	text, byNotation := command.Render(format, out)
	return Copy{Text: text, Omitted: append(omitted, byNotation...)}, nil
}

// unresolved is the names nothing answers, as the omissions a copy reports. It asks about the text
// that is written, so a name the address never mentions does not stop the address being copied.
func (s *Drafts) unresolved(
	ctx context.Context,
	current domain.Draft,
	above []domain.Variable,
	raw parts,
	format command.Format,
) ([]command.Omission, error) {
	texts := raw.texts()
	if format == command.FormatURL {
		texts = []string{raw.url}
	}

	missing, err := s.vars.Missing(ctx, above, texts, current.EnvironmentID)
	if err != nil {
		return nil, err
	}
	out := make([]command.Omission, 0, len(missing))
	for _, name := range missing {
		out = append(out, command.Omission{Kind: command.OmissionVariable, Name: name})
	}
	return out, nil
}

// tokenOmission is a scheme that fetches its credential with none held.
func tokenOmission(auth domain.Auth, held domain.AuthToken) command.Omission {
	scheme, ok := domain.SchemeFor(auth.Type)
	if !ok || !scheme.Fetches || held.Held {
		return command.Omission{}
	}
	return command.Omission{Kind: command.OmissionToken}
}

// renderCopyBody is renderBody for a copy: a body carrying a file is handed back as it is, because
// no file is opened here. The boundary travels so the header and the body name the same one.
func (s *Drafts) renderCopyBody(
	kind domain.BodyKind,
	text string,
	file string,
	rows []domain.FormRow,
) (body string, boundary string, err error) {
	switch kind {
	case domain.BodyBinary:
		// The path, not the text: a body with no file chosen has nothing to write.
		return "", "", nil
	case domain.BodyForm:
		if formCarriesFile(rows) {
			return "", "", nil
		}
		// A form without a file is text like any other and is rendered as a send renders it, so the
		// header names the boundary the body was written with.
		return s.renderForm(rows, "", false)
	default:
		return text, "", nil
	}
}

// formCarriesFile is whether a form has a row only the file system can answer.
func formCarriesFile(rows []domain.FormRow) bool {
	return slices.ContainsFunc(rows, func(row domain.FormRow) bool {
		return row.Enabled && row.File && strings.TrimSpace(row.Name) != "" && row.Src != ""
	})
}

// copyHeaders is the header list a command is written with, in the send's own order. A form a
// notation builds itself declares no type: an empty boundary names nothing, and is not dropped.
func copyHeaders(
	draft domain.Draft,
	kind domain.BodyKind,
	boundary string,
	headers []domain.HeaderPair,
) []domain.HeaderPair {
	if kind == domain.BodyForm && boundary == "" {
		return headers
	}
	return requestHeaders(draft, kind, boundary, headers)
}
