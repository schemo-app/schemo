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

// ExportHar writes one tab's captured traffic as a HAR file and reports whether anything was
// written: a cancelled dialog is not a failure, and a toast for it would tell the user about a file
// that does not exist.
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

// har is the captured traffic of one tab as a HAR document, and a name a save dialog can open on. An empty key is the whole capture: every tab the workspace holds.
//
// The tab is named the way the window names it, by its id or by the page's address when the record
// has no id, and the two sides must agree: this is the second half of `tabKeyOf` in
// `frontend/src/lib/recordTabs.ts`.
func (s *History) har(ctx context.Context, tabKey string) ([]byte, string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, "", err
	}

	// The store's own read and not List: the list is capped at what the panel draws, and an export
	// that quietly left out the two-hundred-and-first request would be a file that lies about the
	// session it claims to be.
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

// A body that cannot be read is no body: losing the answer to a large one is no reason to write no
// file at all. It goes through the lazy read rather than the store, so a record of another device
// is fetched as the file is written; otherwise an export of a session kept elsewhere would be a
// file of empty bodies.
func (s *History) body(ctx context.Context, rec domain.Record, side domain.BodySide) string {
	text, err := s.Body(ctx, rec.ID, side)
	if err != nil {
		return ""
	}
	return text
}

// tabKeyOf names the tab a record was captured in. A tab's own number counts only where the record
// was captured: another device numbers its tabs from its own first one, so a number from there is
// prefixed, or two devices' seventh tabs would be one tab here.
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
