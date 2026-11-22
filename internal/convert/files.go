package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"schemo/internal/convert/har"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

func (s *Transfer) previewFile(ctx context.Context, format Format, path string) (Preview, error) {
	document, size, err := s.readPath(ctx, format, path)
	if err != nil {
		return Preview{}, err
	}
	preview := s.stage(document, size)
	preview.File = filepath.Base(path)
	return preview, nil
}

func (s *Transfer) readPath(ctx context.Context, format Format, path string) (domain.Document, int, error) {
	kind := kindOf(format)
	if format == FormatHAR || kind.folders {
		info, err := os.Stat(path)
		if err != nil {
			return domain.Document{}, 0, platform.ReadError(path, err)
		}
		if kind.folders && info.IsDir() {
			return s.readFolder(ctx, format, path, kind.importPattern())
		}
		if !info.Mode().IsRegular() {
			return domain.Document{}, 0, platform.ReadError(path, errors.New("expected a regular file"))
		}
		if format == FormatHAR {
			document, err := readHAR(path)
			return document, int(info.Size()), err
		}
	}
	data, err := s.files.Read(path)
	if err != nil {
		return domain.Document{}, 0, err
	}
	document, err := s.read(format, data)
	return document, len(data), err
}

func readHAR(path string) (domain.Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.Document{}, platform.ReadError(path, err)
	}
	defer file.Close()
	return har.Read(file)
}

func (s *Transfer) readFolder(ctx context.Context, format Format, path, pattern string) (domain.Document, int, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return domain.Document{}, 0, platform.ReadError(path, err)
	}
	patterns := strings.Split(pattern, ";")
	var combined domain.Document
	var size int
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return domain.Document{}, 0, err
		}
		if !entry.Type().IsRegular() || !slices.ContainsFunc(patterns, func(pattern string) bool {
			match, _ := filepath.Match(pattern, strings.ToLower(entry.Name()))
			return match
		}) {
			continue
		}
		data, err := s.files.Read(filepath.Join(path, entry.Name()))
		if err != nil {
			return domain.Document{}, 0, err
		}
		document, err := s.read(format, data)
		switch domain.CodeOf(err) {
		case domain.CodeNotInsomnia, domain.CodeNothingToImport:
			continue
		}
		if err != nil {
			return domain.Document{}, 0, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		size += len(data)
		// Blocks name collections by index, which shifts once the files are joined.
		for _, draft := range document.Environments {
			blocks := slices.Clone(draft.Blocks)
			for index := range blocks {
				blocks[index].Collection += len(combined.Collections)
			}
			draft.Blocks = blocks
			combined.Environments = append(combined.Environments, draft)
		}
		combined.Collections = append(combined.Collections, document.Collections...)
		combined.Globals = append(combined.Globals, document.Globals...)
		for _, skip := range document.Skipped {
			combined.LeftOut(skip.Code, skip.Count)
		}
	}
	if len(combined.Collections) == 0 && len(combined.Environments) == 0 && len(combined.Globals) == 0 {
		return domain.Document{}, 0, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return combined, size, nil
}
