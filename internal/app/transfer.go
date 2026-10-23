package app

// The two features Transfer borrows in its own shape: the environment list, whose
// import answers with the created row while the transfer wants the list, and the draft, whose
// copies are `request.Copy` while the transfer wants the command's text. Both are wiring, so they
// live beside the composition root rather than in either feature.

import (
	"context"

	"schemo/internal/convert"
	"schemo/internal/convert/command"
	"schemo/internal/domain"
	"schemo/internal/request"
)

type commandCopies struct{ drafts *request.Drafts }

var _ convert.Commands = commandCopies{}

func (c commandCopies) Curl(
	ctx context.Context,
	request domain.ResolvedRequest,
	secrets bool,
) (string, error) {
	// A file rather than a terminal: one option per line, so a command with nine headers is still
	// read by a person.
	copied, err := c.drafts.CopySaved(ctx, request, command.FormatCurlLines, secrets)
	if err != nil {
		return "", err
	}
	return copied.Text, nil
}

func (c commandCopies) Read(text string) ([]domain.CollectionNode, error) {
	return request.ReadCommands(text)
}
