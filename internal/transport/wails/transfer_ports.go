package wails

// The two features the transfer use case borrows in its own shape: the environment list, whose
// import answers with the created row while the transfer wants the list, and the draft, whose
// copies are `draft.Copy` while the transfer wants the command's text. Both are wiring, which is
// why they live beside the composition root rather than in either feature.

import (
	"context"

	"schemo/internal/command"
	"schemo/internal/domain"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/transfer"
)

type environmentImport struct{ *environment.UseCase }

var _ transfer.Environments = environmentImport{}

func (e environmentImport) Import(
	ctx context.Context,
	draft domain.EnvironmentDraft,
) (domain.EnvState, error) {
	created, err := e.UseCase.Import(ctx, draft)
	if err != nil {
		return domain.EnvState{}, err
	}
	return created.EnvState, nil
}

type commandCopies struct{ drafts *draft.UseCase }

var _ transfer.Commands = commandCopies{}

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
	return draft.ReadCommands(text)
}
