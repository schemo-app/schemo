package transfer

// Reading a document and counting what it holds. The readers themselves are pure packages beside
// the formats they know — this is only the dispatch, and the walk the counters need.

import (
	"schemo/internal/domain"
	"schemo/internal/har"
	"schemo/internal/insomnia"
	"schemo/internal/openapi"
	"schemo/internal/postman"
	"schemo/internal/schemofile"
	"schemo/internal/usecase/publication"
)

// read hands the bytes to the reader of the format the source named, and a paste of commands to the
// draft feature — a command is a request in another notation, and that notation is its own.
func (u *UseCase) read(format Format, data []byte) (domain.Document, error) {
	switch format {
	case FormatSchemo:
		return schemofile.Read(data)
	case FormatPage:
		return publication.AsDocument(data)
	case FormatPostman:
		return postman.Read(data)
	case FormatOpenAPI:
		return openapi.Import(data)
	case FormatInsomnia:
		return insomnia.Import(data)
	case FormatHAR:
		return har.Import(data)
	case FormatCurl:
		nodes, err := u.commands.Read(string(data))
		if err != nil {
			return domain.Document{}, err
		}
		return domain.Document{
			Collections: []domain.Collection{{Items: nodes}},
		}, nil
	}
	return domain.Document{}, domain.Refuse(domain.CodeUnknownFormat, domain.ErrNotAllowed,
		domain.Args{"format": string(format)})
}

// countDocument is the size of what was read: requests and folders across every collection of the
// document, the way the dialog counts them before the person commits.
func countDocument(document domain.Document) (requests, folders int) {
	for _, collection := range document.Collections {
		r, f := countCollection(collection)
		requests += r
		folders += f
	}
	return requests, folders
}

// countCollection walks a subtree: a folder is counted and so is everything inside it, because the
// picker says how much a copy would take and a folder is part of what is taken.
func countCollection(collection domain.Collection) (requests, folders int) {
	requests = len(collection.Items)
	for _, child := range collection.Children {
		nestedRequests, nestedFolders := countCollection(child)
		requests += nestedRequests
		folders += nestedFolders + 1
	}
	return requests, folders
}
