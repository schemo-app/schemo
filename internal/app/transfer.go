package app

// Adapters giving Transfer the environment list and command copies in the shape it needs.

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
