// Package workspace owns the spaces the app keeps its things in: which ones exist, which one is on
// screen, and what a new one starts as.
package workspace

import (
	"context"
	"slices"
	"strings"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// maxNameLength is the design's limit for a workspace's name: long enough for "Internal API
// catalogue", short enough to stay one row in the switcher.
const maxNameLength = 40

// TopicChanged is published whenever the workspace on screen moves, so that every window redraws
// around the same one.
const TopicChanged = "workspace:changed"

type UseCase struct {
	store    Store
	ids      platform.IDGen
	notifier domain.Notifier
	// remote is the account's side of a space that lives there. Nothing here is on the path of
	// making or naming a space: only ending one has to be said in both places, and only that asks.
	remote Remote
	// linker and inviter are what making a team's space needs beyond a row here: an account to exist
	// in, and people to be invited into it. A space of somebody's own needs neither.
	linker  Linker
	inviter Inviter
}

func NewUseCase(
	store Store, ids platform.IDGen, notifier domain.Notifier, remote Remote,
	linker Linker, inviter Inviter,
) *UseCase {
	return &UseCase{
		store: store, ids: ids, notifier: notifier, remote: remote,
		linker: linker, inviter: inviter,
	}
}

// Start points the app at the space it opens in: the one it was left in, or the oldest one when the
// user has turned "reopen the last workspace" off.
//
// It writes the pointer rather than answering a different one, and it runs once at launch. A
// preference read on every resolve would follow the whole session: everything that asks which
// space is on screen — the list, the history, the pruning, the imports — would be answered with
// the oldest one, and a window that switched away from it would be switched back.
func (u *UseCase) Start(ctx context.Context) error {
	reopen, err := u.store.ReopenLast(ctx)
	if err != nil {
		return err
	}
	if reopen {
		return nil
	}

	list, err := u.store.Workspaces(ctx)
	if err != nil || len(list) == 0 {
		return err
	}
	active, err := u.store.ActiveWorkspace(ctx)
	if err != nil || active == list[0].ID {
		return err
	}
	// The event is not published: at this hour nothing is listening yet, and the window reads the
	// snapshot on its way up rather than waiting to be told.
	return u.store.SetActiveWorkspace(ctx, list[0].ID)
}

// Snapshot is every workspace and the pointer to the one on screen — the whole of what the switcher
// draws, in one answer — with what each of them holds, because the window draws the counts in the
// same rows as the names.
func (u *UseCase) Snapshot(ctx context.Context) (domain.WorkspaceState, error) {
	list, err := u.store.Workspaces(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	active, err := u.store.ActiveWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	counts, err := u.store.Counts(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	return domain.WorkspaceState{
		Workspaces: list, ActiveID: active, PersonalID: domain.WorkspacePersonalID, Counts: counts,
	}, nil
}

// Switch points the app at another workspace. Nothing is asked first and nothing is confirmed: the
// data below the window changes, which is what the switcher is for.
func (u *UseCase) Switch(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := u.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	if err := u.store.SetActiveWorkspace(ctx, target.ID); err != nil {
		return domain.WorkspaceState{}, err
	}
	u.notifier.Publish(TopicChanged, Changed{Workspace: target})
	return u.Snapshot(ctx)
}

// Create makes a workspace and leaves the app where it was: a new space is empty, and moving the
// window into it without being asked would look like the app had lost its data. The switcher's
// caller switches explicitly — which is what the design's menu does.
func (u *UseCase) Create(ctx context.Context, in CreateInput) (Created, error) {
	name, err := u.freeName(ctx, "", in.Name)
	if err != nil {
		return Created{}, err
	}
	kind, err := cleanKind(in.Kind)
	if err != nil {
		return Created{}, err
	}
	// Nobody is invited into a space of somebody's own: it holds one person, and that is the whole of
	// what it means. The server refuses it too — this is the shape of the answer, not the rule.
	if kind != domain.WorkspaceTeam && len(in.Invites) > 0 {
		return Created{}, domain.Refuse(domain.CodeWorkspacePersonal, domain.ErrNotAllowed, nil)
	}

	made := domain.NewWorkspace(u.ids(), name, kind,
		strings.TrimSpace(in.Color), time.Now().UnixMilli())
	if err := u.store.SaveWorkspace(ctx, made); err != nil {
		return Created{}, err
	}

	// A team's space is made in the account before anybody is invited into it, and it is made now
	// rather than by the engine's next round: the form that made it is showing people to invite, and
	// there is nothing to invite them into until the account has the space.
	//
	// A space that could not be made there is not kept. What would be left is a row that never links,
	// drawn in the switcher for ever — and the person asked for something that is an act of the
	// account in the first place. The answer that was lost is the one case that repairs itself: the
	// next round finds the space in the account and mirrors it back.
	if kind == domain.WorkspaceTeam {
		if err := u.linker.LinkNow(ctx, made.ID); err != nil {
			_ = u.store.DeleteWorkspace(ctx, made.ID)
			return Created{}, err
		}
	}

	out := Created{
		WorkspaceState: domain.WorkspaceState{},
		Invites:        []InviteOutcome{},
	}
	for _, one := range in.Invites {
		address := strings.TrimSpace(one.Email)
		if address == "" {
			continue
		}
		if err := u.inviter.Invite(ctx, made.ID, address, one.RoleID); err != nil {
			out.Invites = append(out.Invites, InviteOutcome{
				Email:   address,
				Failure: domain.FailureOf(err, domain.CodeServerRefused),
			})
			continue
		}
		out.Invites = append(out.Invites, InviteOutcome{Email: address})
	}

	if out.WorkspaceState, err = u.Snapshot(ctx); err != nil {
		return Created{}, err
	}
	return out, nil
}

// cleanKind is what the form chose, kept to the two words there are: an empty one is a space that
// is the person's own, and anything else is a build sending a word this one does not have. It is
// refused rather than taken as one of the two: a space made with the wrong kind is a space somebody
// has to notice and rename.
func cleanKind(kind string) (domain.WorkspaceKind, error) {
	if kind == "" {
		return domain.WorkspacePersonal, nil
	}
	held := domain.WorkspaceKind(kind)
	if !held.Valid() {
		return "", domain.Refuse(domain.CodeUnknownField, nil, domain.Args{"field": "kind"})
	}
	return held, nil
}

func (u *UseCase) Update(
	ctx context.Context,
	id string,
	patch Patch,
) (domain.WorkspaceState, error) {
	current, err := u.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	// The space the app is born with keeps its name and what it is for: the name is how the account's
	// own space is found across the link, and what it is for is what makes it that space. Its colour
	// is its own to change — nothing is found by a colour.
	if domain.IsPersonal(current.ID) && (patch.Name != nil || patch.Kind != nil) {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspacePermanent,
			domain.ErrNotAllowed, nil)
	}

	if patch.Name != nil {
		name, err := u.freeName(ctx, current.ID, *patch.Name)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		current.Name = name
	}
	if patch.Color != nil {
		current.Color = strings.TrimSpace(*patch.Color)
	}
	if patch.Kind != nil {
		kind, err := cleanKind(*patch.Kind)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		// What a space is for decides who may be in it, so it is the account's answer and not this
		// device's: only the server can say whether anybody else is in the space, and it is the one
		// that refuses. A space that is not in the account has nobody else in it by definition — there
		// is no account for anybody to have been invited through — so the word is written here.
		if current.RemoteID != "" {
			if err := u.remote.ChangeKind(ctx, current.RemoteID, kind); err != nil {
				return domain.WorkspaceState{}, err
			}
		}
		current.Kind = kind
	}
	current.UpdatedAt = time.Now().UnixMilli()

	if err := u.store.SaveWorkspace(ctx, current); err != nil {
		return domain.WorkspaceState{}, err
	}
	if active, err := u.store.ActiveWorkspace(ctx); err == nil && active == current.ID {
		u.notifier.Publish(TopicChanged, Changed{Workspace: current})
	}
	return u.Snapshot(ctx)
}

// Delete removes a workspace and everything in it. The last one is refused: every other answer the
// app gives leans on there being a space to keep things in, and which one it is does not matter —
// the row the app is born with goes like any other. Deleting the one on screen moves the pointer to
// the oldest that is left, so the window is never left pointing at a row that is gone.
//
// A space that is also in the account goes from there first. The other order would leave a device
// with no row and an account with a space: the next round reads the account's list, finds nothing
// here paired with it, and makes it again — a space somebody deleted coming back empty.
func (u *UseCase) Delete(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := u.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	// The space the app is born with does not go. It is not a question of how many are left — it is
	// always there, and the app always has somewhere to keep things — but of what this space is: a
	// device's own space in the account is paired with it, and there is no second one to pair with.
	if domain.IsPersonal(target.ID) {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspacePermanent,
			domain.ErrNotAllowed, nil)
	}

	if target.RemoteID != "" {
		if err := u.remote.Delete(ctx, target.RemoteID); err != nil && !gone(err) {
			return domain.WorkspaceState{}, err
		}
	}
	return u.drop(ctx, target)
}

// Leave takes this person out of a space somebody else's. The account is told first and the row
// goes after it: a device that dropped the space and then failed to leave would find the space
// still there on its next round, the person still in it, and make it again.
//
// It is separate from ending one because they are different acts with different rights: whoever
// made a space may end it for everybody, and anybody in it may step out, a member who may not end
// the space having no other way to leave one.
func (u *UseCase) Leave(ctx context.Context, id string) (domain.WorkspaceState, error) {
	target, err := u.store.Workspace(ctx, id)
	if err != nil {
		return domain.WorkspaceState{}, err
	}
	if domain.IsPersonal(target.ID) {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspacePermanent,
			domain.ErrNotAllowed, nil)
	}
	if target.RemoteID == "" {
		return domain.WorkspaceState{}, domain.Refuse(domain.CodeWorkspaceMissing,
			domain.ErrNotFound, nil)
	}

	if err := u.remote.Leave(ctx, target.RemoteID); err != nil && !gone(err) {
		return domain.WorkspaceState{}, err
	}
	return u.drop(ctx, target)
}

// drop removes a space from this device and answers where the window lands.
func (u *UseCase) drop(
	ctx context.Context,
	target domain.Workspace,
) (domain.WorkspaceState, error) {
	// Which workspace is on screen is read before the row goes: afterwards the pointer names something
	// that is no longer there, and the answer to "was this the one being shown" is gone with it.
	active, err := u.store.ActiveWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceState{}, err
	}

	if err := u.store.DeleteWorkspace(ctx, target.ID); err != nil {
		return domain.WorkspaceState{}, err
	}

	// The successor is picked here rather than left to the store's fallback: a reader of this call
	// should see which space the window lands on, and the fallback keeps its one job — a pointer that
	// went stale behind the app's back.
	if active == target.ID {
		remaining, err := u.store.Workspaces(ctx)
		if err != nil {
			return domain.WorkspaceState{}, err
		}
		if len(remaining) > 0 {
			if err := u.store.SetActiveWorkspace(ctx, remaining[0].ID); err != nil {
				return domain.WorkspaceState{}, err
			}
			u.notifier.Publish(TopicChanged, Changed{Workspace: remaining[0]})
		}
	}
	return u.Snapshot(ctx)
}

// gone reports an answer of "there is no such space" from the account. For an act whose point is
// that the space is no longer the person's, it is the act already done: asking about a row the
// server has removed is not a failure to report.
func gone(err error) bool {
	switch domain.CodeOf(err) {
	case domain.CodeWorkspaceMissing, domain.CodeNotFound:
		return true
	}
	return false
}

// freeName is a name a space may carry: a clean one that no other space wears. The space the app
// is born with has no name of its own — the window words it — so it takes none away.
func (u *UseCase) freeName(ctx context.Context, except string, raw string) (string, error) {
	name, err := domain.CleanName(raw, maxNameLength)
	if err != nil {
		return "", err
	}
	all, err := u.store.Workspaces(ctx)
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
