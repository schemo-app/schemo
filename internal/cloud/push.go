package cloud

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"schemo/internal/domain"
)

// Sending what this device owes. Parents go first, or the server refuses children of a collection it
// has not seen. Verdicts are per entity, so one refused row does not hold back the batch.

// push sends what a workspace owes.
func (s *Sync) push(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
	limits domain.Limits,
) error {
	// One server-sized batch per round, so a timeout retries only that batch.
	changes, err := s.store.Owed(ctx, workspace, limits.MaxPushBytes)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	entities, records := splitBatch(changes)
	if len(entities) > 0 {
		if err := s.sendBatches(ctx, caller, workspace, entities); err != nil {
			return s.releaseInFlight(ctx, err)
		}
		s.addProgress(len(entities), 0)
	}
	if len(records) > 0 {
		if err := s.uploadRecords(ctx, caller, workspace, records); err != nil {
			return s.releaseInFlight(ctx, err)
		}
		s.addProgress(len(records), 0)
	}
	return nil
}

func (s *Sync) sendBatches(ctx context.Context, caller domain.Caller, workspace domain.SyncWorkspace, changes []domain.PendingChange) error {
	err := s.sendBatch(ctx, caller, workspace, changes)
	if !domain.RefusedForSize(err) {
		return err
	}
	if len(changes) == 1 {
		return s.store.Parked(ctx, changes[0], domain.ReasonTooLarge)
	}
	middle := len(changes) / 2
	if err := s.sendBatches(ctx, caller, workspace, changes[:middle]); err != nil {
		return err
	}
	return s.sendBatches(ctx, caller, workspace, changes[middle:])
}

// splitBatch separates history (records and runs) from everything else: one queue, two services.
func splitBatch(changes []domain.PendingChange) ([]domain.PendingChange, []domain.PendingChange) {
	entities := make([]domain.PendingChange, 0, len(changes))
	records := make([]domain.PendingChange, 0, len(changes))
	for _, change := range changes {
		if domain.ThroughHistory(change.Entity.Kind) {
			records = append(records, change)
			continue
		}
		entities = append(entities, change)
	}

	// Oldest first among equals, and parents before children. The sort is stable so that the moments
	// the entries were first owed are what decides inside one depth.
	slices.SortStableFunc(entities, func(a, b domain.PendingChange) int {
		return cmp.Compare(domain.Depth(a.Entity.Kind), domain.Depth(b.Entity.Kind))
	})
	return entities, records
}

// sendBatch pushes a batch of rows and does what each verdict says.
func (s *Sync) sendBatch(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
	changes []domain.PendingChange,
) error {
	verdicts, err := s.server.Push(ctx, caller, workspace.RemoteID, pushedOf(changes))
	if err != nil {
		return err
	}
	return s.applyVerdicts(ctx, workspace, changes, verdicts)
}

// applyVerdicts applies the per-entity answer. A lost edit is replaced by the server's row and
// remembered, so the person is told it was not kept.
func (s *Sync) applyVerdicts(
	ctx context.Context,
	workspace domain.SyncWorkspace,
	changes []domain.PendingChange,
	verdicts []domain.Verdict,
) error {
	byID := make(map[string]domain.Verdict, len(verdicts))
	for _, verdict := range verdicts {
		byID[verdict.EntityID] = verdict
	}

	for _, change := range changes {
		verdict, ok := byID[change.Entity.ID]
		if !ok {
			// The server answered about everything it was sent, and this one is not in the answer: it
			// is tried again rather than left in flight forever.
			if err := s.retryOrPark(ctx, change, domain.ReasonUnspecified); err != nil {
				return err
			}
			continue
		}

		switch verdict.Status {
		case domain.PushStored, domain.PushDuplicate:
			if err := s.store.Accepted(ctx, change, verdict.Version); err != nil {
				return err
			}
		case domain.PushSuperseded:
			if verdict.Winner == nil {
				// A conflict with no winner in the answer is a server that broke its own promise, and
				// there is nothing to apply: the entry waits rather than being dropped.
				if err := s.retryOrPark(ctx, change, domain.ReasonUnspecified); err != nil {
					return err
				}
				continue
			}
			if err := s.store.Superseded(ctx, change, *verdict.Winner); err != nil {
				return err
			}
			s.recordConflict(workspace.ID, change)
		default:
			if err := s.retryOrPark(ctx, change, verdict.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// retryOrPark retries what time can fix (a missing parent, a quota) and parks the rest where it
// stays visible.
func (s *Sync) retryOrPark(
	ctx context.Context,
	change domain.PendingChange,
	reason domain.PushReason,
) error {
	switch reason {
	case domain.ReasonParentMissing:
		return s.store.Failed(ctx, change, reason, 30*time.Second)
	case domain.ReasonQuotaExceeded:
		return s.store.Failed(ctx, change, reason, time.Hour)
	case domain.ReasonTooLarge, domain.ReasonForbidden, domain.ReasonInvalidData,
		domain.ReasonEmpty:
		// A record the server would not file for being empty is a record this device sent without a
		// beginning — the row is what it is here, and sending it again sends the same emptiness.
		return s.store.Parked(ctx, change, reason)
	default:
		// No answer or an unknown refusal: retried a few times, then parked where it stays visible.
		if change.Attempts >= unansweredTries {
			return s.store.Parked(ctx, change, domain.ReasonUnspecified)
		}
		return s.store.Failed(ctx, change, domain.ReasonUnspecified, 5*time.Minute)
	}
}

// releaseInFlight requeues what a failed batch left in flight, so it is not stuck until the next start.
func (s *Sync) releaseInFlight(ctx context.Context, err error) error {
	// A round that failed because its context ended is the common reason to be here, and a release
	// that shared the context would fail for the same reason and leave the entries where they were.
	if back := s.store.ReleaseInFlight(context.WithoutCancel(ctx)); back != nil {
		// The batch's own failure is what the caller asked about, but an entry left in flight is one
		// nobody sends, and a round that ends without saying so leaves it there until the next start.
		return errors.Join(err, back)
	}
	return err
}

// uploadRecords uploads the history a workspace owes, records and runs, plus the deletions.
func (s *Sync) uploadRecords(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
	records []domain.PendingChange,
) error {
	keeping := make([]domain.PendingChange, 0, len(records))
	gone := make([]domain.PendingChange, 0, len(records))
	for _, record := range records {
		if record.Entity.Deleted {
			gone = append(gone, record)
			continue
		}
		keeping = append(keeping, record)
	}

	if len(keeping) > 0 {
		entries := make([]domain.PendingChange, 0, len(keeping))
		runs := make([]domain.PendingChange, 0, len(keeping))
		for _, record := range keeping {
			if record.Entity.Kind == domain.EntityRun {
				runs = append(runs, record)
				continue
			}
			entries = append(entries, record)
		}

		answers, runAnswers, err := s.server.Sent(ctx, caller, workspace.RemoteID,
			pushedOf(entries), pushedOf(runs))
		switch {
		case err == nil:
			if err := s.applyRecordVerdicts(ctx, entries, answers); err != nil {
				return err
			}
			if err := s.applyRecordVerdicts(ctx, runs, runAnswers); err != nil {
				return err
			}
		case domain.RefusedForSize(err) && len(keeping) == 1:
			// A single document over the batch limit will never fit, so it is parked instead of
			// blocking the rest of the history every round.
			if err := s.store.Parked(ctx, keeping[0], domain.ReasonTooLarge); err != nil {
				return err
			}
		default:
			return err
		}
	}

	if len(gone) > 0 {
		ids := make([]string, 0, len(gone))
		for _, record := range gone {
			ids = append(ids, record.Entity.ID)
		}
		if _, err := s.server.Retract(ctx, caller, workspace.RemoteID, ids); err != nil {
			return err
		}
		// A retraction the server took is the answer for every id it was given: the call either
		// refused the batch or took it, and it has nothing more to say about one record than another.
		for _, record := range gone {
			if err := s.store.Accepted(ctx, record, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// pushedOf is a batch as the server takes it: the operation id that makes a retry idempotent, and
// the row.
func pushedOf(changes []domain.PendingChange) []domain.Pushed {
	out := make([]domain.Pushed, 0, len(changes))
	for _, change := range changes {
		out = append(out, domain.Pushed{OperationID: change.OperationID, Entity: change.Entity})
	}
	return out
}

// applyRecordVerdicts applies a batch's answer; records and runs never conflict, so a reason means
// refused and none means stored.
func (s *Sync) applyRecordVerdicts(
	ctx context.Context,
	records []domain.PendingChange,
	verdicts []domain.RecordVerdict,
) error {
	byID := make(map[string]domain.RecordVerdict, len(verdicts))
	for _, verdict := range verdicts {
		byID[verdict.RecordID] = verdict
	}

	for _, record := range records {
		verdict, ok := byID[record.Entity.ID]
		// A record the server filed carries no reason, and that is the answer: a record with one was
		// refused, and one that was not answered at all is tried again rather than dropped.
		if ok && verdict.Reason == "" {
			if err := s.store.Accepted(ctx, record, 0); err != nil {
				return err
			}
			continue
		}

		reason := domain.ReasonUnspecified
		if ok {
			reason = verdict.Reason
		}
		if err := s.retryOrPark(ctx, record, reason); err != nil {
			return err
		}
	}
	return nil
}

// unansweredTries is how many rounds a row may go out in without the server taking or refusing it
// by a reason before it is parked.
const unansweredTries = 5
