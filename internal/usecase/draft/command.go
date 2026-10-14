package draft

import (
	"context"

	"schemo/internal/command"
	"schemo/internal/domain"
)

// PasteCommand applies the parse and the draft here rather than by the window, which would
// otherwise take the request apart only to hand the pieces straight back. The reading comes back
// either way: the window has a sentence for a command it could not read.
func (u *UseCase) PasteCommand(ctx context.Context, id domain.DraftID, text string) (Paste, error) {
	reading := command.Parse(text)
	if reading.Kind != command.KindOK {
		return Paste{Reading: reading}, nil
	}
	state, err := u.Replace(ctx, id, seedOf(reading.Request))
	if err != nil {
		return Paste{Reading: reading}, err
	}
	return Paste{Reading: reading, State: &state}, nil
}

// seedOf is what a command came to, as the draft takes it: the same request, with the environment
// left to the window — a command line was never pinned to one.
func seedOf(req command.Request) Seed {
	return Seed{
		Method:   req.Method,
		URL:      req.URL,
		Headers:  req.Headers,
		Body:     req.Body,
		BodyKind: req.BodyKind,
		Form:     req.Form,
		BodyFile: req.BodyFile,
		Auth:     req.Auth,
	}
}
