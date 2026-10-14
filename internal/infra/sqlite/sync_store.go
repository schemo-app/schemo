package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"schemo/internal/domain"
)

// Spaces returns all local workspaces and their sync state.
func (s *Store) Spaces(ctx context.Context) ([]domain.SyncSpace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.name, w.color, w.kind, w.remote_workspace_id, w.remote_account,
        w.link_pending, ifnull(y.state, 'local'),
        ifnull(y.last_sync_seq, 0), ifnull(y.last_history_seq, 0),
        ifnull(y.last_synced_at, 0), ifnull(y.error, ''), ifnull(y.attempts, 0), w.access_json
   FROM workspaces w
   LEFT JOIN workspace_sync y ON y.workspace_id = w.id
  ORDER BY w.position`)
	if err != nil {
		return nil, fmt.Errorf("reading the spaces: %w", err)
	}
	defer rows.Close()

	out := []domain.SyncSpace{}
	for rows.Next() {
		var space domain.SyncSpace
		var state, failed, access string
		if err := rows.Scan(&space.ID, &space.Name, &space.Color, &space.Kind, &space.RemoteID,
			&space.RemoteAccount, &space.LinkPending, &state, &space.Cursor,
			&space.HistoryCursor, &space.SyncedAt, &failed, &space.Attempts, &access); err != nil {
			return nil, fmt.Errorf("reading the spaces: %w", err)
		}
		space.State, space.Failed = domain.SyncState(state), domain.Code(failed)
		space.ReadOnly = !accessOf(access).Write
		out = append(out, space)
	}
	return out, rows.Err()
}

// Link binds a local workspace to a remote server workspace and re-queues state.
func (s *Store) Link(
	ctx context.Context,
	spaceID string,
	remote domain.RemoteSpace,
	account string,
) error {
	return s.inTx(ctx, "linking the space", func(tx *writeTx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE workspaces
			   SET remote_workspace_id = ?, version = ?, updated_at = ?, link_pending = 0,
			       name = CASE WHEN name = '' THEN ? ELSE name END,
			       color = CASE WHEN color = '' THEN ? ELSE color END,
			       kind = ?, remote_account = ?, access_json = ?
			 WHERE id = ?`,
			remote.ID, remote.Version, time.Now().UnixMilli(), remote.Name, remote.Colour,
			kindOr(remote.Kind), account, accessData(remote.Access), spaceID)
		if err != nil {
			return fmt.Errorf("linking workspace %s: %w", spaceID, err)
		}
		return offerAnew(ctx, tx, spaceID)
	})
}

// Mirror creates a local representation of a remote workspace.
func (s *Store) Mirror(ctx context.Context, remote domain.RemoteSpace, account string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, kind, color, position, created_at, updated_at, version,
		                        remote_workspace_id, remote_account, access_json)
		SELECT ?, ?, ?, ?, (SELECT coalesce(max(position), -1) + 1 FROM workspaces), ?, ?, ?, ?,
		       ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM workspaces WHERE remote_workspace_id = ?)`,
		newID(), remote.Name, kindOr(remote.Kind), remote.Colour, now(), now(), remote.Version,
		remote.ID, account, accessData(remote.Access), remote.ID)
	if err != nil {
		return fmt.Errorf("mirroring workspace %s: %w", remote.ID, err)
	}
	return nil
}

// Seen returns the stored user ID for an account key and whether it exists.
func (s *Store) Seen(ctx context.Context, key string) (string, bool, error) {
	return s.Setting(ctx, seenKey(key))
}

// Remember stores the server's user ID for an account key.
func (s *Store) Remember(ctx context.Context, key, userID string) error {
	return s.SaveSetting(ctx, seenKey(key), userID)
}

func seenKey(key string) string { return domain.SettingAccountUser + key }

// Rebind detaches spaces of an account key and associates them with a new userID.
func (s *Store) Rebind(ctx context.Context, key, userID string) error {
	return s.inTx(ctx, "taking the spaces back", func(tx *writeTx) error {
		if err := detach(ctx, tx, key); err != nil {
			return err
		}
		return saveSettingIn(ctx, tx, seenKey(key), userID)
	})
}

func detach(ctx context.Context, tx *writeTx, key string) error {
	ids, err := idsOf(ctx, tx,
		`SELECT id FROM workspaces WHERE remote_account = ? AND remote_workspace_id <> ''`, key)
	if err != nil {
		return fmt.Errorf("reading the spaces of the account: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE workspaces
			   SET remote_workspace_id = '', remote_account = '', link_pending = 0, access_json = ''
			 WHERE id = ?`, id); err != nil {
			return fmt.Errorf("detaching workspace %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE workspace_sync
			   SET state = 'local', last_history_seq = 0, error = ''
			 WHERE workspace_id = ?`, id); err != nil {
			return fmt.Errorf("resetting the history cursor of %s: %w", id, err)
		}
		if err := forgetVersions(ctx, tx, id); err != nil {
			return err
		}
		if err := startOver(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// forgetVersions sets every row of a space back to a version nobody has agreed on. The versions
// were the old account's, and a push made at one of them to a server that holds nothing of the row
// is a change of a version nobody has; at none, it is a creation.
func forgetVersions(ctx context.Context, tx *writeTx, spaceID string) error {
	for _, statement := range []string{
		`UPDATE workspaces SET version = ? WHERE id = ?`,
		`UPDATE collections SET version = ? WHERE workspace_id = ?`,
		`UPDATE collection_nodes SET version = ?
		  WHERE collection_id IN (SELECT id FROM collections WHERE workspace_id = ?)`,
		`UPDATE environments SET version = ? WHERE workspace_id = ?`,
		`UPDATE variables SET version = ? WHERE workspace_id = ?`,
		`UPDATE jar_cookies SET version = ? WHERE workspace_id = ?`,
		`UPDATE jar_domains SET version = ? WHERE workspace_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, versionUnagreed, spaceID); err != nil {
			return fmt.Errorf("forgetting the versions of %s: %w", spaceID, err)
		}
	}
	return nil
}

// Wipe removes every space of an account key with all that stands in them, and forgets the key as
// the one this installation works as: what is left is the work that never belonged to an account.
// Nothing is returned when the key names nothing — a device that never held that account's spaces
// has nothing to erase, and that is not a failure.
func (s *Store) Wipe(ctx context.Context, key string) (int, error) {
	if key == "" {
		return 0, nil
	}
	let := 0
	err := s.inTx(ctx, "wiping an account's spaces", func(tx *writeTx) error {
		gone, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE remote_account = ?`, key)
		if err != nil {
			return fmt.Errorf("deleting the spaces of %s: %w", key, err)
		}
		removed, err := gone.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting the spaces of %s: %w", key, err)
		}
		let = int(removed)

		// The context points at spaces that are gone; another account's sign-in sets its own, and an
		// address left here would only make the next reading of it a puzzle.
		var held string
		err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`,
			domain.SettingAccountContext).Scan(&held)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("reading the account context: %w", err)
		case held != key:
			return nil
		}
		return saveSettingIn(ctx, tx, domain.SettingAccountContext, "")
	})
	if err != nil {
		return 0, err
	}
	if let > 0 {
		if _, err := s.db.ExecContext(ctx, vacuum); err != nil {
			return 0, fmt.Errorf("vacuuming after wiping %s: %w", key, err)
		}
	}
	return let, nil
}

// Release removes fully synchronized workspaces belonging to an account key.
func (s *Store) Release(ctx context.Context, key string) (int, error) {
	ids, err := s.owedNothing(ctx, key)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.DeleteWorkspace(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (s *Store) owedNothing(ctx context.Context, key string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id FROM workspaces w
		 WHERE w.remote_account = ? AND w.remote_workspace_id <> '' AND w.link_pending = 0
		   AND NOT EXISTS (SELECT 1 FROM sync_queue q WHERE q.workspace_id = w.id)`, key)
	if err != nil {
		return nil, fmt.Errorf("reading the spaces of %s that owe nothing: %w", key, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading the spaces of %s that owe nothing: %w", key, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Adopt takes what the account says about a space this device is already paired with: what it is
// for, and what this person may do in it. Both are the server's answers rather than this device's,
// and they arrive together. It says whether the row moved, which is what the windows redraw on.
func (s *Store) Adopt(
	ctx context.Context, spaceID string, remote domain.RemoteSpace,
) (bool, error) {
	kind, access := kindOr(remote.Kind), accessData(remote.Access)
	moved := false
	err := s.inTx(ctx, "keeping workspace "+spaceID+" in step", func(tx *writeTx) error {
		done, err := tx.ExecContext(ctx,
			`UPDATE workspaces SET kind = ?, access_json = ?
			  WHERE id = ? AND (kind <> ? OR access_json <> ?)`,
			kind, access, spaceID, kind, access)
		if err != nil {
			return err
		}
		changed, _ := done.RowsAffected()
		moved = changed > 0
		if !moved || accessOf(access).Write {
			return nil
		}
		// A role that no longer writes is a space nothing is pushed to: what was queued for it would
		// wait for ever, and the status line would go on saying so.
		_, err = tx.ExecContext(ctx, `DELETE FROM sync_queue WHERE workspace_id = ?`, spaceID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("keeping workspace %s in step: %w", spaceID, err)
	}
	return moved, nil
}

// Discard permanently deletes a workspace and its associated entities.
func (s *Store) Discard(ctx context.Context, spaceID string) error {
	return s.DeleteWorkspace(ctx, spaceID)
}

// EnsureOne guarantees that at least one default workspace exists locally.
func (s *Store) EnsureOne(ctx context.Context) error {
	visible, args, err := s.visible(ctx)
	if err != nil {
		return err
	}
	var held int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE `+visible,
		args...).Scan(&held); err != nil {
		return fmt.Errorf("reading whether there are spaces: %w", err)
	}
	if held > 0 {
		return nil
	}

	id := domain.WorkspacePersonalID
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE id = ?`, id).
		Scan(&taken); err != nil {
		return fmt.Errorf("reading whether the born space is here: %w", err)
	}
	if taken > 0 {
		id = newID()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, kind, color, active_environment_id, position,
		                        created_at, updated_at)
		VALUES (?, '', 'personal', '', '', 0, ?, ?)`, id, now(), now())
	if err != nil {
		return fmt.Errorf("making a space to keep things in: %w", err)
	}
	return nil
}

// kindOr validates a workspace kind, returning WorkspacePersonal as fallback.
func kindOr(kind domain.WorkspaceKind) domain.WorkspaceKind {
	if !kind.Valid() {
		return domain.WorkspacePersonal
	}
	return kind
}

// MarkLinkPending records pending workspace remote ID and account assignment.
func (s *Store) MarkLinkPending(ctx context.Context, spaceID, remoteID, account string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET link_pending = 1, remote_workspace_id = ?, remote_account = ?
		  WHERE id = ?`,
		remoteID, account, spaceID); err != nil {
		return fmt.Errorf("naming the space %s will be made under: %w", spaceID, err)
	}
	return nil
}

// Holds reports whether a workspace contains active collections or environments.
func (s *Store) Holds(ctx context.Context, spaceID string) (bool, error) {
	var held int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM collections WHERE workspace_id = ? AND deleted_at IS NULL)
		    OR EXISTS (SELECT 1 FROM environments WHERE workspace_id = ? AND deleted_at IS NULL)`,
		spaceID, spaceID).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("reading what %s holds: %w", spaceID, err)
	}
	return held != 0, nil
}

// Owed retrieves pending synchronization changes capped by maxBytes: a batch the server would
// refuse is not one to build.
func (s *Store) Owed(
	ctx context.Context,
	space domain.SyncSpace,
	maxBytes int64,
) ([]domain.PendingChange, error) {
	if space.RemoteID == "" {
		return nil, nil
	}

	waiting, err := s.owed(ctx, space.ID)
	if err != nil {
		return nil, err
	}
	if waiting, err = s.parentsFirst(ctx, space.ID, waiting); err != nil {
		return nil, err
	}

	// What the person lets out is read once for the whole push: the payloads below are built from
	// it, and a round is one answer rather than a page of them.
	scopes, err := s.SyncScopes(ctx)
	if err != nil {
		return nil, err
	}

	changes := make([]domain.PendingChange, 0, len(waiting))
	weighed := int64(0)
	for _, entry := range waiting {
		entity, found, err := entityFor(ctx, s.db, space, entry, scopes)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := s.forget(ctx, s.db, entry.QueueID); err != nil {
				return nil, err
			}
			continue
		}

		size := int64(len(entity.Payload)) + int64(len(entity.ID)) + 64
		if len(changes) > 0 && maxBytes > 0 && weighed+size > maxBytes {
			// What is left over stays where it is: a batch is one answer, and claiming a row that is
			// not in it would leave it marked "sending" until the next start.
			break
		}
		taken, err := s.claim(ctx, entry)
		if err != nil {
			return nil, err
		}
		if !taken {
			continue
		}
		weighed += size

		changes = append(changes, domain.PendingChange{
			QueueID:     entry.QueueID,
			SpaceID:     space.ID,
			Action:      entry.Action,
			OperationID: entry.OperationID,
			Attempts:    entry.Attempts,
			Entity:      entity,
		})
	}

	return changes, nil
}

// parentsFirst puts a batch in the order the server can take it: by kind, as a push is made, and
// among the collections by how deep each stands in its tree. A child sent before its parent is
// refused and waits out a backoff, and the order is made before the cut by size so that what the
// cut leaves for the next round is the later and deeper, never a parent whose child went out.
func (s *Store) parentsFirst(
	ctx context.Context,
	spaceID string,
	waiting []queueRow,
) ([]queueRow, error) {
	nested := slices.ContainsFunc(waiting, func(one queueRow) bool {
		return one.Kind == domain.EntityCollection
	})
	depths := map[string]int{}
	if nested {
		rows, err := s.db.QueryContext(ctx,
			`WITH RECURSIVE tree(id, depth) AS (
			   SELECT id, 0 FROM collections WHERE workspace_id = ? AND parent_id IS NULL
			   UNION ALL
			   SELECT c.id, tree.depth + 1 FROM collections c JOIN tree ON c.parent_id = tree.id)
			 SELECT id, depth FROM tree`, spaceID)
		if err != nil {
			return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", spaceID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var depth int
			if err := rows.Scan(&id, &depth); err != nil {
				return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", spaceID, err)
			}
			depths[id] = depth
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", spaceID, err)
		}
	}

	slices.SortStableFunc(waiting, func(a, b queueRow) int {
		if byKind := cmp.Compare(domain.Depth(a.Kind), domain.Depth(b.Kind)); byKind != 0 {
			return byKind
		}
		return cmp.Compare(depths[a.EntityID], depths[b.EntityID])
	})
	return waiting, nil
}

// claim marks one entry as in flight, if it is still the one the payload was built for. An edit
// that landed after the entry was read gave it another operation id in the same transaction as the
// row, so the payload in hand is out of date and the entry is left for the next round. An entry
// that is in flight is never changed again: a later edit makes a second entry beside it.
func (s *Store) claim(ctx context.Context, entry queueRow) (bool, error) {
	done, err := s.db.ExecContext(ctx,
		`UPDATE sync_queue SET status = 'sending'
		  WHERE id = ? AND operation_id = ? AND status IN ('pending', 'failed')`,
		entry.QueueID, entry.OperationID)
	if err != nil {
		return false, fmt.Errorf("claiming %s: %w", entry.EntityID, err)
	}
	taken, _ := done.RowsAffected()
	return taken > 0, nil
}

// placeholders is a list of `?` for an IN clause of the given length.
func placeholders(count int) string {
	out := make([]byte, 0, count*2)
	for i := range count {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// owed reads the sync_queue entries of a space that are due. It does not claim them: the batch a
// round can carry is bounded by bytes, and only what the round takes is marked as in flight.
func (s *Store) owed(ctx context.Context, spaceID string) ([]queueRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity_type, entity_id, action, operation_id, retry_count
		   FROM sync_queue
		  WHERE workspace_id = ?
		    AND (status = 'pending'
		         OR (status = 'failed' AND (next_retry_at IS NULL OR next_retry_at <= ?)))
		  ORDER BY created_at, rowid`, spaceID, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("reading what %s owes: %w", spaceID, err)
	}
	defer rows.Close()

	out := []queueRow{}
	for rows.Next() {
		var (
			entry  queueRow
			kind   string
			action string
		)
		if err := rows.Scan(&entry.QueueID, &kind, &entry.EntityID, &action, &entry.OperationID,
			&entry.Attempts); err != nil {
			return nil, fmt.Errorf("reading what %s owes: %w", spaceID, err)
		}
		entry.Kind, entry.Action = domain.EntityType(kind), domain.Action(action)
		out = append(out, entry)
	}
	return out, rows.Err()
}

// queueRow represents an entry from the sync_queue table.
type queueRow struct {
	QueueID     string
	Kind        domain.EntityType
	EntityID    string
	Action      domain.Action
	OperationID string
	Attempts    int
}

// Accepted updates entity version after successful push and removes queue item.
func (s *Store) Accepted(ctx context.Context, change domain.PendingChange, version int32) error {
	return s.inTx(ctx, "settling a change", func(tx *writeTx) error {
		err := agreed(ctx, tx, change.SpaceID, change.Entity.Kind, change.Entity.ID, version)
		if err != nil {
			return err
		}
		return s.forget(ctx, tx, change.QueueID)
	})
}

// Superseded applies winning remote entity and removes queue item.
func (s *Store) Superseded(
	ctx context.Context,
	change domain.PendingChange,
	winner domain.SyncEntity,
) error {
	return s.inTx(ctx, "taking the server's row", func(tx *writeTx) error {
		if err := applyEntity(ctx, tx, change.SpaceID, winner); err != nil {
			return err
		}
		return s.forget(ctx, tx, change.QueueID)
	})
}

// Failed marks queued change as failed and sets exponential backoff retry time.
func (s *Store) Failed(
	ctx context.Context,
	change domain.PendingChange,
	reason domain.PushReason,
	retryIn time.Duration,
) error {
	wait := backoff(retryIn, change.Attempts)
	return s.stuck(ctx, change, "failed", reason, time.Now().Add(wait).UnixMilli())
}

// maxRetryWait caps exponential backoff retry intervals.
const maxRetryWait = time.Hour

// backoff calculates exponential retry delay capped by maxRetryWait.
func backoff(base time.Duration, refused int) time.Duration {
	wait := base
	for range min(refused, 10) {
		wait *= 2
		if wait >= maxRetryWait {
			return maxRetryWait
		}
	}
	return wait
}

// Parked marks queued change as permanently unprocessable.
func (s *Store) Parked(
	ctx context.Context,
	change domain.PendingChange,
	reason domain.PushReason,
) error {
	return s.stuck(ctx, change, "parked", reason, 0)
}

// stuck updates failure or parked status metadata for a queued change.
func (s *Store) stuck(
	ctx context.Context,
	change domain.PendingChange,
	status string,
	reason domain.PushReason,
	retryAt int64,
) error {
	what := "recording what became of " + change.Entity.ID
	return s.inTx(ctx, what, func(tx *writeTx) error {
		// An entry in flight whose row was edited has a second one waiting beside it, built from the
		// live row, and the index allows one waiting entry for a row. The older one has nothing the
		// newer lacks, so it goes instead of being waited on.
		gone, err := tx.ExecContext(ctx,
			`DELETE FROM sync_queue WHERE id = ? AND status = 'sending' AND `+waitingBeside,
			change.QueueID)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if superseded, _ := gone.RowsAffected(); superseded > 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE sync_queue SET status = ?, reason = ?, retry_count = retry_count + 1,
			        next_retry_at = ?
			  WHERE id = ?`, status, string(reason), nullIfZero64(retryAt), change.QueueID)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	})
}

// waitingBeside is true of a queue entry that has another for the same row which is not in flight,
// or an in flight one made after it. Only one entry per row may wait, and the newest is built from
// the row as it stands, so it is the one that stays.
const waitingBeside = `EXISTS (
	SELECT 1 FROM sync_queue o
	 WHERE o.workspace_id = sync_queue.workspace_id AND o.entity_type = sync_queue.entity_type
	   AND o.entity_id = sync_queue.entity_id AND o.id <> sync_queue.id
	   AND (o.status <> 'sending' OR o.rowid > sync_queue.rowid))`

// ReleaseInFlight puts back what a batch left in flight, which a crash or a failed round leaves
// behind. An entry with a newer one beside it is dropped for it rather than put back, since two
// waiting entries for one row are what the index forbids and one violation would leave every other
// entry in flight where it was.
func (s *Store) ReleaseInFlight(ctx context.Context) error {
	return s.inTx(ctx, "releasing what was in flight", func(tx *writeTx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_queue WHERE status = 'sending' AND `+waitingBeside); err != nil {
			return fmt.Errorf("releasing what was in flight: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_queue SET status = 'pending' WHERE status = 'sending'`); err != nil {
			return fmt.Errorf("releasing what was in flight: %w", err)
		}
		return nil
	})
}

// waitingListed is how many entries of one space are described to the window. A first link queues
// every row of the space, and each described entry is named by a query of its own; the number of
// entries is in the counts, which is what the window draws it from.
const waitingListed = 100

// Waiting retrieves the oldest queued changes for a workspace, described for the window.
func (s *Store) Waiting(ctx context.Context, spaceID string) ([]domain.QueuedChange, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, entity_id, action, status, retry_count, reason, ifnull(next_retry_at, 0)
		   FROM sync_queue
		  WHERE workspace_id = ?
		  ORDER BY created_at, rowid
		  LIMIT ?`, spaceID, waitingListed)
	if err != nil {
		return nil, fmt.Errorf("reading the queue of %s: %w", spaceID, err)
	}
	defer rows.Close()

	held := []domain.QueuedChange{}
	for rows.Next() {
		var (
			entry                domain.QueuedChange
			kind, action, status string
		)
		if err := rows.Scan(&kind, &entry.EntityID, &action, &status, &entry.Attempts, &entry.Reason,
			&entry.RetryAt); err != nil {
			return nil, fmt.Errorf("reading the queue of %s: %w", spaceID, err)
		}
		entry.Kind, entry.Action = domain.EntityType(kind), domain.Action(action)
		entry.Status = domain.QueueStatus(status)
		held = append(held, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the queue of %s: %w", spaceID, err)
	}

	for at := range held {
		held[at].Name = nameOf(ctx, s.db, held[at].Kind, held[at].EntityID)
	}
	return held, nil
}

// nameOf fetches human-readable display name for a sync entity.
func nameOf(ctx context.Context, q querier, kind domain.EntityType, id string) string {
	switch kind {
	case domain.EntityWorkspace:
		var name string
		if err := q.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ?`, id).
			Scan(&name); err == nil {
			return name
		}
	case domain.EntityCollection, domain.EntityRequest, domain.EntityEnvironment,
		domain.EntityVariable:
		table, ok := syncedTable(kind)
		if !ok {
			return ""
		}
		var name string
		if err := q.QueryRowContext(ctx, `SELECT name FROM `+table+` WHERE id = ?`, id).
			Scan(&name); err == nil {
			return name
		}
	case domain.EntityRun:
		var collection string
		if err := q.QueryRowContext(ctx,
			`SELECT ifnull(collection_id, '') FROM collection_runs WHERE id = ?`, id).
			Scan(&collection); err == nil {
			return nameOf(ctx, q, domain.EntityCollection, collection)
		}
	case domain.EntityCookie:
		var name, host string
		if err := q.QueryRowContext(ctx,
			`SELECT name, domain FROM jar_cookies WHERE id = ?`, id).Scan(&name, &host); err == nil {
			return name + " · " + host
		}
	case domain.EntityJarDomain:
		var host string
		if err := q.QueryRowContext(ctx,
			`SELECT domain FROM jar_domains WHERE id = ?`, id).Scan(&host); err == nil {
			return host
		}
	case domain.EntityRecord:
		var method, url string
		if err := q.QueryRowContext(ctx,
			`SELECT method, url FROM records WHERE id = ?`, id).Scan(&method, &url); err == nil {
			return method + " " + url
		}
	}
	return ""
}

// QueueCounts aggregates sync queue item counts grouped by status per workspace.
func (s *Store) QueueCounts(ctx context.Context) (map[string]domain.SyncCounts, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT workspace_id, status, count(*) FROM sync_queue GROUP BY workspace_id, status`)
	if err != nil {
		return nil, fmt.Errorf("weighing the queues: %w", err)
	}
	defer rows.Close()

	out := map[string]domain.SyncCounts{}
	for rows.Next() {
		var (
			spaceID, status string
			count           int
		)
		if err := rows.Scan(&spaceID, &status, &count); err != nil {
			return nil, fmt.Errorf("weighing the queues: %w", err)
		}
		counts := out[spaceID]
		switch domain.QueueStatus(status) {
		case domain.QueuePending:
			counts.Pending = count
		case domain.QueueSending:
			counts.Sending = count
		case domain.QueueFailed:
			counts.Failed = count
		case domain.QueueParked:
			counts.Parked = count
		}
		out[spaceID] = counts
	}
	return out, rows.Err()
}

// RecordRound updates workspace_sync record after completing a sync round.
func (s *Store) RecordRound(
	ctx context.Context,
	spaceID string,
	state domain.SyncState,
	at int64,
	failed domain.Code,
	attempts int,
) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspace_sync (workspace_id, state, last_synced_at, error, attempts)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET
		   state = excluded.state,
		   last_synced_at = CASE WHEN excluded.last_synced_at = 0
		                         THEN workspace_sync.last_synced_at
		                         ELSE excluded.last_synced_at END,
		   error = excluded.error, attempts = excluded.attempts`,
		spaceID, string(state), at, string(failed), attempts)
	if err != nil {
		return fmt.Errorf("recording where %s stands: %w", spaceID, err)
	}
	return nil
}

// SetState updates workspace sync state column in workspace_sync.
func (s *Store) SetState(ctx context.Context, spaceID string, state domain.SyncState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspace_sync (workspace_id, state) VALUES (?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET state = excluded.state`, spaceID, string(state))
	if err != nil {
		return fmt.Errorf("recording the state of %s: %w", spaceID, err)
	}
	return nil
}

// Reset clears workspace queue state and triggers full re-synchronization.
func (s *Store) Reset(ctx context.Context, spaceID string) error {
	return s.inTx(ctx, "starting the space over", func(tx *writeTx) error {
		return offerAnew(ctx, tx, spaceID)
	})
}

// offerAnew resets queue state and enqueues all local workspace entities. A space this person may
// not write in is started over and offered nothing: the round does not push there, so what was
// queued would wait for ever. A role that is given the right later is read as an access change,
// which the server answers with a resync that comes here again.
func offerAnew(ctx context.Context, tx *writeTx, spaceID string) error {
	if err := startOver(ctx, tx, spaceID); err != nil {
		return err
	}
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT access_json FROM workspaces WHERE id = ?`, spaceID).Scan(&raw)
	if err != nil {
		return fmt.Errorf("reading the access to %s: %w", spaceID, err)
	}
	if !accessOf(raw).Write {
		return nil
	}
	if err := offerEverything(ctx, tx, spaceID); err != nil {
		return err
	}
	// What was just offered is the whole space as it stands, not somebody's unsettled work, so a pull
	// may still rewrite those rows: the read from the beginning is how a role change reaches them.
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_queue SET offered = 1 WHERE workspace_id = ? AND entity_type NOT IN (?, ?)`,
		spaceID, entityRecord, entityRun); err != nil {
		return fmt.Errorf("marking what was offered anew in %s: %w", spaceID, err)
	}
	return nil
}

// offerEverything enqueues creation/deletion actions for workspace entities.
func offerEverything(ctx context.Context, tx *writeTx, spaceID string) error {
	levels := []struct {
		table  string
		kind   domain.EntityType
		scope  string
		action domain.Action
	}{
		{"collections", entityCollection, `workspace_id = ?`, actionCreate},
		{"collection_nodes", entityRequest,
			`collection_id IN (SELECT id FROM collections WHERE workspace_id = ?)`, actionCreate},
		{"environments", entityEnvironment, `workspace_id = ?`, actionCreate},
		{"variables", entityVariable, `workspace_id = ?`, actionCreate},
		{"jar_cookies", entityCookie, `workspace_id = ?`, actionUpdate},
		{"jar_domains", entityJarDomain, `workspace_id = ?`, actionUpdate},
	}
	for _, level := range levels {
		for _, fate := range []struct {
			where  string
			action domain.Action
		}{
			{` AND deleted_at IS NULL`, level.action},
			{` AND deleted_at IS NOT NULL`, actionDelete},
		} {
			ids, err := idsOf(ctx, tx,
				`SELECT id FROM `+level.table+` WHERE `+level.scope+fate.where, spaceID)
			if err != nil {
				return fmt.Errorf("offering the rows of %s: %w", level.table, err)
			}
			if err := queueAll(ctx, tx, spaceID, level.kind, fate.action, ids); err != nil {
				return err
			}
		}
	}

	if err := enqueue(ctx, tx, spaceID, entityWorkspace, spaceID, actionUpdate); err != nil {
		return err
	}
	return nil
}

// forget removes entry from sync_queue by ID.
func (s *Store) forget(ctx context.Context, q execer, queueID string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM sync_queue WHERE id = ?`, queueID); err != nil {
		return fmt.Errorf("dropping an entry from the queue: %w", err)
	}
	return nil
}

// startOver clears sync_queue entries and resets workspace sync sequence.
//
// A record and a run are not the queue of a space's contents: they are this device's own day, they
// were not made again by starting over, and nothing on the server will offer them back. They stay.
func startOver(ctx context.Context, tx *writeTx, spaceID string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_queue WHERE workspace_id = ? AND entity_type NOT IN (?, ?)`,
		spaceID, entityRecord, entityRun); err != nil {
		return fmt.Errorf("clearing the queue of %s: %w", spaceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE workspace_sync SET last_sync_seq = 0, attempts = 0 WHERE workspace_id = ?`,
		spaceID); err != nil {
		return fmt.Errorf("resetting the cursor of %s: %w", spaceID, err)
	}
	return nil
}

// agreed records updated server version for local entities.
func agreed(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	kind domain.EntityType,
	entityID string,
	version int32,
) error {
	if domain.ThroughHistory(kind) {
		// A record and a run carry no version: history is a log, and what it says about one of them
		// is whether it was filed and not which version of it won.
		return nil
	}
	if kind == domain.EntityWorkspace {
		_, err := tx.ExecContext(ctx, `UPDATE workspaces SET version = ? WHERE id = ?`,
			version, spaceID)
		if err != nil {
			return fmt.Errorf("recording the version of %s: %w", spaceID, err)
		}
		return nil
	}

	table, ok := syncedTable(kind)
	if !ok {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET version = ? WHERE id = ?`, version, entityID)
	if err != nil {
		return fmt.Errorf("recording the version of %s: %w", entityID, err)
	}
	return nil
}

// syncedTable returns table name corresponding to entity type.
func syncedTable(kind domain.EntityType) (string, bool) {
	switch kind {
	case domain.EntityCollection:
		return "collections", true
	case domain.EntityRequest:
		return "collection_nodes", true
	case domain.EntityEnvironment:
		return "environments", true
	case domain.EntityVariable:
		return "variables", true
	case domain.EntityCookie:
		return "jar_cookies", true
	case domain.EntityJarDomain:
		return "jar_domains", true
	}
	return "", false
}

// execer defines DB execution interface for *sql.DB and *writeTx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// nullIfZero64 returns nil if value is zero, else value.
func nullIfZero64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
