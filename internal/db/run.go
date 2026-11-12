package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"schemo/internal/domain"
)

// A run and the rows it produced are read and written in one piece: once when it starts and again
// every time a request comes back.

// SaveRun writes twice: when the run starts, so its results have a parent, and when it ends, with
// the counters and the total time.
//
// Every write offers the run to the server again. A run is a document of its own there, kept once
// by its id.
func (s *Store) SaveRun(ctx context.Context, run domain.CollectionRun) error {
	return s.inTx(ctx, fmt.Sprintf("saving run %s", run.ID), func(tx *writeTx) error {
		if err := writeRun(ctx, tx, run); err != nil {
			return err
		}
		return s.offerRun(ctx, tx, run.ID)
	})
}

// writeRun puts a run and its rows where they belong, once by id. A run that arrived from another
// device is written with this, not SaveRun: the server already holds it.
func writeRun(ctx context.Context, tx *writeTx, run domain.CollectionRun) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO collection_runs (id, collection_id, node_id, environment, started_at,
		                              finished_at, passed, failed, duration_us)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   finished_at = excluded.finished_at, passed = excluded.passed,
		   failed = excluded.failed, duration_us = excluded.duration_us`,
		run.ID, run.CollectionID, run.NodeID, run.Environment, run.StartedAt, run.FinishedAt,
		run.Passed, run.Failed, run.DurationUs); err != nil {
		return fmt.Errorf("saving run %s: %w", run.ID, err)
	}
	for _, result := range run.Results {
		if err := writeRunResult(ctx, tx, run.ID, result); err != nil {
			return err
		}
	}
	return nil
}

// AppendRunResult adds one row to a run. Rows go in one at a time because that is how a run makes
// them: a fifty-request run whose window is closed after the tenth keeps the ten.
func (s *Store) AppendRunResult(
	ctx context.Context,
	runID string,
	result domain.CollectionRunResult,
) error {
	return s.inTx(ctx, fmt.Sprintf("saving a result of run %s", runID), func(tx *writeTx) error {
		if err := writeRunResult(ctx, tx, runID, result); err != nil {
			return err
		}
		return s.offerRun(ctx, tx, runID)
	})
}

// writeRunResult writes one row of a run, keyed by where it stood in the run.
func writeRunResult(
	ctx context.Context,
	tx *writeTx,
	runID string,
	result domain.CollectionRunResult,
) error {
	failure, err := failureColumn(result.Failure)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO collection_run_results (run_id, node_id, position, status, ok, duration_us,
		                                     error, failure, record_id, assertions_passed,
		                                     assertions_total, skipped)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id, position) DO UPDATE SET
		   status = excluded.status, ok = excluded.ok,
		   duration_us = excluded.duration_us, error = excluded.error,
		   failure = excluded.failure,
		   record_id = excluded.record_id,
		   assertions_passed = excluded.assertions_passed,
		   assertions_total = excluded.assertions_total,
		   skipped = excluded.skipped`,
		runID, result.NodeID, result.Position, result.Status, result.OK, result.DurationUs,
		result.Error, failure, nullIfEmpty(result.RecordID), result.AssertionsPassed,
		result.AssertionsTotal, result.Skipped); err != nil {
		return fmt.Errorf("saving a result of run %s: %w", runID, err)
	}
	return nil
}

// offerRun queues a run for the server in the workspace its collection belongs to. The table carries no
// workspace of its own, since a run of a deleted collection is a run nobody may read.
func (s *Store) offerRun(ctx context.Context, tx *writeTx, runID string) error {
	var workspace string
	err := tx.QueryRowContext(ctx,
		`SELECT c.workspace_id FROM collection_runs r
		   JOIN entities c ON c.id = r.collection_id
		  WHERE r.id = ?`, runID).Scan(&workspace)
	if errors.Is(err, sql.ErrNoRows) {
		// A run whose collection is gone is not a run the server has anything to file it under.
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the workspace of run %s: %w", runID, err)
	}
	return enqueue(ctx, tx, workspace, entityRun, runID, actionUpdate)
}

// failureColumn is a row's refusal as the column holds it, or an empty string for a row the app did
// not refuse. A failure with no sentence of ours is the machine's, which the error column already
// holds.
func failureColumn(failure *domain.Failure) (string, error) {
	if failure == nil {
		return "", nil
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		return "", fmt.Errorf("encoding a failure: %w", err)
	}
	return string(encoded), nil
}

const runColumns = `id, collection_id, node_id, environment, started_at, finished_at, passed, failed, duration_us`

func scanRun(row interface{ Scan(...any) error }) (domain.CollectionRun, error) {
	var run domain.CollectionRun
	err := row.Scan(&run.ID, &run.CollectionID, &run.NodeID, &run.Environment, &run.StartedAt,
		&run.FinishedAt, &run.Passed, &run.Failed, &run.DurationUs)
	return run, err
}

// ListRuns reads run summaries for a level, newest first.
func (s *Store) ListRuns(ctx context.Context, collectionID, nodeID string) ([]domain.CollectionRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+` FROM collection_runs
		  WHERE collection_id = ? AND node_id = ?
		  ORDER BY started_at DESC, rowid DESC`, collectionID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("listing runs for %s: %w", collectionID, err)
	}
	defer rows.Close()

	runs := []domain.CollectionRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("reading a run of %s: %w", collectionID, err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// LastRun reads the newest run of a node, or of the whole collection when the node is empty.
func (s *Store) LastRun(ctx context.Context, collectionID, nodeID string) (domain.CollectionRun, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM collection_runs
		  WHERE collection_id = ? AND node_id = ?
		  ORDER BY started_at DESC, rowid DESC LIMIT 1`, collectionID, nodeID)
	return s.readRun(ctx, row, collectionID)
}

// ReadRun reads a saved run and its results within the named level.
func (s *Store) ReadRun(ctx context.Context, collectionID, nodeID, id string) (domain.CollectionRun, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM collection_runs WHERE collection_id = ? AND node_id = ? AND id = ?`,
		collectionID, nodeID, id)
	return s.readRun(ctx, row, collectionID)
}

func (s *Store) readRun(ctx context.Context, row *sql.Row, collectionID string) (domain.CollectionRun, bool, error) {
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CollectionRun{}, false, nil
	}
	if err != nil {
		return domain.CollectionRun{}, false, fmt.Errorf("reading a run of %s: %w", collectionID, err)
	}
	run.Results, err = resultsOf(ctx, s.db, run.ID)
	return run, err == nil, err
}

// The empty id is the top level. A level's requests and its collections are numbered in one
// sequence, so both tables are asked: a new row lands after what it follows, not at a group's end.
