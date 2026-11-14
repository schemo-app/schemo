package request

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	"schemo/internal/convert/har"
	"schemo/internal/desktop"
	"schemo/internal/domain"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The kind of file a saved session is, as the dialog offers it. HAR is its own extension and
// filter: devtools and proxies read a session by name.
var harFilter = []application.FileFilter{{DisplayName: "HAR", Pattern: "*.har"}}

// ExportHar writes one tab's capture as a HAR file and reports whether one was written; a cancelled
// dialog is not a failure.
func (s *History) ExportHar(ctx context.Context, title, tabKey string) (bool, error) {
	data, name, err := s.har(ctx, tabKey)
	if err != nil {
		return false, err
	}
	path, err := s.host.SaveFile(title, harFileName(name), harFilter...)
	if err != nil || path == "" {
		return false, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, fmt.Errorf("file %s: %w", path, err)
	}
	return true, nil
}

// harFileName is a name the page can be recognised by and no file system refuses: `/` and `:` are
// legal in a host and not in a file's.
func harFileName(name string) string {
	return desktop.SafeFileName(name, "session", ".har")
}

// har exports one tab's capture (or all of it, for an empty key) as HAR. Tab keys must match
// tabKeyOf in frontend/src/lib/recordTabs.ts.
func (s *History) har(ctx context.Context, tabKey string) ([]byte, string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, "", err
	}

	// The store's uncapped read, not List, which stops at what the panel draws.
	records, err := s.store.Records(ctx, workspace, domain.SourceBrowser, 0)
	if err != nil {
		return nil, "", err
	}

	// Named by the first record of the tab, and by a flag rather than by the default still standing:
	// a tab whose name is literally "session" would otherwise rename the file on every row.
	name, named := "session", false
	entries := []har.Entry{}
	for _, rec := range records {
		if tabKey != "" && tabKeyOf(rec) != tabKey {
			continue
		}
		if !named {
			name, named = tabNameOf(rec), true
		}
		entries = append(entries, har.Entry{
			Record:       rec,
			RequestBody:  s.body(ctx, rec, domain.SideRequest),
			ResponseBody: s.body(ctx, rec, domain.SideResponse),
		})
	}

	// Oldest first: a HAR file is a session in the order it happened, and the list it was built from
	// is newest-first because that is the order a panel shows.
	slices.Reverse(entries)

	data, err := har.Export(s.build.Name, s.build.Version, entries)
	if err != nil {
		return nil, "", err
	}
	return data, name, nil
}

// body reads through the lazy loader so another device's bodies are fetched; an unreadable body is
// written empty rather than failing the file.
func (s *History) body(ctx context.Context, rec domain.Record, side domain.BodySide) string {
	text, err := s.content(ctx, rec.ID, side)
	if err != nil {
		return ""
	}
	return text
}

// tabKeyOf names a record's tab; other devices' tab ids are prefixed, since each device numbers its own.
func tabKeyOf(rec domain.Record) string {
	key := "unknown"
	switch {
	case rec.TabID != 0:
		key = strconv.Itoa(rec.TabID)
	case rec.TabURL != "":
		key = rec.TabURL
	}
	if rec.IsRemote {
		return "remote:" + key
	}
	return key
}

// What the file is called in a save dialog: the page's host when the record knows it, and its title
// otherwise. A name nobody can read is a name nobody can find again.
func tabNameOf(rec domain.Record) string {
	if host := domain.HostOf(rec.TabURL); host != "" {
		return host
	}
	if rec.TabTitle != "" {
		return rec.TabTitle
	}
	return "session"
}
