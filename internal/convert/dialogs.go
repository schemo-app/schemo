package convert

// File dialogs per format: the description, the picker patterns (semicolon-separated, as the
// framework reads them) and the suggested extension.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/desktop"
)

// SaveCSV writes the table selection from the window, including its column order and filters.
// A cancelled save dialog returns false without writing a file.
func (s *Transfer) SaveCSV(title string, columns []string, rows [][]string) (bool, error) {
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	writer.UseCRLF = true
	if err := writer.Write(columns); err != nil {
		return false, err
	}
	if err := writer.WriteAll(rows); err != nil {
		return false, err
	}
	path, err := s.host.SaveFile(title, "response.csv",
		application.FileFilter{DisplayName: "CSV", Pattern: "*.csv"})
	if err != nil || path == "" {
		return false, err
	}
	if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
		return false, fmt.Errorf("file %s: %w", path, err)
	}
	return true, nil
}

// PickImport opens the system picker for a local import. A nil preview means cancellation.
func (s *Transfer) PickImport(
	ctx context.Context,
	format, title string,
) (*Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return nil, err
	}
	kind := kindOf(parsed)
	path, err := s.host.OpenPath(title, kind.folders, application.FileFilter{
		DisplayName: kind.name,
		Pattern:     kind.importPattern(),
	})
	if err != nil || path == "" {
		return nil, err
	}
	preview, err := s.previewFile(ctx, parsed, path)
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

// PreviewPath reads a dropped file or folder the same way as a picked path.
func (s *Transfer) PreviewPath(ctx context.Context, format string, path string) (Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return Preview{}, err
	}
	return s.previewFile(ctx, parsed, path)
}

// PreviewText reads a text the window already holds, such as pasted commands, and names it the way
// the card will draw it.
func (s *Transfer) PreviewText(
	ctx context.Context,
	format string,
	text string,
	fileName string,
) (Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return Preview{}, err
	}
	preview, err := s.previewText(ctx, parsed, text)
	if err != nil {
		return Preview{}, err
	}
	preview.File = fileName
	return preview, nil
}

// PreviewUrl reads a document from a URL — a specification or a collection somebody published.
func (s *Transfer) PreviewUrl(
	ctx context.Context,
	format string,
	url string,
) (Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return Preview{}, err
	}
	preview, err := s.previewUrl(ctx, parsed, url)
	if err != nil {
		return Preview{}, err
	}
	preview.File = url
	return preview, nil
}

// TakeImportLink returns and clears the pending page link, so only the first of mount and event opens
// the dialog.
func (s *Transfer) TakeImportLink() string {
	return s.host.TakeImportLink()
}

// Export writes a collection to a file the user picks and returns its name; empty means cancelled.
func (s *Transfer) Export(
	ctx context.Context,
	title string,
	id string,
	format string,
) (string, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.export(ctx, id, parsed)
	if err != nil {
		return "", err
	}

	kind := kindOf(parsed)
	path, err := s.host.SaveFile(title,
		desktop.SafeFileName(out.Name, "collection", kind.suffix), filterOf(parsed)...)
	if err != nil || path == "" {
		return "", err
	}
	// The file is meant to be shared, so the collection writer leaves secret variables out.
	if err := os.WriteFile(path, out.Data, 0o644); err != nil {
		return "", fmt.Errorf("file %s: %w", path, err)
	}
	return filepath.Base(path), nil
}

// ExportText is the same export as the text the window copies: a collection somebody is handed in a
// chat is handed as its text.
func (s *Transfer) ExportText(
	ctx context.Context,
	id string,
	format string,
) (string, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.export(ctx, id, parsed)
	if err != nil {
		return "", err
	}
	return string(out.Data), nil
}

type fileKind struct {
	name          string
	pattern       string
	importFormats string
	suffix        string
	folders       bool
}

func (kind fileKind) importPattern() string {
	return cmp.Or(kind.importFormats, kind.pattern)
}

func kindOf(format Format) fileKind {
	switch format {
	case FormatSchemo:
		return fileKind{
			name:    "Schemo collection",
			pattern: "*.schemo.json;*.json",
			suffix:  ".schemo.json",
		}
	case FormatPostman:
		return fileKind{
			name:    "Postman collection",
			pattern: "*.json",
			suffix:  ".postman_collection.json",
		}
	case FormatOpenAPI:
		return fileKind{
			name:    "OpenAPI document",
			pattern: "*.yaml;*.yml;*.json",
			suffix:  ".openapi.yaml",
		}
	case FormatInsomnia:
		return fileKind{
			name:          "Insomnia export",
			pattern:       "*.json",
			importFormats: "*.json;*.yaml;*.yml",
			suffix:        ".insomnia.json",
			folders:       true,
		}
	case FormatHAR:
		return fileKind{name: "HAR file", pattern: "*.har", suffix: ".har"}
	}
	// A paste of commands is not a file, and a dialog for it is never asked for: this is the shape
	// of an answer, not a format anybody picked.
	return fileKind{name: "File", pattern: "*"}
}

func filterOf(format Format) []application.FileFilter {
	kind := kindOf(format)
	return []application.FileFilter{{DisplayName: kind.name, Pattern: kind.pattern}}
}
