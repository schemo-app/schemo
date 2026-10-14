package sqlite

// What the scripting feature keeps: the code of every level, and the report of every run.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// NULL is "not set here" and a JSON object is "nothing to run": the difference between inheriting
// and having nothing to add. An empty object is still a value, and it is written as one.
func encodeScripts(scripts *domain.Scripts) (sql.NullString, error) {
	if scripts == nil {
		return sql.NullString{}, nil
	}
	encoded, err := json.Marshal(scripts)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encoding the scripts: %w", err)
	}
	return sql.NullString{String: string(encoded), Valid: true}, nil
}

// Scripts reads what is set on a level — a collection, a node, or the draft the command line is
// composing — and nothing when nothing is set there: the editor shows the difference between "take
// the parent's" and "nothing to run here".
//
// The workspace is named because one of the three tables needs it: the command line's draft is the
// same fixed key in every workspace, so without it this query would read a level of the wrong space
// — whichever row the plan happened to reach first.
func (s *Store) Scripts(ctx context.Context, workspaceID, id string) (*domain.Scripts, error) {
	var raw sql.NullString
	// Three tables and one id space: a level is a collection, a node of one, or the draft the command
	// line is composing, and whoever asks knows the id and not the table.
	err := s.db.QueryRowContext(ctx,
		`SELECT scripts_json FROM collections WHERE id = ? AND deleted_at IS NULL
		 UNION ALL SELECT scripts_json FROM collection_nodes WHERE id = ? AND deleted_at IS NULL
		 UNION ALL SELECT scripts_json FROM drafts WHERE workspace_id = ? AND id = ?`,
		id, id, workspaceID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("level %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the scripts of %s: %w", id, err)
	}
	if !raw.Valid {
		return nil, nil
	}
	var scripts domain.Scripts
	if err := json.Unmarshal([]byte(raw.String), &scripts); err != nil {
		return nil, fmt.Errorf("reading the scripts of %s: %w", id, err)
	}
	return &scripts, nil
}

// SaveScripts writes what is set on a level. NULL stays NULL: a level with no code of its own is a
// level that inherits, and writing an empty object there would turn "take the parents" into
// "nothing to run" — the two the column exists to tell apart.
func (s *Store) SaveScripts(
	ctx context.Context,
	workspaceID, id string,
	scripts *domain.Scripts,
) error {
	encoded, err := encodeScripts(scripts)
	if err != nil {
		return err
	}
	// The draft is the one of the three whose id is the same word in every workspace, so it is the
	// one that has to say which workspace it means: without it, saving the command line's code would
	// write it into every workspace's command line.
	//
	// A level of the tree is a row the other side keeps, so writing its code is a change to it and is
	// queued; the draft is the composer's own buffer, which never leaves the machine, and is written
	// without the outbox. The two are told apart by the entity the level names, and the draft names
	// none.
	const levelSet = `scripts_json = ?, updated_at = ?`
	levels := []struct {
		table  string
		entity domain.EntityType
		where  string
		args   []any
	}{
		{"collections", entityCollection, `id = ? AND deleted_at IS NULL`, []any{id}},
		{"collection_nodes", entityRequest, `id = ? AND deleted_at IS NULL`, []any{id}},
		{"drafts", "", `workspace_id = ? AND id = ?`, []any{workspaceID, id}},
	}
	return s.inTx(ctx, fmt.Sprintf("saving the scripts of %s", id), func(tx *writeTx) error {
		for _, level := range levels {
			result, err := tx.ExecContext(ctx,
				`UPDATE `+level.table+` SET `+levelSet+` WHERE `+level.where,
				append([]any{encoded, time.Now().UnixMilli()}, level.args...)...)
			if err != nil {
				return fmt.Errorf("saving the scripts of %s: %w", id, err)
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("saving the scripts of %s: %w", id, err)
			}
			if changed == 0 {
				continue
			}
			if level.entity == "" {
				return nil
			}
			return enqueue(ctx, tx, workspaceID, level.entity, id, actionUpdate)
		}
		return fmt.Errorf("level %s: %w", id, domain.ErrNotFound)
	})
}

// SaveScriptRun atomically replaces a report with one upsert.
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
