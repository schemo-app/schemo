package convert

// Writing a collection out in one of the formats the export dialog offers. The writers are pure
// packages beside the readers of the same formats; what is here is the choice of environments and
// the one place a secret's value may travel.

import (
	"context"

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

	var data []byte
	switch format {
	case FormatSchemo:
		environments, err := s.environmentsOf(ctx, secrets)
		if err != nil {
			return Export{}, err
		}
		document := domain.Document{
			Collections:  []domain.Collection{collection},
			Environments: environments,
		}
		data, err = schemofile.Write(document, secrets)
		if err != nil {
			return Export{}, err
		}
	case FormatPostman:
		data, err = postman.Export(collection)
		if err != nil {
			return Export{}, err
		}
	case FormatOpenAPI:
		data, err = openapi.Export(collection)
		if err != nil {
			return Export{}, err
		}
	case FormatInsomnia:
		environments, err := s.environmentsOf(ctx, secrets)
		if err != nil {
			return Export{}, err
		}
		data, err = insomnia.Export(collection, environments, secrets)
		if err != nil {
			return Export{}, err
		}
	default:
		// A command and a published page are not files this app writes: cURL is written by Curl and a
		// page by a publish, so asking for one here is the window saying the wrong thing.
		return Export{}, domain.Refuse(domain.CodeUnknownFormat, domain.ErrNotAllowed,
			domain.Args{"format": string(format)})
	}
	return Export{Data: data, Name: collection.Name}, nil
}

// environmentsOf is the environment list as a file would carry it: secret values are read one by
// one (Reveal is the only way one leaves the database) and only when the person asked for them. A
// name nothing answers is left as it is, and the file says a value was there without carrying it.
func (s *Transfer) environmentsOf(
	ctx context.Context,
	secrets bool,
) ([]domain.EnvironmentDraft, error) {
	state, err := s.environments.State(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.EnvironmentDraft, 0, len(state.Environments))
	for _, environment := range state.Environments {
		draft := domain.EnvironmentDraft{Name: environment.Name, Color: environment.Color}
		for _, variable := range environment.Vars {
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
			draft.Variables = append(draft.Variables, item)
		}
		out = append(out, draft)
	}
	return out, nil
}
