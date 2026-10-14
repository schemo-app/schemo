package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/record"
)

// requestMask is a request seen the way everything that outlives the send sees it: with a secret's
// value left as its mask. A pre-request script may have changed the request, and masking that text
// happens in the feature that owns the values — so the composition asks it again, once, for the
// request that actually goes out.
type requestMask struct {
	drafts *draft.UseCase
	values *environment.UseCase
}

var _ record.Masker = requestMask{}

// Mask re-renders a request a script changed. The URL and the headers come from the script's
// answer, because those are what it rewrote; the body is rendered again from what the prepared
// attempt is made of, because a form and a file are not text a script could have handed back.
func (m requestMask) Mask(
	ctx context.Context,
	in record.SendInput,
	sent domain.ScriptRequest,
) (record.Masked, error) {
	// What a script handed back has already been substituted — it is a rewrite of the request that
	// went out — so there is no level above it left to answer, and nothing here refuses: the request
	// has left already, and its braces are its own text.
	prepared, err := m.drafts.Mask(ctx, draft.Seed{
		Method:   sent.Method,
		URL:      sent.URL,
		Body:     in.Body,
		BodyKind: in.BodyKind,
		Form:     in.Form,
		BodyFile: in.BodyFile,
		Headers:  sent.Headers,
	})
	if err != nil {
		return record.Masked{}, err
	}
	return record.Masked{
		URL:     prepared.MaskedURL,
		Headers: prepared.MaskedHeaders,
		Body:    prepared.MaskedBody,
	}, nil
}

// MaskTexts masks one space's secret values wherever they sit in the texts. Nothing is rendered
// again here — a capture is a request nobody prepared in this app — so the values themselves are
// asked for, and the space is the one the record is filed under.
func (m requestMask) MaskTexts(
	ctx context.Context,
	workspace string,
	texts []string,
) ([]string, error) {
	return m.values.MaskTextsIn(ctx, workspace, nil, texts, "")
}
