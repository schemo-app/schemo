package request

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
)

// ChooseSchemaFile selects a schema file for the request's protocol.
func (s *Drafts) ChooseSchemaFile(title string, protocol domain.Protocol) (string, error) {
	format, ok := schemaFormats[protocol]
	if !ok {
		return "", fmt.Errorf("request protocol %q has no schema files", protocol)
	}
	var patterns []string
	for _, ext := range format.extensions {
		patterns = append(patterns, "*"+ext)
	}
	return s.host.OpenFile(title, application.FileFilter{DisplayName: format.name, Pattern: strings.Join(patterns, ";")})
}

type schemaFormat struct {
	name       string
	extensions []string
}

var schemaFormats = map[domain.Protocol]schemaFormat{
	domain.ProtocolGraphQL: {name: "GraphQL", extensions: []string{".graphql", ".gql", ".graphqls"}},
	domain.ProtocolGRPC:    {name: "Protocol Buffers", extensions: []string{".proto"}},
}

// ChooseSchemaFolder asks for a folder whose schema files are added to a request's sources, for a
// module that spreads its definitions over many files.
func (s *Drafts) ChooseSchemaFolder(title string) (string, error) {
	return s.host.OpenFolder(title)
}

// schemaSourceFiles lists the schema files a file or folder source stands for.
func schemaSourceFiles(ctx context.Context, source domain.SchemaSource, protocol domain.Protocol) ([]string, error) {
	_, err := os.Stat(source.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, domain.Refuse(domain.CodeFileMissing, domain.ErrNotFound, domain.Args{"path": source.Path})
	}
	if err != nil {
		return nil, err
	}
	if source.Kind == domain.SchemaSourceFile {
		return []string{source.Path}, nil
	}
	return schemaFilesIn(ctx, source.Path, source.Skipped, schemaFormats[protocol].extensions...)
}

// schemaFileName is how a folder's file is named in its source: relative, with forward slashes.
func schemaFileName(folder, path string) string {
	name, err := filepath.Rel(folder, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(name)
}

// A folder picked by mistake, a home directory say, is refused instead of read for minutes.
const maxSchemaFiles = 1000
const maxSchemaInputBytes = 4 * domain.MaxBodyBytes

type schemaReader struct {
	mu    sync.Mutex
	files FileSource
	read  map[string][]byte
	bytes int
}

func newSchemaReader(files FileSource, count int) (*schemaReader, error) {
	if count > maxSchemaFiles {
		return nil, fmt.Errorf("more than %d schema sources", maxSchemaFiles)
	}
	return &schemaReader{files: files, read: make(map[string][]byte)}, nil
}

func (s *schemaReader) Read(path string) ([]byte, error) {
	path = filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.read[path]; ok {
		return data, nil
	}
	if len(s.read) == maxSchemaFiles {
		return nil, fmt.Errorf("more than %d schema files", maxSchemaFiles)
	}
	data, err := s.files.Read(path)
	if err != nil {
		return nil, err
	}
	if len(data) > domain.MaxBodyBytes || s.bytes+len(data) > maxSchemaInputBytes {
		return nil, fmt.Errorf("schema input exceeds the size limit at %q", filepath.Base(path))
	}
	s.read[path] = data
	s.bytes += len(data)
	return data, nil
}

// schemaFilesIn lists the files under a folder that end in one of the extensions, but the skipped.
func schemaFilesIn(ctx context.Context, folder string, skipped []string, extensions ...string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Hidden folders and node_modules hold copies that would clash with the real definitions.
		if entry.IsDir() && path != folder && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() || !slices.Contains(extensions, strings.ToLower(filepath.Ext(entry.Name()))) ||
			slices.Contains(skipped, schemaFileName(folder, path)) {
			return nil
		}
		if len(paths) == maxSchemaFiles {
			return fmt.Errorf("the folder holds more than %d %s files", maxSchemaFiles, strings.Join(extensions, " "))
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("the folder holds no %s files", strings.Join(extensions, " "))
	}
	return paths, nil
}
