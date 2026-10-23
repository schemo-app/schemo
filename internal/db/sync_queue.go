package db

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"schemo/internal/domain"
)

// Owed retrieves pending synchronization changes capped by maxBytes: a batch the server would
// refuse is not one to build.
func (s *Store) Owed(
	ctx context.Context,
	workspace domain.SyncWorkspace,
	maxBytes int64,
) ([]domain.PendingChange, error) {
	if workspace.RemoteID == "" {
		return nil, nil
	}

	waiting, err := s.owed(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	if waiting, err = s.parentsFirst(ctx, workspace.ID, waiting); err != nil {
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
		entity, found, err := entityFor(ctx, s.db, workspace, entry, scopes)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := s.forget(ctx, s.db, entry.QueueID); err != nil {
				return nil, err
			}
			continue
		}

		size := int64(len(entity.Data)) + int64(len(entity.ID)) + 64
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
			WorkspaceID: workspace.ID,
			Action:      entry.Action,
			OperationID: entry.OperationID,
			Attempts:    entry.Attempts,
			Entity:      entity,
		})
	}

	return changes, nil
}

// parentsFirst puts a batch in the order the server can take it: by kind, as a push is made, and
// among collections by depth. A child sent before its parent is refused and waits out a backoff;
// the order is made before the size cut so the cut leaves the later, deeper rows for the next
// round.
func (s *Store) parentsFirst(
	ctx context.Context,
	workspaceID string,
	waiting []queueRow,
) ([]queueRow, error) {
	nested := slices.ContainsFunc(waiting, func(one queueRow) bool {
		return one.Kind == domain.EntityCollection
	})
	depths := map[string]int{}
	if nested {
		rows, err := s.db.QueryContext(ctx,
			`WITH RECURSIVE tree(id, depth) AS (
			   SELECT id, 0 FROM entities
			    WHERE workspace_id = ? AND kind = 'collection' AND parent_id = ''
			   UNION ALL
			   SELECT c.id, tree.depth + 1 FROM entities c JOIN tree ON c.parent_id = tree.id
			    WHERE c.kind = 'collection')
			 SELECT id, depth FROM tree`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", workspaceID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var depth int
			if err := rows.Scan(&id, &depth); err != nil {
				return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", workspaceID, err)
			}
			depths[id] = depth
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("reading how deep the collections of %s stand: %w", workspaceID, err)
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
// that landed after the entry was read gave it another operation id, so the payload in hand is
// stale and the entry is left for the next round. An entry in flight is never changed: a later edit
// makes a second entry beside it.
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

// owed reads the sync_queue entries of a workspace that are due. It does not claim them: the batch a
// round can carry is bounded by bytes, and only what the round takes is marked as in flight.
func (s *Store) owed(ctx context.Context, workspaceID string) ([]queueRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, entity_type, entity_id, action, operation_id, retry_count
		   FROM sync_queue
		  WHERE workspace_id = ?
		    AND (status = 'pending'
		         OR (status = 'failed' AND (next_retry_at IS NULL OR next_retry_at <= ?)))
		  ORDER BY created_at, rowid`, workspaceID, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("reading what %s owes: %w", workspaceID, err)
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
			return nil, fmt.Errorf("reading what %s owes: %w", workspaceID, err)
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
		err := agreed(ctx, tx, change.WorkspaceID, change.Entity.Kind, change.Entity.ID, version)
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
		if err := applyEntity(ctx, tx, change.WorkspaceID, winner); err != nil {
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
		// An entry in flight whose row was edited has a second one waiting beside it, built from
		// the live row, and the index allows one waiting entry per row. The older one has nothing
		// the newer lacks, so it goes.
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

// waitingBeside is true of a queue entry with another for the same row that is not in flight, or an
// in-flight one made after it. Only one entry per row may wait and the newest is built from the row
// as it stands, so it stays.
const waitingBeside = `EXISTS (
	SELECT 1 FROM sync_queue o
	 WHERE o.workspace_id = sync_queue.workspace_id AND o.entity_type = sync_queue.entity_type
	   AND o.entity_id = sync_queue.entity_id AND o.id <> sync_queue.id
	   AND (o.status <> 'sending' OR o.rowid > sync_queue.rowid))`

// ReleaseInFlight puts back what a batch left in flight after a crash or failed round. An entry
// with a newer one beside it is dropped rather than put back: two waiting entries for one row
// violate the index and would leave every other entry in flight.
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

// waitingListed is how many entries of one workspace are described to the window. A first link queues
// every row, and each described entry costs a query; the full number is in the counts.
const waitingListed = 100

// Waiting retrieves the oldest queued changes for a workspace, described for the window.
func (s *Store) Waiting(ctx context.Context, workspaceID string) ([]domain.QueuedChange, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, entity_id, action, status, retry_count, reason, ifnull(next_retry_at, 0)
		   FROM sync_queue
		  WHERE workspace_id = ?
		  ORDER BY created_at, rowid
		  LIMIT ?`, workspaceID, waitingListed)
	if err != nil {
		return nil, fmt.Errorf("reading the queue of %s: %w", workspaceID, err)
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
			return nil, fmt.Errorf("reading the queue of %s: %w", workspaceID, err)
		}
		entry.Kind, entry.Action = domain.EntityType(kind), domain.Action(action)
		entry.Status = domain.QueueStatus(status)
		held = append(held, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the queue of %s: %w", workspaceID, err)
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
	case domain.EntityRun:
		var collection string
		if err := q.QueryRowContext(ctx,
			`SELECT ifnull(collection_id, '') FROM collection_runs WHERE id = ?`, id).
			Scan(&collection); err == nil {
			return nameOf(ctx, q, domain.EntityCollection, collection)
		}
	case domain.EntityRecord:
		var method, url string
		if err := q.QueryRowContext(ctx,
			`SELECT method, url FROM records WHERE id = ?`, id).Scan(&method, &url); err == nil {
			return method + " " + url
		}
	default:
		if row, err := anyEntity(ctx, q, id); err == nil {
			if spec := syncKinds[kind]; spec.label != nil {
				return spec.label(row)
			}
			return row.Name
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
			workspaceID, status string
			count               int
		)
		if err := rows.Scan(&workspaceID, &status, &count); err != nil {
			return nil, fmt.Errorf("weighing the queues: %w", err)
		}
		counts := out[workspaceID]
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
		out[workspaceID] = counts
	}
	return out, rows.Err()
}
