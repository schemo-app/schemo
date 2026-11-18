package convert

// Exporting collections. The writers receive the environments owned by the collection.

import (
	"context"
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

	var data []byte
	switch format {
	case FormatSchemo:
		data, err = schemofile.Write(domain.Document{Collections: []domain.Collection{collection}}, false)
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
		data, err = insomnia.Export(collection, collection.Environments, false)
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
