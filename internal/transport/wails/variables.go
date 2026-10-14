package wails

import (
	"context"

	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/environment"
)

type activeEnvironment struct {
	environments *environment.UseCase
}

var _ collection.Environment = activeEnvironment{}

func (a activeEnvironment) ActiveEnvironment(ctx context.Context) (string, error) {
	state, err := a.environments.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	for _, one := range state.Environments {
		if one.ID == state.ActiveID {
			return one.Name, nil
		}
	}
	return "", nil
}
