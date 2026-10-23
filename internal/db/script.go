package db

// What the script package keeps: the code of every level, and the report of every run.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"schemo/internal/domain"
)

// Scripts reads the scripts of a level: a collection, a request or a draft.
func (s *Store) Scripts(ctx context.Context, workspaceID, id string) (*domain.Scripts, error) {
	row, err := scriptLevel(ctx, s.db, workspaceID, id)
	if err != nil {
		return nil, err
	}
	var held struct {
		Scripts *domain.Scripts `json:"scripts"`
	}
	if err := json.Unmarshal(row.Data, &held); err != nil {
		return nil, fmt.Errorf("reading the scripts of %s: %w", id, err)
	}
	return held.Scripts, nil
}

func scriptLevel(ctx context.Context, q querier, workspaceID, id string) (entity, error) {
	row, err := scanEntity(q.QueryRowContext(ctx, `SELECT `+entityColumns+` FROM entities
		WHERE deleted_at IS NULL AND ((id = ? AND kind IN (?, ?)) OR (id = ? AND kind = ?))`,
		id, domain.EntityCollection, domain.EntityRequest, draftKey(workspaceID, domain.DraftID(id)), kindDraft))
	if errors.Is(err, sql.ErrNoRows) {
		return entity{}, fmt.Errorf("level %s: %w", id, domain.ErrNotFound)
	}
	return row, err
}

// SaveScripts replaces the scripts of a level and leaves the rest of it as it is.
func (s *Store) SaveScripts(ctx context.Context, workspaceID, id string, scripts *domain.Scripts) error {
	return s.inTx(ctx, "saving the scripts of "+id, func(tx *writeTx) error {
		row, err := scriptLevel(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(row.Data, &fields); err != nil {
			return fmt.Errorf("reading %s: %w", id, err)
		}
		if scripts == nil {
			delete(fields, "scripts")
		} else if fields["scripts"], err = json.Marshal(scripts); err != nil {
			return err
		}
		if row.Data, err = json.Marshal(fields); err != nil {
			return err
		}
		row.UpdatedAt = 0
		return putEntity(ctx, tx, row)
	})
}

func (s *Store) SaveScriptRun(ctx context.Context, workspaceID string, run domain.ScriptRun) error {
	return writeScriptRun(ctx, s.db, workspaceID, run)
}

// writeScriptRun writes one report whole and writes it again whole: a save that replaces a report
// replaces what it said, and the lines of the one before it are not left behind.
func writeScriptRun(
	ctx context.Context,
	tx execer,
	workspaceID string,
	run domain.ScriptRun,
) error {
	// An empty record id is a run that happened around no send — a pre-request script that stopped the
	// request — and the column takes it as NULL, which is what nothing reads it as.
	logs, err := json.Marshal(domain.OrEmpty(run.Logs))
	if err != nil {
		return fmt.Errorf("encoding the logs of run %s: %w", run.ID, err)
	}
	tests, err := json.Marshal(domain.OrEmpty(run.Tests))
	if err != nil {
		return fmt.Errorf("encoding the tests of run %s: %w", run.ID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO script_runs (id, workspace_id, record_id, node_id, scope, ok, error, duration_us,
		                         created_at, logs_json, tests_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   ok = excluded.ok, error = excluded.error, duration_us = excluded.duration_us,
		   logs_json = excluded.logs_json, tests_json = excluded.tests_json`,
		run.ID, workspaceID, nullIfEmpty(run.RecordID), run.NodeID, string(run.Scope), run.OK,
		run.Error, run.DurationUs, run.CreatedAt, string(logs), string(tests)); err != nil {
		return fmt.Errorf("saving run %s: %w", run.ID, err)
	}
	return nil
}

// AssertionCounts reads the test arrays without loading the reports or their logs into Go.
func (s *Store) AssertionCounts(ctx context.Context, recordID string) (int, int, error) {
	const query = `
		SELECT coalesce(sum(CASE WHEN json_extract(t.value, '$.passed') = 1 THEN 1 ELSE 0 END), 0), count(*)
		  FROM script_runs r
		  JOIN json_each(r.tests_json) t
		 WHERE r.record_id = ?`

	var passed, total int
	if err := s.db.QueryRowContext(ctx, query, recordID).Scan(&passed, &total); err != nil {
		return 0, 0, fmt.Errorf("counting the assertions of record %s: %w", recordID, err)
	}
	return passed, total, nil
}

// ScriptRuns reads what the scripts of one record did, in the order they ran — the collection's
// first, then the folders', then the request's own, which is the order they were executed in.
func (s *Store) ScriptRuns(ctx context.Context, recordID string) ([]domain.ScriptRun, error) {
	return scriptRuns(ctx, s.db, recordID)
}

func scriptRuns(ctx context.Context, q querier, recordID string) ([]domain.ScriptRun, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, coalesce(node_id, ''), scope, ok, error, duration_us, created_at, logs_json, tests_json
		   FROM script_runs
		  WHERE record_id = ?
		  ORDER BY created_at, rowid`, recordID)
	if err != nil {
		return nil, fmt.Errorf("reading the runs of record %s: %w", recordID, err)
	}
	defer rows.Close()

	runs := []domain.ScriptRun{}
	for rows.Next() {
		run := domain.ScriptRun{RecordID: recordID, Logs: []domain.ScriptLog{},
			Tests: []domain.TestResult{}}
		var logs, tests []byte
		if err := rows.Scan(&run.ID, &run.NodeID, &run.Scope, &run.OK, &run.Error,
			&run.DurationUs, &run.CreatedAt, &logs, &tests); err != nil {
			return nil, fmt.Errorf("reading the runs of record %s: %w", recordID, err)
		}
		if err := json.Unmarshal(logs, &run.Logs); err != nil {
			return nil, fmt.Errorf("reading the logs of run %s: %w", run.ID, err)
		}
		if err := json.Unmarshal(tests, &run.Tests); err != nil {
			return nil, fmt.Errorf("reading the tests of run %s: %w", run.ID, err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the runs of record %s: %w", recordID, err)
	}
	return runs, nil
}
