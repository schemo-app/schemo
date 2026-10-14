package wails

import (
	"context"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/transport/desktop"
)

// The kind of file a saved session is, as the dialog offers it. HAR is its own extension and its
// own filter: a collection is a JSON document somebody may open in an editor, and a session is a
// file devtools and proxies read by name.
const (
	harKindName  = "HAR"
	harPattern   = "*.har"
	harExtension = ".har"
)

var harFilter = []application.FileFilter{{DisplayName: harKindName, Pattern: harPattern}}

// ExportHar writes one tab's captured traffic as a HAR file and answers whether anything was
// written: a cancelled dialog is not a failure, and a window that showed a toast for it would be
// telling the user about a file that does not exist.
func (s *RecordsService) ExportHar(ctx context.Context, title, tabKey string) (bool, error) {
	data, name, err := s.records.ExportHAR(ctx, tabKey)
	if err != nil {
		return false, err
	}

	path, err := s.host.SaveFile(title, harFileName(name), harFilter...)
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, fmt.Errorf("file %s: %w", path, err)
	}
	return true, nil
}

// `/` and `:` are legal in a host and not in a file's, and the dialog should open on a name
// somebody can recognise as the page they were looking at.
func harFileName(name string) string {
	return desktop.SafeFileName(name, "session", harExtension)
}
