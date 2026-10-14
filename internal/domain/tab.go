package domain

import "strings"

// Tab is one tab of the window's strip: what is open, and which of them the work area shows. The
// order is the slice's — the strip itself is the tabstrip feature's — and this is the shape both
// sides agree on.
type Tab struct {
	ID      string  `json:"id"`
	Kind    TabKind `json:"kind"`
	Ref     string  `json:"ref,omitempty"`
	DraftID DraftID `json:"draftId,omitempty"`
	Record  string  `json:"record,omitempty"`
	Pinned  bool    `json:"pinned"`
	Preview bool    `json:"preview"`
}

// TabKind is what a tab is about: a request being composed, a collection's page, or one of the
// four pages the window keeps a single copy of — the overview, the history, the capture and the
// collections this device has published.
type TabKind string

const (
	TabKindRequest     TabKind = "request"
	TabKindCollections TabKind = "collections"
	TabKindWelcome     TabKind = "welcome"
	TabKindHistory     TabKind = "history"
	TabKindCapture     TabKind = "capture"
	TabKindPubs        TabKind = "pubs"
)

// Valid reports whether the kind is one the strip knows how to draw.
func (k TabKind) Valid() bool {
	switch k {
	case TabKindRequest, TabKindCollections, TabKindWelcome, TabKindHistory, TabKindCapture,
		TabKindPubs:
		return true
	}
	return false
}

// Page reports whether the kind is a page: it shows the same thing however it was reached, so a
// strip holds at most one of each.
func (k TabKind) Page() bool {
	switch k {
	case TabKindWelcome, TabKindHistory, TabKindCapture, TabKindPubs:
		return true
	}
	return false
}

// TabDraftPrefix names the drafts the strip mints for its request tabs. The command line's fixed id
// is the strip's too — the first blank tab keeps it.
const TabDraftPrefix = "tab:"

// IsTabDraft reports whether a draft belongs to the strip, and is therefore written down: a
// collection node's draft is a proposal until it is saved, and a tab's is what a restart restores.
func IsTabDraft(id DraftID) bool {
	return id == DraftCommandLine || strings.HasPrefix(string(id), TabDraftPrefix)
}
