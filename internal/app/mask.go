package app

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// requestMask masks the request that actually went out: a pre-request script may have changed it, so
// the environments feature is asked again.
type requestMask struct {
	drafts       *request.Drafts
	environments *workspace.Environments
}

var _ request.Masker = (*requestMask)(nil)

// Mask re-renders a script-changed request: URL and headers from the script, the body from the
// prepared attempt, since forms and files are not text a script returns.
func (m *requestMask) Mask(
	ctx context.Context,
	workspace string,
	in request.SendInput,
	sent domain.ScriptRequest,
) (request.Masked, error) {
	// Already substituted, so nothing is resolved or refused here.
	body := sent.Body
	if in.Protocol == "" && (in.BodyKind == domain.BodyForm || in.BodyKind == domain.BodyBinary) {
		body = in.Body
	}
	prepared, err := m.drafts.MaskIn(ctx, workspace, request.Seed{
		Method:        sent.Method,
		URL:           sent.URL,
		Body:          body,
		BodyKind:      in.BodyKind,
		Form:          in.Form,
		BodyFile:      in.BodyFile,
		Headers:       sent.Headers,
		EnvironmentID: in.EnvironmentID,
	}, in.Variables)
	if err != nil {
		return request.Masked{}, err
	}
	return request.Masked{
		URL:     prepared.MaskedURL,
		Headers: prepared.MaskedHeaders,
		Body:    prepared.MaskedBody,
	}, nil
}

// MaskTexts masks a workspace's secrets in raw texts, for captures nobody prepared in this app.
func (m *requestMask) MaskTexts(
	ctx context.Context,
	workspace string,
	texts []string,
) ([]string, error) {
	return m.environments.MaskTextsIn(ctx, workspace, nil, texts, "")
}
