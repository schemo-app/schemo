// Package tabstrip owns the window's strip: which requests are open, in what order, and which one
// the work area shows. It lives in Go because the strip decides which draft is being edited and is
// restored at the next launch, which is more than a window's view state — and the rules are the
// mockup's (Schemo App): a plain open lands in the preview slot, ⌘-open keeps a tab, a close falls
// to the neighbour, and a page — the history or the capture — is one however it was reached.
package tabstrip

import (
	"context"
	"slices"
	"sync"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// UseCase keeps one strip per workspace, in memory and — while the restore switch is on — in the
// store. Every change writes the whole strip: it is a few rows of one moment, and a half strip
// stored would be one the next launch restores wrongly.
type UseCase struct {
	mu     sync.Mutex
	store  Store
	policy Policy
	scope  domain.Scope
	ids    platform.IDGen
	strips map[string]strip
}

type strip struct {
	tabs   []domain.Tab
	active string
}

func NewUseCase(store Store, policy Policy, scope domain.Scope, ids platform.IDGen) *UseCase {
	return &UseCase{
		store:  store,
		policy: policy,
		scope:  scope,
		ids:    ids,
		strips: map[string]strip{},
	}
}

// Strip is the strip as it stands. The first look of a run restores it — or starts a fresh one,
// when the restore switch is off — and answers the drafts the resting may have dropped.
func (u *UseCase) Strip(ctx context.Context) (Strip, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Strip{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.loadLocked(ctx, workspace)
}

// Reconcile closes tabs whose saved request or collection no longer exists.
func (u *UseCase) Reconcile(ctx context.Context, tree []domain.Collection) (Strip, error) {
	refs := make(map[string]domain.TabKind)
	var visit func([]domain.Collection)
	visit = func(levels []domain.Collection) {
		for _, level := range levels {
			refs[level.ID] = domain.TabKindCollections
			for _, node := range level.Items {
				refs[node.ID] = domain.TabKindRequest
			}
			visit(level.Children)
		}
	}
	visit(tree)
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		oldActive := indexOf(s.tabs, s.active)
		var discarded []domain.DraftID
		s.tabs = slices.DeleteFunc(s.tabs, func(tab domain.Tab) bool {
			if tab.Ref == "" || refs[tab.Ref] == tab.Kind {
				return false
			}
			discarded = append(discarded, draftOf(tab)...)
			return true
		})
		if len(s.tabs) == 0 {
			s.active = ""
		} else if indexOf(s.tabs, s.active) < 0 {
			s.active = s.tabs[max(0, min(oldActive, len(s.tabs)-1))].ID
		}
		return discarded, nil
	})
}

// MintDraftID names the draft a request tab about to open will edit: the command line's while it is
// free — an upgrade keeps the request the older build left half composed — and a minted one after.
func (u *UseCase) MintDraftID(ctx context.Context) (domain.DraftID, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	stripped, err := u.loadLocked(ctx, workspace)
	if err != nil {
		return "", err
	}
	return u.draftIDFor(stripped.Tabs), nil
}

// Open opens a tab for the thing named, or activates the one already open for it. A plain open
// lands in the preview slot — the preview that was there steps aside in place — and a ⌘-open takes
// a place of its own right after the active tab. The tab becomes the active one unless the open is
// a background one: the palette stages those and the work area stays where it was. A page is one:
// opening the history shows the history the strip already holds.
func (u *UseCase) Open(ctx context.Context, in OpenInput) (Strip, error) {
	if !in.Kind.Valid() {
		return Strip{}, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
			domain.Args{"field": "kind"})
	}
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		if in.Kind.Page() {
			u.openPage(s, in)
			return nil, nil
		}
		return u.openThing(s, in), nil
	})
}

// openPage shows the page's tab, making it where there is none.
func (u *UseCase) openPage(s *strip, in OpenInput) {
	for i := range s.tabs {
		if s.tabs[i].Kind != in.Kind {
			continue
		}
		if !in.Background {
			s.active = s.tabs[i].ID
		}
		return
	}
	// A page never takes the preview slot: that slot is for the thing being looked at.
	tab := domain.Tab{ID: u.ids(), Kind: in.Kind}
	s.tabs = insertAfter(s.tabs, s.active, tab)
	if !in.Background {
		s.active = tab.ID
	}
}

// openThing shows the tab of a request or a collection, and answers the drafts that go with a tab
// this open stepped over.
func (u *UseCase) openThing(s *strip, in OpenInput) []domain.DraftID {
	if i := sameThing(s.tabs, in); i >= 0 {
		// The thing is already open: the tab is shown, and a ⌘-open keeps a preview for good. A
		// seed prepared for an open thing is a draft nobody will edit, so it goes with the answer.
		if in.NewTab {
			s.tabs[i].Preview = false
		}
		if !in.Background {
			s.active = s.tabs[i].ID
		}
		if in.DraftID != "" && in.DraftID != s.tabs[i].DraftID {
			return []domain.DraftID{in.DraftID}
		}
		return nil
	}

	tab := domain.Tab{ID: u.ids(), Kind: in.Kind, Ref: in.Ref, DraftID: in.DraftID,
		Record: in.Record}
	if tab.Kind == domain.TabKindRequest && tab.DraftID == "" {
		tab.DraftID = u.draftIDFor(s.tabs)
	}

	var discarded []domain.DraftID
	if in.NewTab {
		s.tabs = insertAfter(s.tabs, s.active, tab)
	} else {
		tab.Preview = true
		if i := previewIndex(s.tabs); i >= 0 {
			// The plain open is what the preview slot is for: the old preview goes, with a request
			// tab's draft, and the new one takes its place without moving anything around it.
			discarded = append(discarded, draftOf(s.tabs[i])...)
			s.tabs[i] = tab
		} else {
			s.tabs = insertAfter(s.tabs, s.active, tab)
		}
	}
	if !in.Background {
		s.active = tab.ID
	}
	return discarded
}

// New appends a blank request tab at the end and shows it — the strip's «+» button.
func (u *UseCase) New(ctx context.Context) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		tab := u.blank(s.tabs)
		s.tabs = append(s.tabs, tab)
		s.active = tab.ID
		return nil, nil
	})
}

// Activate shows a tab.
func (u *UseCase) Activate(ctx context.Context, id string) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		if indexOf(s.tabs, id) < 0 {
			return nil, unknownTab(id)
		}
		s.active = id
		return nil, nil
	})
}

// Close closes a tab and shows its neighbour: the one that took its place, or the last one left.
// The last tab closing empties the strip; what the window draws for that is the design's own page.
func (u *UseCase) Close(ctx context.Context, id string) (Strip, error) {
	return u.closeWhere(ctx, id, func(_ []domain.Tab, t domain.Tab) bool { return t.ID == id })
}

// CloseOthers closes every tab but the named one and the pinned ones, keeping the named one on
// screen.
func (u *UseCase) CloseOthers(ctx context.Context, id string) (Strip, error) {
	return u.closeWhere(ctx, id, func(_ []domain.Tab, t domain.Tab) bool {
		return t.ID != id && !t.Pinned
	})
}

// CloseRight closes every tab after the named one, keeping the pinned ones.
func (u *UseCase) CloseRight(ctx context.Context, id string) (Strip, error) {
	return u.closeWhere(ctx, id, func(tabs []domain.Tab, t domain.Tab) bool {
		return !t.Pinned && indexOf(tabs, t.ID) > indexOf(tabs, id)
	})
}

// closeWhere closes what the predicate names and hands the shown tab to the neighbour rule: the
// named tab when it survived, and otherwise the tab that took the old active place, clamped into
// what is left.
func (u *UseCase) closeWhere(
	ctx context.Context,
	named string,
	closes func(tabs []domain.Tab, t domain.Tab) bool,
) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		if indexOf(s.tabs, named) < 0 {
			return nil, unknownTab(named)
		}
		old := s.tabs
		oldActive := indexOf(old, s.active)

		var (
			next      []domain.Tab
			discarded []domain.DraftID
		)
		for _, t := range old {
			if closes(old, t) {
				discarded = append(discarded, draftOf(t)...)
				continue
			}
			next = append(next, t)
		}
		// The last tab may go: what is left is an empty strip, and the window draws the offer the
		// design has for it — a blank tab nobody asked for is not a tab.
		if len(next) == 0 {
			s.tabs = nil
			s.active = ""
			return discarded, nil
		}
		if indexOf(next, s.active) < 0 {
			at := oldActive
			if at >= len(next) {
				at = len(next) - 1
			}
			if at < 0 {
				at = 0
			}
			if indexOf(next, named) >= 0 {
				at = indexOf(next, named)
			}
			s.active = next[at].ID
		}
		s.tabs = next
		return discarded, nil
	})
}

// Pin pins or unpins a tab. Pinned tabs stand before the others — and a pin is never a preview.
func (u *UseCase) Pin(ctx context.Context, id string, pinned bool) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		i := indexOf(s.tabs, id)
		if i < 0 {
			return nil, unknownTab(id)
		}
		s.tabs[i].Pinned = pinned
		if pinned {
			s.tabs[i].Preview = false
		}
		return nil, nil
	})
}

// Keep takes a preview out of the preview slot: it stays where it is for good.
func (u *UseCase) Keep(ctx context.Context, id string) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		i := indexOf(s.tabs, id)
		if i < 0 {
			return nil, unknownTab(id)
		}
		s.tabs[i].Preview = false
		return nil, nil
	})
}

// Move drags a tab onto another's side: it lands after the target when it came from before it, and
// before it otherwise, taking the target's place among the pinned. A drag is never a preview.
func (u *UseCase) Move(ctx context.Context, id string, to string) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		from, target := indexOf(s.tabs, id), indexOf(s.tabs, to)
		if from < 0 {
			return nil, unknownTab(id)
		}
		if target < 0 || id == to {
			return nil, nil
		}
		moved := s.tabs[from]
		moved.Pinned = s.tabs[target].Pinned
		moved.Preview = false

		s.tabs = slices.Delete(s.tabs, from, from+1)
		s.tabs = slices.Insert(s.tabs, target, moved)
		return nil, nil
	})
}

// Duplicate copies a tab right after itself, shown and unpinned: the draft is the caller's — it
// has already been copied under the id handed in. The copy of a node-bound tab is a plain request:
// a copy that kept the node would be a second editor of a saved request.
func (u *UseCase) Duplicate(ctx context.Context, id string, draftID domain.DraftID) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		source := tabAt(s.tabs, id)
		if source == nil {
			return nil, unknownTab(id)
		}
		if source.Kind.Page() {
			return nil, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
				domain.Args{"field": "kind"})
		}
		copied := *source
		copied.ID = u.ids()
		copied.Ref = ""
		copied.DraftID = draftID
		copied.Pinned = false
		copied.Preview = false
		s.tabs = insertAfter(s.tabs, id, copied)
		s.active = copied.ID
		return nil, nil
	})
}

// SetRecord points a tab at the history record it shows — what a browser tab follows when the
// capture under it moves on.
func (u *UseCase) SetRecord(ctx context.Context, id string, record string) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		i := indexOf(s.tabs, id)
		if i < 0 {
			return nil, unknownTab(id)
		}
		s.tabs[i].Record = record
		return nil, nil
	})
}

// Adopt points a request tab at the node it has just been saved as: it stays where it is and keeps
// its pin — it is still the request it was — and edits the node's draft from now on, so the draft
// it was composing goes. A page is not saved as a node.
func (u *UseCase) Adopt(ctx context.Context, id string, nodeID string) (Strip, error) {
	return u.mutate(ctx, func(s *strip) ([]domain.DraftID, error) {
		i := indexOf(s.tabs, id)
		if i < 0 {
			return nil, unknownTab(id)
		}
		if s.tabs[i].Kind != domain.TabKindRequest {
			return nil, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
				domain.Args{"field": "kind"})
		}
		discarded := draftOf(s.tabs[i])
		s.tabs[i].Ref = nodeID
		s.tabs[i].DraftID = domain.DraftID(nodeID)
		s.tabs[i].Preview = false
		return discarded, nil
	})
}

// mutate is every change to the strip: it takes the lock, restores the strip on the first look,
// lets the change run, and writes the strip down. A refused change leaves it as it was.
func (u *UseCase) mutate(
	ctx context.Context,
	change func(*strip) ([]domain.DraftID, error),
) (Strip, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Strip{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	loaded, err := u.loadLocked(ctx, workspace)
	if err != nil {
		return Strip{}, err
	}
	s := u.strips[workspace]
	s.tabs = slices.Clone(s.tabs)
	discarded, err := change(&s)
	if err != nil {
		return Strip{}, err
	}
	discarded = append(loaded.Discarded, discarded...)
	sortPinnedFirst(s.tabs)
	restore, err := u.policy.RestoreTabs(ctx)
	if err != nil {
		return Strip{}, err
	}
	if restore {
		if err := u.store.SaveTabs(ctx, workspace, s.tabs, s.active); err != nil {
			return Strip{}, err
		}
	}
	u.strips[workspace] = s
	return Strip{Tabs: slices.Clone(s.tabs), Active: s.active, Discarded: discarded}, nil
}

// loadLocked answers the strip of a workspace, restoring the stored one on the first look of a run
// — or starting fresh when the restore switch is off, clearing what a time with it on had left and
// answering the drafts that go with the rows.
func (u *UseCase) loadLocked(ctx context.Context, workspace string) (Strip, error) {
	if s, ok := u.strips[workspace]; ok {
		return Strip{Tabs: slices.Clone(s.tabs), Active: s.active}, nil
	}
	restore, err := u.policy.RestoreTabs(ctx)
	if err != nil {
		return Strip{}, err
	}
	tabs, active, err := u.store.Tabs(ctx, workspace)
	if err != nil {
		return Strip{}, err
	}
	// Rows written under a model this build cannot draw — a kind that no longer exists — go with
	// their drafts: the strip is only ever as old as the shape it was stored in.
	var dropped []domain.DraftID
	kept := make([]domain.Tab, 0, len(tabs))
	for _, t := range tabs {
		if !t.Kind.Valid() {
			dropped = append(dropped, draftOf(t)...)
			continue
		}
		kept = append(kept, t)
	}
	tabs = kept

	if !restore {
		// The command line is not a tab's to take: the fresh tab below takes it over, and the
		// request left half composed in it survives a launch with the tabs not restored.
		discarded := dropped
		for _, t := range tabs {
			for _, id := range draftOf(t) {
				if id != domain.DraftCommandLine {
					discarded = append(discarded, id)
				}
			}
		}
		if err := u.store.ClearTabs(ctx, workspace); err != nil {
			return Strip{}, err
		}
		blank := u.blank(nil)
		u.strips[workspace] = strip{tabs: []domain.Tab{blank}, active: blank.ID}
		return Strip{Tabs: []domain.Tab{blank}, Active: blank.ID, Discarded: discarded}, nil
	}

	if len(tabs) == 0 {
		blank := u.blank(nil)
		u.strips[workspace] = strip{tabs: []domain.Tab{blank}, active: blank.ID}
		return Strip{Tabs: []domain.Tab{blank}, Active: blank.ID, Discarded: dropped}, nil
	}
	if indexOf(tabs, active) < 0 {
		active = tabs[0].ID
	}
	if len(dropped) > 0 {
		// What this build cannot draw is cleared from the store as well: the drafts go with it, and
		// the next launch finds a strip this one wrote.
		if err := u.store.SaveTabs(ctx, workspace, tabs, active); err != nil {
			return Strip{}, err
		}
	}
	u.strips[workspace] = strip{tabs: tabs, active: active}
	return Strip{Tabs: slices.Clone(tabs), Active: active, Discarded: dropped}, nil
}

// blank is the tab an empty strip holds: a new request with nothing in it.
func (u *UseCase) blank(tabs []domain.Tab) domain.Tab {
	return domain.Tab{ID: u.ids(), Kind: domain.TabKindRequest, DraftID: u.draftIDFor(tabs)}
}

// draftIDFor names the draft a request tab gets: the command line's while no tab holds it — an
// upgrade then finds the request where it was left — and a minted one otherwise.
func (u *UseCase) draftIDFor(tabs []domain.Tab) domain.DraftID {
	for _, t := range tabs {
		if t.DraftID == domain.DraftCommandLine {
			return domain.DraftID(domain.TabDraftPrefix + u.ids())
		}
	}
	return domain.DraftCommandLine
}

// draftOf is the draft a closed tab takes with it: a tab's own — the command line's or a minted
// one — and not a node's, whose proposal is not written down and not this feature's to delete.
func draftOf(t domain.Tab) []domain.DraftID {
	if domain.IsTabDraft(t.DraftID) {
		return []domain.DraftID{t.DraftID}
	}
	return nil
}

// sameThing is the tab already about what the open names, or -1: a record is looked up by the
// record it shows, and everything else by what it edits. A blank request names nothing.
func sameThing(tabs []domain.Tab, in OpenInput) int {
	if in.Record != "" {
		for i := range tabs {
			if tabs[i].Kind == domain.TabKindRequest && tabs[i].Record == in.Record {
				return i
			}
		}
		return -1
	}
	if in.Ref == "" {
		return -1
	}
	for i := range tabs {
		if tabs[i].Kind == in.Kind && tabs[i].Ref == in.Ref {
			return i
		}
	}
	return -1
}

func sortPinnedFirst(tabs []domain.Tab) {
	slices.SortStableFunc(tabs, func(a, b domain.Tab) int {
		if a.Pinned == b.Pinned {
			return 0
		}
		if a.Pinned {
			return -1
		}
		return 1
	})
}

func previewIndex(tabs []domain.Tab) int {
	for i, t := range tabs {
		if t.Preview {
			return i
		}
	}
	return -1
}

func insertAfter(tabs []domain.Tab, after string, tab domain.Tab) []domain.Tab {
	at := indexOf(tabs, after)
	if at < 0 {
		at = len(tabs) - 1
	}
	at++
	return slices.Insert(tabs, at, tab)
}

func indexOf(tabs []domain.Tab, id string) int {
	return slices.IndexFunc(tabs, func(t domain.Tab) bool { return t.ID == id })
}

func tabAt(tabs []domain.Tab, id string) *domain.Tab {
	if i := indexOf(tabs, id); i >= 0 {
		return &tabs[i]
	}
	return nil
}

func unknownTab(id string) error {
	return domain.Refuse(domain.CodeNotFound, domain.ErrNotFound, domain.Args{"tab": id})
}
