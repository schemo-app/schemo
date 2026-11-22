package convert

// Exporting collections. The writers are pure format packages; this file picks the environments and
// is the one place a secret value may leave.

import (
	"context"
	"slices"
	"strings"

	"schemo/internal/convert/insomnia"
	"schemo/internal/convert/openapi"
	"schemo/internal/convert/postman"
	"schemo/internal/convert/schemofile"
	"schemo/internal/domain"
)

// export writes a collection — or one request, when the id names one — as the format the person
// chose, and answers with the bytes and the name the save dialog should open on.
func (s *Transfer) export(
	ctx context.Context,
	id string,
	format Format,
	secrets bool,
) (Export, error) {
	collection, err := s.collections.Full(ctx, id)
	if err != nil {
		return Export{}, err
	}
	switch format {
	case FormatPostman, FormatOpenAPI, FormatInsomnia:
		if err := checkExportProtocol(collection, format); err != nil {
			return Export{}, err
		}
	}

	var environments []domain.EnvironmentDraft
	switch format {
	case FormatSchemo, FormatOpenAPI, FormatInsomnia:
		if environments, err = s.environmentsOf(ctx, collection, secrets); err != nil {
			return Export{}, err
		}
	}

	var data []byte
	switch format {
	case FormatSchemo:
		// The file carries this collection's blocks; values for every collection stay home.
		for index := range environments {
			environments[index].Variables = nil
		}
		data, err = schemofile.Write(domain.Document{
			Collections:  []domain.Collection{collection},
			Environments: environments,
		}, secrets)
	case FormatPostman:
		data, err = postman.Export(collection)
	case FormatOpenAPI:
		data, err = openapi.Export(collection, effective(environments))
	case FormatInsomnia:
		data, err = insomnia.Export(collection, effective(environments), secrets)
	default:
		// A command and a published page are not files this app writes: cURL is written by Curl and a
		// page by a publish, so asking for one here is the window saying the wrong thing.
		return Export{}, domain.Refuse(domain.CodeUnknownFormat, domain.ErrNotAllowed,
			domain.Args{"format": string(format)})
	}
	if err != nil {
		return Export{}, err
	}
	return Export{Data: data, Name: collection.Name}, nil
}

func checkExportProtocol(collection domain.Collection, format Format) error {
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			if err := checkExportProtocol(*entry.Collection, format); err != nil {
				return err
			}
			continue
		}
		if entry.Node != nil && entry.Node.Protocol != "" {
			return domain.Refuse(domain.CodeExportProtocol, domain.ErrNotAllowed, domain.Args{
				"format": strings.ToUpper(string(format)), "name": entry.Node.Name,
			})
		}
	}
	return nil
}

// environmentsOf is every environment with a block for the collection's top level: its values for
// all collections and the block (as block 0). Secret values are revealed only when asked for.
func (s *Transfer) environmentsOf(
	ctx context.Context,
	collection domain.Collection,
	secrets bool,
) ([]domain.EnvironmentDraft, error) {
	root, err := s.rootOf(ctx, collection)
	if err != nil || root == "" {
		return nil, err
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return nil, err
	}
	out := []domain.EnvironmentDraft{}
	for _, environment := range state.Environments {
		block := environment.Block(root)
		if len(block) == 0 {
			continue
		}
		shared, err := s.exported(ctx, environment.Vars, secrets)
		if err != nil {
			return nil, err
		}
		own, err := s.exported(ctx, block, secrets)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.EnvironmentDraft{
			Name:      environment.Name,
			Color:     environment.Color,
			Variables: shared,
			Blocks:    []domain.BlockDraft{{Variables: own}},
		})
	}
	return out, nil
}

// rootOf is the top-level collection an exported level or request sits in: blocks are keyed by it.
func (s *Transfer) rootOf(ctx context.Context, collection domain.Collection) (string, error) {
	id := collection.ID
	if id == "" && len(collection.Items) == 1 {
		id = collection.Items[0].CollectionID
	}
	if id == "" {
		return "", nil
	}
	workspaces, err := s.workspaces.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	tree, err := s.collections.TreeOf(ctx, workspaces.ActiveID)
	if err != nil {
		return "", err
	}
	for _, top := range tree {
		if holds(top, id) {
			return top.ID, nil
		}
	}
	return "", nil
}

func holds(level domain.Collection, id string) bool {
	return level.ID == id || slices.ContainsFunc(level.Children, func(child domain.Collection) bool {
		return holds(child, id)
	})
}

// exported copies variables for a file; a secret's value is read only when the person asked.
func (s *Transfer) exported(ctx context.Context, vars []domain.Variable, secrets bool) ([]domain.Variable, error) {
	out := make([]domain.Variable, 0, len(vars))
	for _, variable := range vars {
		item := domain.Variable{
			Name:     variable.Name,
			Kind:     variable.Kind,
			Enabled:  variable.Enabled,
			Position: variable.Position,
		}
		switch {
		case variable.Kind != domain.VariableSecret:
			item.Value = variable.Value
		case secrets && variable.HasValue:
			value, err := s.environments.Reveal(ctx, variable.ID)
			if err != nil {
				return nil, err
			}
			item.Value = value
		}
		out = append(out, item)
	}
	return out, nil
}

// effective is what each environment answers for the collection: its enabled values for all
// collections with the block's over them.
func effective(drafts []domain.EnvironmentDraft) []domain.EnvironmentDraft {
	out := make([]domain.EnvironmentDraft, 0, len(drafts))
	for _, draft := range drafts {
		vars := enabled(draft.Variables)
		for _, block := range draft.Blocks {
			vars = overlay(vars, enabled(block.Variables))
		}
		out = append(out, domain.EnvironmentDraft{Name: draft.Name, Color: draft.Color, Variables: vars})
	}
	return out
}

func enabled(vars []domain.Variable) []domain.Variable {
	return slices.DeleteFunc(slices.Clone(vars), func(v domain.Variable) bool { return !v.Enabled })
}

// overlay is base with top over it: a name top holds takes top's variable.
func overlay(base, top []domain.Variable) []domain.Variable {
	out := slices.Clone(base)
	for _, variable := range top {
		at := slices.IndexFunc(out, func(held domain.Variable) bool { return held.Name == variable.Name })
		if at < 0 {
			out = append(out, variable)
			continue
		}
		out[at] = variable
	}
	for index := range out {
		out[index].Position = index + 1
	}
	return out
}
