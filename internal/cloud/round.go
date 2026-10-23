package cloud

import (
	"cmp"
	"context"
	"schemo/internal/domain"
	"slices"
	"strings"
)

func (s *Sync) round(ctx context.Context, only []string) error {
	return s.exclusive(ctx, func() error { return s.exchangeRound(ctx, only) })
}

func (s *Sync) exchangeRound(ctx context.Context, only []string) error {
	s.beginRound()
	caller, err := s.session.Caller(ctx)
	if err != nil {
		s.setSignedIn(false)
		if !isRefusal(err) {
			return err
		}
		s.finishRound(err)
		// A refusal ends the round quietly: asking the same question on a timer would be refused
		// again. A server that did not answer is not a refusal (the account is fine and the weather
		// is not), and the loop is where backing off for it lives.
		if domain.CodeOf(err) == domain.CodeServerUnreachable {
			return err
		}
		return nil
	}
	s.setSignedIn(true)

	limits, err := s.serverLimits(ctx, caller)
	if err != nil {
		// The round could not even learn what the server allows, so it goes to the loop as the
		// failure it is: that is what the backoff is for, and the row counts the seconds by it.
		s.finishRound(err)
		return err
	}

	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return err
	}

	var discoveryErr error
	if only == nil {
		var discovered bool
		discovered, discoveryErr = s.reconcile(ctx, caller, workspaces)
		if discovered {
			if workspaces, err = s.store.SyncWorkspaces(ctx); err != nil {
				return err
			}
		}
	}

	alive := s.activeWorkspaces(workspaces)
	if only != nil {
		alive = slices.DeleteFunc(alive, func(workspace domain.SyncWorkspace) bool {
			return !slices.Contains(only, workspace.ID)
		})
	}

	var first error
	for _, workspace := range alive {
		if err := s.exchangeWorkspace(ctx, caller, workspace, limits); err != nil {
			s.recordFailure(workspace, err)
			if first == nil {
				first = err
			}
		}
	}

	if only == nil {
		s.watch(ctx, alive)
	}

	if first == nil {
		first = discoveryErr
	}

	s.finishRound(first)
	return first
}

// exchangeWorkspace is one workspace's half of a round: what this device owes it and what it owes this
// device. A member with a reader's role is the ordinary case of a workspace read and not written, and
// the account's word about it was adopted at the top of the round, so the workspace is pulled and not
// pushed into: work sent there would only come back refused.
func (s *Sync) exchangeWorkspace(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
	limits domain.Limits,
) error {
	if !workspace.Exchangeable() {
		return nil
	}

	var pushErr error
	if !workspace.ReadOnly {
		pushErr = s.push(ctx, caller, workspace, limits)
	}
	pullErr := s.pull(ctx, caller, workspace)

	var historyErr error
	if pullErr == nil {
		historyErr = s.pullHistory(ctx, caller, workspace)
	}
	if err := cmp.Or(pushErr, pullErr, historyErr); err != nil {
		return err
	}
	return s.store.RecordRound(ctx, workspace.ID, domain.SyncSynced, s.now().UnixMilli(), "", 0)
}

func (s *Sync) serverLimits(ctx context.Context, caller domain.Caller) (domain.Limits, error) {
	s.mu.Lock()
	known := s.taken
	s.mu.Unlock()
	if known.MaxPushBytes > 0 {
		return known, nil
	}

	limits, err := s.server.Limits(ctx, caller)
	if err != nil {
		return domain.Limits{}, err
	}
	s.mu.Lock()
	s.taken = limits
	s.mu.Unlock()
	return limits, nil
}

func isRefusal(err error) bool {
	switch domain.CodeOf(err) {
	case domain.CodeNotSignedIn, domain.CodeServerUnreachable, domain.CodeServerRefused,
		domain.CodeWorkspaceMissing, domain.CodeNotFound, domain.CodeNotAllowed:
		return true
	}
	return domain.AsFailure(err) != nil
}

// reconcile synchronizes local workspaces with remote workspaces and updates context on account switches.
// It reports whether it changed anything, alongside an error too: what was done before the failure
// is still done, and the round reads the workspaces again for it.
func (s *Sync) reconcile(
	ctx context.Context,
	caller domain.Caller,
	workspaces []domain.SyncWorkspace,
) (bool, error) {
	if allLinkedWorkspacesPaused(workspaces) {
		return false, nil
	}

	account, err := s.server.Workspaces(ctx, caller)
	if err != nil {
		return false, err
	}
	remote := account.Workspaces

	who, err := s.resolveIdentity(ctx, caller)
	if err != nil {
		return false, err
	}
	mine := who.Key
	s.rememberAccountWorkspaces(remote, mine)

	again, err := s.rebindIfRecreated(ctx, who)
	if err != nil {
		return false, err
	}
	released, err := s.releaseOtherAccounts(ctx, mine)
	if err != nil {
		return false, err
	}
	if again || released > 0 {
		if workspaces, err = s.store.SyncWorkspaces(ctx); err != nil {
			return false, err
		}
	}

	gone, err := s.discardRemovedWorkspaces(ctx, workspaces, remote, mine)
	if err != nil {
		return false, err
	}
	changed := gone || again || released > 0

	pairs := newPairing(workspaces, remote)
	steps := []func() (bool, error){
		func() (bool, error) { return s.adoptKinds(ctx, pairs) },
		func() (bool, error) { return s.pairPersonal(ctx, pairs, mine) },
		func() (bool, error) { return s.mirrorRemoteWorkspaces(ctx, pairs, mine) },
		func() (bool, error) { return s.adoptAccountContext(ctx, mine) },
	}
	for _, step := range steps {
		did, err := step()
		changed = changed || did
		if err != nil {
			return changed, err
		}
	}

	made, refused, err := s.createWaiting(ctx, caller, account.OwnOrgID, pairs, mine)
	changed = changed || made
	if err != nil {
		return changed, err
	}

	if changed {
		s.noteContentArrived()
	}
	return changed, refused
}

// releaseOtherAccounts lets go of what an account this device was signed in to before has left in
// it, and says how many rows that was. Nothing is let go of the account that is signed in now.
func (s *Sync) releaseOtherAccounts(ctx context.Context, mine string) (int, error) {
	before, err := s.store.Context(ctx)
	if err != nil || before == "" || before == mine {
		return 0, err
	}
	return s.store.Release(ctx, before)
}

// pairing is what a round of reconcile knows of which local workspace stands for which one in the
// account, kept up to date as the round pairs them.
type pairing struct {
	workspaces []domain.SyncWorkspace
	remote     []domain.RemoteWorkspace
	// paired holds the account's workspaces that have a local one; linked, the local ones linked now.
	paired map[string]bool
	linked map[string]bool
}

func newPairing(workspaces []domain.SyncWorkspace, remote []domain.RemoteWorkspace) *pairing {
	p := &pairing{
		workspaces: workspaces,
		remote:     remote,
		paired:     make(map[string]bool, len(workspaces)),
		linked:     make(map[string]bool),
	}
	for _, workspace := range workspaces {
		if workspace.RemoteID != "" {
			p.paired[workspace.RemoteID] = true
		}
	}
	return p
}

// unpaired is the workspaces with nobody to be paired with: the local ones the account does not know
// yet and the account's ones that nothing here stands for.
func (p *pairing) unpaired() ([]domain.SyncWorkspace, []domain.RemoteWorkspace) {
	here := make([]domain.SyncWorkspace, 0, len(p.workspaces))
	for _, workspace := range p.workspaces {
		if workspace.RemoteID == "" && !p.linked[workspace.ID] {
			here = append(here, workspace)
		}
	}
	there := make([]domain.RemoteWorkspace, 0, len(p.remote))
	for _, one := range p.remote {
		if !p.paired[one.ID] {
			there = append(there, one)
		}
	}
	return here, there
}

// adoptKinds takes what the account says about a paired workspace, what it is for and what this person
// may do in it, as the server's word: a role taken away arrives as a reader's answer, and the round
// that reads it stops pushing what the workspace would refuse.
func (s *Sync) adoptKinds(ctx context.Context, p *pairing) (bool, error) {
	did := false
	for _, workspace := range p.workspaces {
		if workspace.RemoteID == "" {
			continue
		}
		for _, one := range p.remote {
			if one.ID != workspace.RemoteID {
				continue
			}
			moved, err := s.store.Adopt(ctx, workspace.ID, one)
			if err != nil {
				return did, err
			}
			did = did || moved
		}
	}
	return did, nil
}

// pairPersonal links the workspace the app is born with to the account's own workspace, when both are
// still waiting for it.
func (s *Sync) pairPersonal(ctx context.Context, p *pairing, mine string) (bool, error) {
	here, there := p.unpaired()
	personal := findWorkspace(here, domain.WorkspacePersonalID)
	if personal == nil {
		return false, nil
	}
	twin, found := personalWorkspaceOfAccount(there)
	if !found {
		return false, nil
	}
	if err := s.store.Link(ctx, personal.ID, twin, mine); err != nil {
		return false, err
	}
	p.paired[twin.ID] = true
	p.linked[personal.ID] = true
	return true, nil
}

// mirrorRemoteWorkspaces makes here the workspaces that are only in the account.
func (s *Sync) mirrorRemoteWorkspaces(ctx context.Context, p *pairing, mine string) (bool, error) {
	_, there := p.unpaired()
	did := false
	for _, one := range there {
		if err := s.store.Mirror(ctx, one, mine); err != nil {
			return did, err
		}
		p.paired[one.ID] = true
		did = true
	}
	return did, nil
}

// createWaiting makes in the account the workspaces that are only here. A refusal of the account is
// told to the window and the others still go on: the first is `refused`, and `err` is what failed
// here.
func (s *Sync) createWaiting(
	ctx context.Context,
	caller domain.Caller,
	orgID string,
	p *pairing,
	mine string,
) (made bool, refused error, err error) {
	for _, workspace := range p.workspaces {
		if p.linked[workspace.ID] || !isWaitingToLink(workspace) || isForeign(workspace, mine) {
			continue
		}
		if workspace.Name == "" {
			held, err := s.store.Holds(ctx, workspace.ID)
			if err != nil {
				return made, refused, err
			}
			if !held {
				continue
			}
		}

		remoteID := workspace.RemoteID
		if remoteID == "" {
			remoteID = s.ids()
			if err := s.store.MarkLinkPending(ctx, workspace.ID, remoteID, mine); err != nil {
				return made, refused, err
			}
			made = true
		}

		created, err := s.server.Create(ctx, caller, orgID, remoteID,
			workspace.Name, workspace.Color, workspace.Kind)
		if err != nil {
			s.recordFailure(workspace, err)
			if refused == nil {
				refused = err
			}
			continue
		}
		if err := s.store.Link(ctx, workspace.ID, created, mine); err != nil {
			return made, refused, err
		}
		s.noteSeen(created.ID)
		made = true
	}
	return made, refused, nil
}

// LinkNow immediately creates a remote workspace and links it to a local workspace.
//
//wails:ignore
func (s *Sync) LinkNow(ctx context.Context, workspaceID string) error {
	return s.exclusive(ctx, func() error { return s.linkNow(ctx, workspaceID) })
}

func (s *Sync) linkNow(ctx context.Context, workspaceID string) error {
	caller, err := s.session.Caller(ctx)
	if err != nil {
		return err
	}
	account, err := s.server.Workspaces(ctx, caller)
	if err != nil {
		return err
	}
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return err
	}

	var workspace domain.SyncWorkspace
	found := false
	for _, one := range workspaces {
		if one.ID == workspaceID {
			workspace, found = one, true
			break
		}
	}
	if !found {
		return domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	mine, err := s.accountKey(ctx, caller)
	if err != nil {
		return err
	}
	if isForeign(workspace, mine) {
		return domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	remoteID := workspace.RemoteID
	if remoteID == "" {
		remoteID = s.ids()
		if err := s.store.MarkLinkPending(ctx, workspace.ID, remoteID, mine); err != nil {
			return err
		}
	}

	made, err := s.server.Create(ctx, caller, account.OwnOrgID, remoteID,
		workspace.Name, workspace.Color, workspace.Kind)
	if err != nil {
		return err
	}
	if err := s.store.Link(ctx, workspace.ID, made, mine); err != nil {
		return err
	}
	s.noteSeen(made.ID)

	s.noteContentArrived()
	s.Wake()
	return nil
}

func (s *Sync) discardRemovedWorkspaces(
	ctx context.Context,
	workspaces []domain.SyncWorkspace,
	remote []domain.RemoteWorkspace,
	mine string,
) (bool, error) {
	if len(remote) == 0 {
		return false, nil
	}
	seen := make(map[string]bool, len(remote))
	for _, one := range remote {
		seen[one.ID] = true
	}

	gone := false
	for _, workspace := range workspaces {
		if workspace.RemoteID == "" || workspace.LinkPending || seen[workspace.RemoteID] ||
			!belongsToAccount(workspace, mine) {
			continue
		}
		if err := s.store.Discard(ctx, workspace.ID); err != nil {
			return gone, err
		}
		gone = true
	}
	if !gone {
		return false, nil
	}

	return true, s.store.EnsureOne(ctx)
}

// resolveIdentity resolves current caller credentials and unique account key.
func (s *Sync) resolveIdentity(ctx context.Context, caller domain.Caller) (domain.Identity, error) {
	me, err := s.session.Me(ctx)
	if err != nil {
		return domain.Identity{}, err
	}
	if strings.TrimSpace(me.Email) == "" || me.UserID == "" {
		return domain.Identity{}, domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	return domain.Identity{Key: domain.AccountKey(caller.Address, me.Email), UserID: me.UserID}, nil
}

func (s *Sync) accountKey(ctx context.Context, caller domain.Caller) (string, error) {
	who, err := s.resolveIdentity(ctx, caller)
	return who.Key, err
}

// rebindIfRecreated handles account re-creation cases on the server (e.g. database resets).
func (s *Sync) rebindIfRecreated(ctx context.Context, who domain.Identity) (bool, error) {
	seen, known, err := s.store.Seen(ctx, who.Key)
	if err != nil {
		return false, err
	}
	switch {
	case !known:
		return false, s.store.Remember(ctx, who.Key, who.UserID)
	case seen == who.UserID:
		return false, nil
	default:
		return true, s.store.Rebind(ctx, who.Key, who.UserID)
	}
}

// adoptAccountContext sets active local account context once workspaces are available.
func (s *Sync) adoptAccountContext(ctx context.Context, key string) (bool, error) {
	held, err := s.store.Context(ctx)
	if err != nil || held == key {
		return false, err
	}
	return true, s.store.SetContext(ctx, key)
}

// noteSeen adds a workspace this device just created to the account's list, so the same round
// exchanges it instead of taking it for one removed elsewhere.
func (s *Sync) noteSeen(remoteID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen != nil {
		s.seen[remoteID] = true
	}
}

func (s *Sync) rememberAccountWorkspaces(remote []domain.RemoteWorkspace, mine string) {
	seen := make(map[string]bool, len(remote))
	for _, one := range remote {
		seen[one.ID] = true
	}

	s.mu.Lock()
	s.seen, s.known, s.mine = seen, true, mine
	s.mu.Unlock()
}
