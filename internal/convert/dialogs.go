package convert

// The dialogs a window cannot open for itself, and the writing of an exported file. What the file
// dialogs offer per format: the kind of file, the patterns a picker filters by (a semicolon-separated
// list, which is how the framework reads several) and the ending a suggested name gets. The ending
// tells whoever is handed the file which tool it is for before it is opened.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/desktop"
)

// PreviewFile asks for a file and reads it without writing anything: the dialog shows what the file
// holds before the person commits. A nil preview is a cancelled dialog, not a failure.
func (s *Transfer) PreviewFile(
	ctx context.Context,
	title string,
	format string,
) (*Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return nil, err
	}
	path, err := s.host.OpenFile(title, filterOf(parsed)...)
	if err != nil || path == "" {
		return nil, err
	}
	preview, err := s.previewFile(ctx, parsed, path)
	if err != nil {
		return nil, err
	}
	preview.File = filepath.Base(path)
	return &preview, nil
}

// PreviewPath reads a dropped file with the same limits and preview as a picked file.
func (s *Transfer) PreviewPath(ctx context.Context, format string, path string) (Preview, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return Preview{}, err
	}
	preview, err := s.previewFile(ctx, parsed, path)
	if err != nil {
		return Preview{}, err
	}
	preview.File = filepath.Base(path)
	return preview, nil
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

// TakeImportLink reports and clears the page a followed link named. Destructive on purpose: the
// window's mount and its event handler both call it and only the first may open the dialog; the
// link is usually what started this app, so the mount usually finds it.
func (s *Transfer) TakeImportLink() string {
	return s.host.TakeImportLink()
}

// Export writes a collection into a file the user picks, with the format's own ending on the
// suggested name, and returns the name the file was written under. An empty name is a cancelled
// dialog, not an error.
func (s *Transfer) Export(
	ctx context.Context,
	title string,
	id string,
	format string,
	secrets bool,
) (string, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.export(ctx, id, parsed, secrets)
	if err != nil {
		return "", err
	}

	kind := kindOf(parsed)
	path, err := s.host.SaveFile(title,
		desktop.SafeFileName(out.Name, "collection", kind.suffix), filterOf(parsed)...)
	if err != nil || path == "" {
		return "", err
	}
	// 0644 and not the database's 0600: the file is meant to be handed to somebody else. When the
	// person asked for the secret values, they are in it — and the window has said so.
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
	secrets bool,
) (string, error) {
	parsed, err := FormatOf(format)
	if err != nil {
		return "", err
	}
	out, err := s.export(ctx, id, parsed, secrets)
	if err != nil {
		return "", err
	}
	return string(out.Data), nil
}

type fileKind struct {
	name    string
	pattern string
	suffix  string
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
			name:    "Insomnia export",
			pattern: "*.json",
			suffix:  ".insomnia.json",
		}
	case FormatHAR:
		return fileKind{name: "HAR file", pattern: "*.har", suffix: ".har"}
	}
	// A paste of commands is not a file, and a dialog for it is never asked for: this is the shape
	// of an answer, not a format anybody picked.
	return fileKind{name: "File", pattern: "*", suffix: ""}
}

func filterOf(format Format) []application.FileFilter {
	kind := kindOf(format)
	return []application.FileFilter{{DisplayName: kind.name, Pattern: kind.pattern}}
}
