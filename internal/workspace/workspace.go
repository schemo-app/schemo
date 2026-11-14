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

// Start points the app at the last workspace, or the oldest when "reopen the last workspace" is off.
// It runs once at launch, so a switch later is not undone.
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

// Snapshot is every workspace with its counts and the active pointer: everything the switcher draws.
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

// Create makes a workspace without switching to it; the caller switches explicitly.
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

	// A team workspace is created in the account now, so invitations have somewhere to go. If that
	// fails the row is dropped, since it would never link.
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

// parseKind accepts "" (personal) and the two kinds; an unknown word is refused rather than guessed.
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

	// The device's own workspace keeps its name and kind, which pair it with the account's; the
	// colour may change.
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
		// Only the server knows who else is in a linked workspace, so it decides the kind.
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

// Delete removes a workspace; the last one is refused, and deleting the active one moves the pointer
// to the oldest. A linked workspace is ended in the account first, or the next round would recreate it.
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

// Leave takes this person out of someone else's workspace. The account is told first, or the next
// round would bring the workspace back.
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

// removable is the workspace being ended or left; the device's own workspace is paired with the
// account and cannot be either.
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

	// Pick the successor here so it is explicit; the store's fallback is for stale pointers.
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

// isAlreadyGone treats "no such workspace" from the account as the act already done.
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

// Remote is the account's side of a linked workspace; ending one only locally would come back empty
// on the next round.
type Remote interface {
	// Delete ends a workspace in the account by id; a name could be refused mid-rename.
	Delete(ctx context.Context, workspaceID string) error
	// ChangeKind asks the account to change the kind; only it knows who else is in the workspace.
	ChangeKind(ctx context.Context, workspaceID string, kind domain.WorkspaceKind) error
	// Leave takes this person out of someone else's workspace, the only exit for a member who cannot
	// delete it.
	Leave(ctx context.Context, workspaceID string) error
}

// Linker creates the account's copy of a workspace now, through the sync engine's idempotent path.
// Only team workspaces need it; personal ones link on the next round.
type Linker interface {
	LinkNow(ctx context.Context, workspaceID string) error
}

// Inviter is the team of a workspace, as far as making one is concerned.
type Inviter interface {
	Invite(ctx context.Context, workspaceID, email, roleID string) error
}

// The shapes that exist only for the window: what making a workspace asks for, what it answers with,
// an invitation on its way out, and what became of one. A workspace and its members are domain types.

// WorkspaceInput is what the create card sends. The kind is a label that travels with the workspace;
// membership is managed in the cloud panel.
type WorkspaceInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// Kind is "personal" or "team", and empty is the personal one: a workspace nobody said anything about
	// is somebody's own.
	Kind string `json:"kind,omitempty"`
	// Invites go out once the account has the workspace.
	Invites []Invitation `json:"invites,omitempty"`
}

// Created is the new state plus a result per invited address: the workspace exists once the account
// has it, even if some invitations fail.
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
