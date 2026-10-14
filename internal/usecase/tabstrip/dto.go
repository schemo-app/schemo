package tabstrip

import "schemo/internal/domain"

// Strip is the whole answer: what is open, which tab is on screen, and the drafts that fell out of
// the strip along the way. A closed request tab's draft is the caller's to delete — this feature
// keeps the strip and never touches the drafts table.
type Strip struct {
	Tabs      []domain.Tab     `json:"tabs"`
	Active    string           `json:"active"`
	Discarded []domain.DraftID `json:"discarded,omitempty"`
}

// OpenInput is what opening a tab says about it. DraftID is filled in for a request tab whose draft
// the caller has already prepared (a record's copy, a duplicate); a request names the node it edits
// by Ref and the record it shows by Record; a collections tab names the collection by Ref; a page
// names nothing; NewTab is the ⌘-click — a plain open lands in the preview slot.
type OpenInput struct {
	Kind    domain.TabKind `json:"kind"`
	Ref     string         `json:"ref,omitempty"`
	DraftID domain.DraftID `json:"draftId,omitempty"`
	Record  string         `json:"record,omitempty"`
	NewTab  bool           `json:"newTab,omitempty"`
	// Background opens without showing: the palette's «in the background» stages a tab and the work
	// area stays where it was.
	Background bool `json:"background,omitempty"`
}
