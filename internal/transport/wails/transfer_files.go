package wails

// What the file dialogs offer per format: the kind of file, the patterns a picker filters by — a
// semicolon-separated list, which is how the framework reads several — and the ending a suggested
// name gets. The ending is what tells whoever is handed the file which tool it is for before it is
// opened, which is why it is longer than the kind.

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/usecase/transfer"
)

type fileKind struct {
	name    string
	pattern string
	suffix  string
}

func kindOf(format transfer.Format) fileKind {
	switch format {
	case transfer.FormatSchemo:
		return fileKind{
			name:    "Schemo collection",
			pattern: "*.schemo.json;*.json",
			suffix:  ".schemo.json",
		}
	case transfer.FormatPostman:
		return fileKind{
			name:    "Postman collection",
			pattern: "*.json",
			suffix:  ".postman_collection.json",
		}
	case transfer.FormatOpenAPI:
		return fileKind{
			name:    "OpenAPI document",
			pattern: "*.yaml;*.yml;*.json",
			suffix:  ".openapi.yaml",
		}
	case transfer.FormatInsomnia:
		return fileKind{
			name:    "Insomnia export",
			pattern: "*.json",
			suffix:  ".insomnia.json",
		}
	case transfer.FormatHAR:
		return fileKind{name: "HAR file", pattern: "*.har", suffix: ".har"}
	}
	// A paste of commands is not a file, and a dialog for it is never asked for: this is the shape
	// of an answer, not a format anybody picked.
	return fileKind{name: "File", pattern: "*", suffix: ""}
}

func filterOf(format transfer.Format) []application.FileFilter {
	kind := kindOf(format)
	return []application.FileFilter{{DisplayName: kind.name, Pattern: kind.pattern}}
}
