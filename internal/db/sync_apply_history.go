package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"schemo/internal/domain"
)

// ApplyHistoryPage applies a page of remote execution history entries and moves the history cursor.
func (s *Store) ApplyHistoryPage(
	ctx context.Context,
	workspaceID string,
	changes []domain.RecordChange,
	cursor int64,
) (int, error) {
	skipped := 0

	err := s.inTx(ctx, "applying the history the server sent", func(tx *writeTx) error {
		for _, change := range changes {
			if _, err := tx.ExecContext(ctx, `SAVEPOINT row`); err != nil {
				return fmt.Errorf("applying record %s: %w", change.RecordID, err)
			}
			if err := applyRecord(ctx, tx, workspaceID, change); err != nil {
				if _, err := tx.ExecContext(ctx, `ROLLBACK TO row`); err != nil {
					return fmt.Errorf("applying record %s: %w", change.RecordID, err)
				}
				skipped++
				continue
			}
			if _, err := tx.ExecContext(ctx, `RELEASE row`); err != nil {
				return fmt.Errorf("applying record %s: %w", change.RecordID, err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_history_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE
			   SET last_history_seq = max(workspace_sync.last_history_seq, excluded.last_history_seq)`,
			workspaceID, cursor); err != nil {
			return fmt.Errorf("moving the history cursor of %s: %w", workspaceID, err)
		}
		return nil
	})
	return skipped, err
}

func applyRecord(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	change domain.RecordChange,
) error {
	switch {
	case change.Run != nil:
		// A run is an entry of the log with nothing beside it: written once by id, with the records
		// that name it arriving under their own numbers. It is not marked as owed, since the server
		// already holds a run this device was given.
		return writeRun(ctx, tx, *change.Run)
	case change.Deleted:
		_, err := deleteRecord(ctx, tx, change.RecordID)
		return err
	default:
		return writeRecord(ctx, tx, workspaceID, change.Record, change.Seq, nil)
	}
}

// deleteRecord marks a record gone and clears what hangs off it: bodies, report lines and
// assertions. The run stays; it is a document of its own and its other records are still there.
func deleteRecord(ctx context.Context, tx *writeTx, id string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE records SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now(), id)
	if err != nil {
		return false, fmt.Errorf("taking back record %s: %w", id, err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("taking back record %s: %w", id, err)
	}
	if changed == 0 {
		return false, nil
	}
	for _, orphan := range []string{
		`DELETE FROM record_bodies WHERE record_id = ?`,
		`DELETE FROM script_runs WHERE record_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, orphan, id); err != nil {
			return false, fmt.Errorf("clearing what belongs to record %s: %w", id, err)
		}
	}
	return true, nil
}

func writeRecord(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	record domain.Record,
	seq int64,
	cachedAt *int64,
) error {
	headers, err := json.Marshal(domain.OrEmpty(record.RequestHeaders))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	back, err := json.Marshal(domain.OrEmpty(record.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	writes, err := cookieWritesJSON(ctx, tx, record.ID, record.CookieWrites)
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}

	// A write through here says the server holds the record: the cache mark comes off a row the log
	// or a snapshot named, and the server's cookie writes replace the local ones, unless this
	// device still owes a change, because an undo is its own act and must outlive the echo.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO records (id, workspace_id, source, method, url, status, status_text,
		                      content_type, error, cancelled, duration_us, dns_us, connect_us,
		                      tls_us, wait_us, download_us, request_bytes, response_bytes,
		                      request_headers_json, response_headers_json, cookie_writes_json,
		                      started_at, finished_at, node_id, run_id, agent, tab_id, tab_title,
		                      tab_url, favicon_url, updated_at, remote_seq, cached_at, is_remote)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         ?, ?, ?, ?, ?, ?, 1)
		 ON CONFLICT(id) DO UPDATE SET
		     remote_seq = max(records.remote_seq, excluded.remote_seq),
		     cookie_writes_json = CASE
		         WHEN records.is_remote = 1 AND excluded.remote_seq > records.remote_seq
		              AND NOT EXISTS (SELECT 1 FROM sync_queue
		                              WHERE workspace_id = records.workspace_id
		                                AND entity_type = ? AND entity_id = records.id)
		         THEN excluded.cookie_writes_json ELSE records.cookie_writes_json END,
		     cached_at = CASE WHEN excluded.cached_at IS NULL THEN NULL ELSE records.cached_at END`,
		record.ID, workspaceID, string(record.Source), record.Method, record.URL, record.Status,
		record.StatusText, record.ContentType, record.Error, record.Cancelled,
		record.DurationUs, record.DNSUs, record.ConnectUs, record.TLSUs, record.WaitUs,
		record.DownloadUs, record.RequestBytes, record.ResponseBytes,
		string(headers), string(back), writes, record.StartedAt, finishedAt(record),
		record.NodeID, nullIfEmpty(record.RunID), record.Agent, nullIfZero(record.TabID),
		nullIfEmpty(record.TabTitle), nullIfEmpty(record.TabURL), nullIfEmpty(record.FavIconURL),
		now(), seq, cachedAt, string(entityRecord))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	return nil
}

// cookieWritesJSON is what the record's row keeps of the mutation its answer made. A value the
// server did not hand over is one it lacks or this reader may not see, and either way the copy held
// here is the better one.
func cookieWritesJSON(
	ctx context.Context,
	tx *writeTx,
	recordID string,
	writes []domain.CookieWrite,
) (string, error) {
	held, err := localWrites(ctx, tx, recordID, writes)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(domain.OrEmpty(held))
	if err != nil {
		return "", fmt.Errorf("encoding the cookie writes: %w", err)
	}
	return string(encoded), nil
}

// localWrites answers what the arriving record left out. Nothing is read unless a value arrived
// flagged, which is what keeps this free for every record this device made itself.
func localWrites(
	ctx context.Context,
	tx *writeTx,
	recordID string,
	writes []domain.CookieWrite,
) ([]domain.CookieWrite, error) {
	wanted := false
	for _, write := range writes {
		wanted = wanted || write.Withheld || write.PreviousWithheld
	}
	if !wanted {
		return writes, nil
	}

	var stored []byte
	err := tx.QueryRowContext(ctx, `SELECT cookie_writes_json FROM records WHERE id = ?`,
		recordID).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the cookie writes of %s: %w", recordID, err)
	}
	// A cookie is found by what names it — the name, the domain and the path — and the jar holds no
	// two alike.
	byCookie := map[string]domain.CookieWrite{}
	if len(stored) > 0 {
		var rows []domain.CookieWrite
		if err := json.Unmarshal(stored, &rows); err == nil {
			for _, row := range rows {
				byCookie[cookieKey(row)] = row
			}
		}
	}

	kept := make([]domain.CookieWrite, 0, len(writes))
	for _, write := range writes {
		local, found := byCookie[cookieKey(write)]
		if !found {
			kept = append(kept, write)
			continue
		}
		// A device told only what the cookie became, or only what it was, cannot say the other: the
		// side that came flagged comes back whole, and stays flagged where the local copy is
		// missing it too.
		if write.Withheld {
			write.Value, write.Withheld = local.Value, local.Withheld
		}
		if write.PreviousWithheld {
			write.Previous, write.PreviousWithheld = local.Previous, local.PreviousWithheld
		}
		kept = append(kept, write)
	}
	return kept, nil
}

// cookieKey is what tells two cookies apart, and it is the tuple the jar itself keys them by. NUL
// sits in none of the three, so no two cookies can be spelt into one key.
func cookieKey(write domain.CookieWrite) string {
	return write.Name + "\x00" + write.Domain + "\x00" + write.Path
}

func finishedAt(record domain.Record) int64 {
	if record.DurationUs <= 0 {
		return record.StartedAt
	}
	return record.StartedAt + record.DurationUs/1000
}

// ApplyHistorySnapshot writes a full snapshot of remote execution history within a range.
func (s *Store) ApplyHistorySnapshot(
	ctx context.Context,
	workspaceID string,
	records []domain.Record,
	from, head int64,
) error {
	return s.inTx(ctx, "applying a snapshot of the history the server holds", func(tx *writeTx) error {
		bound, held := from, make([]any, 0, len(records)+3)
		for _, record := range records {
			if err := writeRecord(ctx, tx, workspaceID, record, 0, nil); err != nil {
				return err
			}
			held = append(held, record.ID)
			if bound == 0 || record.StartedAt < bound {
				bound = record.StartedAt
			}
		}

		query := `UPDATE records SET deleted_at = ?
		           WHERE workspace_id = ? AND is_remote = 1 AND cached_at IS NULL
		             AND deleted_at IS NULL AND started_at >= ?`
		args := []any{now(), workspaceID, bound}
		if len(held) > 0 {
			query += ` AND id NOT IN (` + placeholders(len(held)) + `)`
			args = append(args, held...)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("dropping what the server no longer holds: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_history_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE SET last_history_seq = excluded.last_history_seq`,
			workspaceID, head); err != nil {
			return fmt.Errorf("moving the history cursor of %s: %w", workspaceID, err)
		}
		return nil
	})
}
