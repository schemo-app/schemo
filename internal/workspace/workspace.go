// Package workspace owns what a workspace holds: the workspaces themselves, the collection tree,
// environments and variables, search, and the app settings.
package workspace

import (
	"context"
	"slices"
	"strings"
	"time"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// maxWorkspaceName is the longest workspace name: long enough for "Internal API catalogue", short
// enough to stay one row in the switcher.
const maxWorkspaceName = 40

// TopicChanged is published whenever the workspace on screen moves, so that every window redraws
// around the same one.
const TopicChanged = "workspace:changed"

// Workspaces is the switcher: which workspaces exist, which one the window shows, and what a new one is
// called. Every call answers with the whole set, which the window draws at once.
type Workspaces struct {
	store    *db.Store
	ids      platform.IDGen
	notifier domain.Notifier
	// remote is the account's side of a workspace that lives there. Nothing here is on the path of
	// making or naming a workspace: only ending one has to be said in both places, and only that asks.
	remote Remote
	// linker and inviter are what making a team's workspace needs beyond a row here: an account to exist
	// in, and people to be invited into it. A workspace of somebody's own needs neither.
	linker  Linker
	inviter Inviter
}

func NewWorkspaces(
	store *db.Store, ids platform.IDGen, notifier domain.Notifier, remote Remote,
	linker Linker, inviter Inviter,
) *Workspaces {
	return &Workspaces{
		store: store, ids: ids, notifier: notifier, remote: remote,
		linker: linker, inviter: inviter,
	}
}

// Start points the app at the workspace it opens in: the one it was left in, or the oldest one when the
// user has turned "reopen the last workspace" off.
//
// It writes the pointer rather than answering a different one, and runs once at launch. A
// preference read on every resolve would follow the whole session: everything that asks which workspace
// is on screen (the list, the history, the pruning, the imports) would be answered with the oldest
// one, and a window that switched away would be switched back.
//
//wails:ignore
func (s *Workspaces) Start(ctx context.Context) error {
	reopen, err := s.store.ReopenLast(ctx)
	if err != nil {
		return err
	}
	if reopen {
		return nil
	}

	list, err := s.store.Workspaces(ctx)
	if err != nil || len(list) == 0 {
		return err
	}
	active, err := s.store.ActiveWorkspace(ctx)
	if err != nil || active == list[0].ID {
		return err
	}
	// The event is not published: at this hour nothing is listening yet, and the window reads the
	// snapshot on its way up rather than waiting to be told.
	return s.store.SetActiveWorkspace(ctx, list[0].ID)
}

// Snapshot is every workspace and the pointer to the one on screen, the whole of what the switcher
// draws in one answer, with what each of them holds, because the window draws the counts in the
// same rows as the names.
func (s *Workspaces) Snapshot(ctx context.Context) (domain.WorkspaceState, error) {
	list, err := s.store.Workspaces(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	active, err := s.store.ActiveWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	counts, err := s.store.Counts(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	return domain.WorkspaceState{
		Workspaces: list, ActiveID: active, PersonalID: domain.WorkspacePersonalID, Counts: counts,
	}, nil
}

// Switch points the app at another workspace. Nothing is asked first and nothing is confirmed: the
// data below the window changes, which is what the switcher is for.
func (s *Workspaces) Switch(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := s.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	if err := s.store.SetActiveWorkspace(ctx, target.ID); err != nil {
		return domain.WorkspaceState{}, err
	}
	s.notifier.Publish(TopicChanged, Changed{Workspace: target})
	return s.Snapshot(ctx)
}

// Create makes a workspace and leaves the app where it was: a new workspace is empty, and moving the
// window into it without being asked would look like the app had lost its data. The switcher's
// caller switches explicitly.
func (s *Workspaces) Create(ctx context.Context, in WorkspaceInput) (Created, error) {
	name, err := s.uniqueName(ctx, "", in.Name)
	if err != nil {
		return Created{}, err
	}
	kind, err := parseKind(in.Kind)
	if err != nil {
		return Created{}, err
	}
	// Nobody is invited into a workspace of somebody's own: it holds one person, and that is the whole of
	// what it means. The server refuses it too — this is the shape of the answer, not the rule.
	if kind != domain.WorkspaceTeam && len(in.Invites) > 0 {
		return Created{}, domain.Refuse(domain.CodeWorkspacePersonal, domain.ErrNotAllowed, nil)
	}

	created := domain.NewWorkspace(s.ids(), name, kind,
		strings.TrimSpace(in.Color), time.Now().UnixMilli())
	if err := s.store.SaveWorkspace(ctx, created); err != nil {
		return Created{}, err
	}

	// A team's workspace is created in the account before anybody is invited into it, and it is created
	// now rather than by the engine's next round: the form is showing people to invite, and there
	// is nothing to invite them into until the account has the workspace.
	//
	// A workspace that could not be created there is not kept: it would be a row that never links,
	// drawn in the switcher for ever. The one case that repairs itself is a lost answer, since the
	// next round finds the workspace in the account and mirrors it back.
	if kind == domain.WorkspaceTeam {
		if err := s.linker.LinkNow(ctx, created.ID); err != nil {
			_ = s.store.DeleteWorkspace(ctx, created.ID)
			return Created{}, err
		}
	}

	invites := s.inviteAll(ctx, created.ID, in.Invites)
	state, err := s.Snapshot(ctx)
	if err != nil {
		return Created{}, err
	}
	return Created{WorkspaceState: state, Invites: invites}, nil
}

// inviteAll asks everybody written in the form into the new workspace. A refusal is told beside the
// address it was about and the others still go on; an empty address is a row nobody filled in.
func (s *Workspaces) inviteAll(ctx context.Context, workspaceID string, invites []Invitation) []InviteOutcome {
	outcomes := []InviteOutcome{}
	for _, one := range invites {
		address := strings.TrimSpace(one.Email)
		if address == "" {
			continue
		}
		outcome := InviteOutcome{Email: address}
		if err := s.inviter.Invite(ctx, workspaceID, address, one.RoleID); err != nil {
			outcome.Failure = domain.FailureOf(err, domain.CodeServerRefused)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

// parseKind is what the form chose, kept to the two words there are: an empty one is a workspace that
// is the person's own, and anything else is a build sending a word this one does not have. It is
// refused rather than taken as one of the two, since a workspace created with the wrong kind is one
// somebody has to notice and rename.
func parseKind(kind string) (domain.WorkspaceKind, error) {
	if kind == "" {
		return domain.WorkspacePersonal, nil
	}
	held := domain.WorkspaceKind(kind)
	if !held.Valid() {
		return "", domain.Refuse(domain.CodeUnknownField, nil, domain.Args{"field": "kind"})
	}
	return held, nil
}

func (s *Workspaces) Update(
	ctx context.Context,
	id string,
	patch WorkspacePatch,
) (domain.WorkspaceState, error) {
	current, err := s.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	// The workspace the app is born with keeps its name and what it is for: the name is how the
	// account's own workspace is found across the link, and what it is for is what makes it that workspace.
	// Its colour is its own to change, since nothing is found by a colour.
	if domain.IsPersonal(current.ID) && (patch.Name != nil || patch.Kind != nil) {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspacePermanent,
			domain.ErrNotAllowed, nil)
	}

	if patch.Name != nil {
		name, err := s.uniqueName(ctx, current.ID, *patch.Name)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		current.Name = name
	}
	if patch.Color != nil {
		current.Color = strings.TrimSpace(*patch.Color)
	}
	if patch.Kind != nil {
		kind, err := parseKind(*patch.Kind)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		// What a workspace is for decides who may be in it, so it is the account's answer and not this
		// device's: only the server can say whether anybody else is in the workspace, and it is the one
		// that refuses. A workspace that is not in the account has nobody else in it by definition, so
		// the word is written here.
		if current.RemoteID != "" {
			if err := s.remote.ChangeKind(ctx, current.RemoteID, kind); err != nil {
				return domain.WorkspaceState{}, err
			}
		}
		current.Kind = kind
	}
	current.UpdatedAt = time.Now().UnixMilli()

	if err := s.store.SaveWorkspace(ctx, current); err != nil {
		return domain.WorkspaceState{}, err
	}
	if active, err := s.store.ActiveWorkspace(ctx); err == nil && active == current.ID {
		s.notifier.Publish(TopicChanged, Changed{Workspace: current})
	}
	return s.Snapshot(ctx)
}

// Delete removes a workspace and everything in it. The last one is refused: every other answer the
// app gives leans on there being a workspace to keep things in, and which one it is does not matter, so
// the row the app is born with goes like any other. Deleting the one on screen moves the pointer to
// the oldest that is left, so the window never points at a row that is gone.
//
// A workspace that is also in the account goes from there first. The other order would leave a device
// with no row and an account with a workspace: the next round would find nothing paired with it and
// make it again, a deleted workspace coming back empty.
func (s *Workspaces) Delete(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := s.removable(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	if target.RemoteID != "" {
		if err := s.remote.Delete(ctx, target.RemoteID); err != nil && !isAlreadyGone(err) {
			return domain.WorkspaceState{}, err
		}
	}
	return s.removeFromDevice(ctx, target)
}

// Leave takes this person out of a workspace somebody else's. The account is told first and the row
// goes after it: a device that dropped the workspace and then failed to leave would find the workspace
// still there on its next round, the person still in it, and make it again.
//
// It is separate from ending one because they are different acts with different rights: whoever
// created a workspace may end it for everybody, and anybody in it may step out, a member who may not
// end the workspace having no other way to leave one.
func (s *Workspaces) Leave(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := s.removable(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	if target.RemoteID == "" {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspaceMissing,
			domain.ErrNotFound, nil)
	}

	if err := s.remote.Leave(ctx, target.RemoteID); err != nil && !isAlreadyGone(err) {
		return domain.WorkspaceState{}, err
	}
	return s.removeFromDevice(ctx, target)
}

// removable is the workspace an ending or a leaving is about. The workspace the app is born with is
// neither: not because of how many are left, since it is always there and the app always has
// somewhere to keep things, but because of what it is: a device's own workspace in the account is
// paired with it, and there is no second one to pair with.
func (s *Workspaces) removable(ctx context.Context, id string) (domain.Workspace, error) {
	target, err := s.store.Workspace(ctx, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if domain.IsPersonal(target.ID) {
		return domain.Workspace{}, domain.Refuse(domain.CodeWorkspacePermanent, domain.ErrNotAllowed, nil)
	}
	return target, nil
}

// removeFromDevice removes a workspace from this device and answers where the window lands.
func (s *Workspaces) removeFromDevice(
	ctx context.Context,
	target domain.Workspace,
) (domain.WorkspaceState, error) {
	// Which workspace is on screen is read before the row goes: afterwards the pointer names something
	// that is no longer there, and the answer to "was this the one being shown" is gone with it.
	active, err := s.store.ActiveWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	if err := s.store.DeleteWorkspace(ctx, target.ID); err != nil {
		return domain.WorkspaceState{}, err
	}

	// The successor is picked here rather than left to the store's fallback, so a reader of this
	// call can see which workspace the window lands on; the fallback keeps its one job, a pointer that
	// went stale behind the app's back.
	if active == target.ID {
		remaining, err := s.store.Workspaces(ctx)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		if len(remaining) > 0 {
			if err := s.store.SetActiveWorkspace(ctx, remaining[0].ID); err != nil {
				return domain.WorkspaceState{}, err
			}
			s.notifier.Publish(TopicChanged, Changed{Workspace: remaining[0]})
		}
	}
	return s.Snapshot(ctx)
}

// isAlreadyGone reports an answer of "there is no such workspace" from the account. For an act whose
// point is that the workspace is no longer the person's, that is the act already done, not a failure to
// report.
func isAlreadyGone(err error) bool {
	switch domain.CodeOf(err) {
	case domain.CodeWorkspaceMissing, domain.CodeNotFound:
		return true
	}
	return false
}

// uniqueName is a name a workspace may carry: a clean one that no other workspace wears. The workspace the app
// is born with has no name of its own — the window words it — so it takes none away.
func (s *Workspaces) uniqueName(ctx context.Context, except string, raw string) (string, error) {
	name, err := domain.CleanName(raw, maxWorkspaceName)
	if err != nil {
		return "", err
	}
	all, err := s.store.Workspaces(ctx)
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(all, func(w domain.Workspace) bool {
		return w.ID != except && w.Name == name
	}) {
		return "", domain.NameTaken()
	}
	return name, nil
}

// Remote is the account's side of a workspace that lives there. Ending one is not this device's to do
// alone: a copy deleted only here would be handed back by the next round, empty, with the person's
// work gone from where they deleted it.
//
// It is declared here and not taken from the engine because it is not the engine's question: what a
// workspace is to the account belongs to the workspace, and nothing about it is on the path of a push.
type Remote interface {
	// Delete ends a workspace in the account. It names the workspace by its id and nothing else: the act is
	// made deliberate by the sheet that asked for it, and a name travelling with it would be the
	// one this device happens to draw, which a rename still in flight would be refused over.
	Delete(ctx context.Context, workspaceID string) error
	// ChangeKind asks the account to change what a workspace is for. It is asked rather than decided
	// because the rule is about who is in the workspace, and only the account knows that: a workspace a
	// second person works in may not be turned into somebody's own, and the refusal is the server's
	// to give.
	ChangeKind(ctx context.Context, workspaceID string, kind domain.WorkspaceKind) error
	// Leave takes this person out of a workspace somebody else's. It is the one way a member exits: a
	// member who may not delete the workspace cannot end it for everybody, and a row deleted only here
	// would be handed back by the next round, with the account still holding their place.
	Leave(ctx context.Context, workspaceID string) error
}

// Linker makes a workspace in the account now and joins this device's row to it. It is the sync
// engine's own road, asked for rather than copied: a create whose answer was lost is found again by
// the id the engine names and writes down before the call, and a second implementation would be a
// second set of ways to go wrong. It is asked only where a workspace has to exist in the account at
// once, such as a team's, which other people are about to be invited into. A workspace of somebody's
// own links when the engine next runs.
type Linker interface {
	LinkNow(ctx context.Context, workspaceID string) error
}

// Inviter is the team of a workspace, as far as making one is concerned.
type Inviter interface {
	Invite(ctx context.Context, workspaceID, email, roleID string) error
}

// The shapes that exist only for the window: what making a workspace asks for, what it answers with,
// an invitation on its way out, and what became of one. A workspace and its members are domain types.

// WorkspaceInput is what the create card sends: what the workspace is called, how it looks, and what it is
// for. The kind ("Personal" or "Team") is a word this app keeps and travels with the workspace, not a
// claim about who may enter: who is in a workspace is membership, and membership is the cloud panel's
// to hand out.
type WorkspaceInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// Kind is "personal" or "team", and empty is the personal one: a workspace nobody said anything about
	// is somebody's own.
	Kind string `json:"kind,omitempty"`
	// Invites are the people written in the create form, with the role each is to be given here.
	// They are sent the moment the workspace exists in the account, because there is nothing to invite
	// anybody into before that.
	Invites []Invitation `json:"invites,omitempty"`
}

// Created is what making a workspace came to: the state a window draws, and what became of each address
// written in the form. They are one answer rather than two calls, because the workspace exists either
// way once the account has it: a refused letter does not undo it, and the person is owed the one
// that failed rather than a sentence about all of them.
type Created struct {
	// The state is the whole set as every other call answers it — a window draws the switcher and the
	// form from one answer — with what became of the invitations beside it.
	domain.WorkspaceState
	// Invites are the outcomes, in the order the addresses were written. A workspace made without any is
	// an empty list and not a nil one, which is what a window draws nothing for.
	Invites []InviteOutcome `json:"invites"`
}

// WorkspacePatch is a partial change: a nil field is left as it is.
type WorkspacePatch struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
	// Kind is "personal" or "team": what a workspace is for changes when the person working in it decides
	// it is somebody else's too.
	Kind *string `json:"kind,omitempty"`
}

// Invitation is one address written in the create form, and the role it will be given. An empty
// role is the organisation's default, which is the server's to choose.
type Invitation struct {
	Email  string `json:"email"`
	RoleID string `json:"roleId,omitempty"`
}

// InviteOutcome is what became of one address. A refusal is the server's own — "no seats left"
// and "no such address" are different things to tell somebody — and the window words it.
type InviteOutcome struct {
	Email   string          `json:"email"`
	Failure *domain.Failure `json:"failure,omitempty"`
}

// Changed is what a window receives when the workspace on screen moves.
type Changed struct {
	Workspace domain.Workspace `json:"workspace"`
}

// Tabs is the strip of the workspace on screen as the window left it. Tab drafts that no tab
// names any more (a run that ended before the window discarded them) are deleted on the way.
func (s *Workspaces) Tabs(ctx context.Context) (domain.Strip, error) {
	active, err := s.store.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Strip{}, err
	}
	strip, err := s.store.Strip(ctx, active)
	if err != nil {
		return domain.Strip{}, err
	}
	named := map[domain.DraftID]bool{}
	for _, tab := range strip.Tabs {
		named[tab.DraftID] = true
	}
	drafts, err := s.store.DraftIDs(ctx, active)
	if err != nil {
		return domain.Strip{}, err
	}
	for _, id := range drafts {
		if domain.IsTabDraft(id) && !named[id] {
			if err := s.store.DeleteDraft(ctx, active, id); err != nil {
				return domain.Strip{}, err
			}
		}
	}
	return strip, nil
}

// SaveTabs stores the strip of the workspace it names. The workspace travels with the strip
// so that a save sent just before a switch lands where it came from.
func (s *Workspaces) SaveTabs(ctx context.Context, strip domain.Strip) error {
	if _, err := s.store.Workspace(ctx, strip.Workspace); err != nil {
		return err
	}
	strip.Tabs = domain.OrEmpty(strip.Tabs)
	for _, tab := range strip.Tabs {
		if !tab.Kind.Valid() || tab.ID == "" {
			return domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed, domain.Args{"field": "kind"})
		}
	}
	return s.store.SaveStrip(ctx, strip)
}
