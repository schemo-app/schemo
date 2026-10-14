package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// SaveRecord persists a record and its bodies in a single transaction. It is not offered to the
// server here: the code around it has not reported yet, and `QueueRecord` offers it after.
func (s *Store) SaveRecord(ctx context.Context, workspaceID string, rec domain.Record) error {
	requestHeaders, err := json.Marshal(domain.OrEmpty(rec.RequestHeaders))
	if err != nil {
		return fmt.Errorf("saving record %s: %w", rec.ID, err)
	}
	responseHeaders, err := json.Marshal(domain.OrEmpty(rec.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("saving record %s: %w", rec.ID, err)
	}
	writes, err := json.Marshal(domain.OrEmpty(rec.CookieWrites))
	if err != nil {
		return fmt.Errorf("saving record %s: %w", rec.ID, err)
	}

	finishedAt := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("saving record %s", rec.ID), func(tx *writeTx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO records (id, workspace_id, source, method, url, status, status_text,
                      content_type, error, cancelled, duration_us, dns_us, connect_us,
                      tls_us, wait_us, download_us, request_bytes, response_bytes,
                      request_headers_json, response_headers_json, cookie_writes_json,
                      started_at, finished_at, node_id, run_id, tab_id, tab_title, tab_url,
                      favicon_url, agent, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			         ?, ?, ?, ?)`,
			rec.ID, workspaceID, string(rec.Source), rec.Method, rec.URL, rec.Status, rec.StatusText,
			rec.ContentType,
			rec.Error, rec.Cancelled, rec.DurationUs, rec.DNSUs, rec.ConnectUs, rec.TLSUs,
			rec.WaitUs, rec.DownloadUs, rec.RequestBytes, rec.ResponseBytes,
			string(requestHeaders), string(responseHeaders), string(writes), rec.StartedAt, finishedAt,
			rec.NodeID, nullIfEmpty(rec.RunID), nullIfZero(rec.TabID), nullIfEmpty(rec.TabTitle),
			nullIfEmpty(rec.TabURL), nullIfEmpty(rec.FavIconURL), rec.Agent, finishedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("record %s: %w", rec.ID, domain.ErrConflict)
			}
			return fmt.Errorf("saving record %s: %w", rec.ID, err)
		}

		for _, side := range []struct {
			kind domain.BodySide
			body *domain.BodyRef
		}{
			{domain.SideRequest, rec.RequestBody},
			{domain.SideResponse, rec.ResponseBody},
		} {
			if side.body == nil {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO record_bodies (record_id, side, content, size, truncated)
				 VALUES (?, ?, ?, ?, ?)`,
				rec.ID, string(side.kind), []byte(side.body.Inline), side.body.Size,
				side.body.Truncated); err != nil {
				return fmt.Errorf("saving the %s body of %s: %w", side.kind, rec.ID, err)
			}
		}

		return nil
	})
}

// recordColumns defines the common SQL projection list for record metadata queries.
const recordColumns = `id, workspace_id, source, method, url, status, status_text,
	       content_type, error, cancelled, duration_us, dns_us, connect_us, tls_us, wait_us,
	       download_us, request_bytes, response_bytes, request_headers_json,
	       response_headers_json, cookie_writes_json, started_at, node_id, ifnull(run_id, ''),
	       ifnull(tab_id, 0), ifnull(tab_title, ''), ifnull(tab_url, ''),
	       ifnull(favicon_url, ''), is_remote, ifnull(agent, '')`

// rowScanner abstracts scanning methods shared between sql.Row and sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row rowScanner) (domain.Record, error) {
	var (
		rec                                     domain.Record
		requestHeaders, responseHeaders, writes string
	)
	if err := row.Scan(&rec.ID, &rec.WorkspaceID, &rec.Source, &rec.Method, &rec.URL,
		&rec.Status, &rec.StatusText, &rec.ContentType, &rec.Error, &rec.Cancelled, &rec.DurationUs,
		&rec.DNSUs, &rec.ConnectUs, &rec.TLSUs, &rec.WaitUs, &rec.DownloadUs, &rec.RequestBytes,
		&rec.ResponseBytes, &requestHeaders, &responseHeaders, &writes, &rec.StartedAt,
		&rec.NodeID, &rec.RunID, &rec.TabID, &rec.TabTitle, &rec.TabURL, &rec.FavIconURL,
		&rec.IsRemote, &rec.Agent); err != nil {
		return domain.Record{}, err
	}
	for _, part := range []struct {
		raw  string
		into any
		what string
	}{
		{requestHeaders, &rec.RequestHeaders, "request headers"},
		{responseHeaders, &rec.ResponseHeaders, "response headers"},
		{writes, &rec.CookieWrites, "cookie writes"},
	} {
		if err := json.Unmarshal([]byte(part.raw), part.into); err != nil {
			return domain.Record{}, fmt.Errorf("reading the %s of %s: %w", part.what, rec.ID, err)
		}
	}
	return rec, nil
}

// Records lists workspace records matching the given source filter without inline body payloads.
func (s *Store) Records(
	ctx context.Context,
	workspaceID string,
	source domain.RecordSource,
	limit int,
) ([]domain.Record, error) {
	scope, args := listScope(workspaceID, source, limit)

	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM records`+scope, args...)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}
	defer rows.Close()

	out := []domain.Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("listing records: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	if err := s.attachBodyRefs(ctx, out, scope, args); err != nil {
		return nil, err
	}
	return out, nil
}

// Record reads a single record by ID including its body references.
func (s *Store) Record(ctx context.Context, id string) (domain.Record, error) {
	rec, err := scanRecord(s.db.QueryRowContext(ctx,
		`SELECT `+recordColumns+` FROM records WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Record{}, fmt.Errorf("record %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return domain.Record{}, fmt.Errorf("reading record %s: %w", id, err)
	}
	records := []domain.Record{rec}
	if err := s.attachBodyRefs(ctx, records,
		` WHERE id = ? AND deleted_at IS NULL`, []any{id}); err != nil {
		return domain.Record{}, err
	}
	return records[0], nil
}

// attachBodyRefs populates body size and truncation metadata for the provided records.
func (s *Store) attachBodyRefs(
	ctx context.Context,
	records []domain.Record,
	scope string,
	args []any,
) error {
	if len(records) == 0 {
		return nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT record_id, side, size, truncated
		   FROM record_bodies
		  WHERE record_id IN (SELECT id FROM records`+scope+`)`, args...)
	if err != nil {
		return fmt.Errorf("listing the bodies of records: %w", err)
	}
	defer rows.Close()

	at := make(map[string]*domain.Record, len(records))
	for i := range records {
		at[records[i].ID] = &records[i]
	}

	for rows.Next() {
		var (
			id        string
			side      domain.BodySide
			size      int64
			truncated int
		)
		if err := rows.Scan(&id, &side, &size, &truncated); err != nil {
			return fmt.Errorf("listing the bodies of records: %w", err)
		}
		rec, ok := at[id]
		if !ok {
			continue
		}
		ref := &domain.BodyRef{Size: size, Truncated: truncated != 0}
		if side == domain.SideRequest {
			rec.RequestBody = ref
		} else {
			rec.ResponseBody = ref
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	declaredSizes(at)
	return nil
}

// declaredSizes sets default body references for remote records missing local body rows.
func declaredSizes(at map[string]*domain.Record) {
	for _, rec := range at {
		if !rec.IsRemote {
			continue
		}
		if rec.RequestBody == nil && rec.RequestBytes > 0 {
			rec.RequestBody = &domain.BodyRef{Size: rec.RequestBytes}
		}
		if rec.ResponseBody == nil && rec.ResponseBytes > 0 {
			rec.ResponseBody = &domain.BodyRef{Size: rec.ResponseBytes}
		}
	}
}

// listScope builds the SQL WHERE and ORDER BY clauses for record filtering.
func listScope(workspaceID string, source domain.RecordSource, limit int) (string, []any) {
	scope := ` WHERE workspace_id = ? AND deleted_at IS NULL AND cached_at IS NULL`
	args := []any{workspaceID}
	if source != "" {
		scope += ` AND source = ?`
		args = append(args, string(source))
	}
	scope += ` ORDER BY started_at DESC, seq DESC`
	if limit > 0 {
		scope += ` LIMIT ?`
		args = append(args, limit)
	}
	return scope, args
}

// RecordsBefore retrieves a paginated set of records created prior to a given timestamp.
func (s *Store) RecordsBefore(
	ctx context.Context,
	workspaceID string,
	before int64,
	limit int,
) ([]domain.Record, error) {
	scope := ` WHERE workspace_id = ? AND deleted_at IS NULL AND started_at < ?
	           ORDER BY started_at DESC, seq DESC LIMIT ?`
	args := []any{workspaceID, before, limit}

	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM records`+scope, args...)
	if err != nil {
		return nil, fmt.Errorf("listing older records: %w", err)
	}
	defer rows.Close()

	out := []domain.Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("listing older records: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing older records: %w", err)
	}
	if err := s.attachBodyRefs(ctx, out, scope, args); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveCached stores remote records fetched on demand and marks them with a cached timestamp.
func (s *Store) SaveCached(ctx context.Context, workspaceID string, records []domain.Record) error {
	return s.inTx(ctx, "keeping older records read from the server", func(tx *writeTx) error {
		at := now()
		for _, record := range records {
			if err := writeRecord(ctx, tx, workspaceID, record, 0, &at); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadBody retrieves the string content of a single record body by record ID and side.
func (s *Store) ReadBody(ctx context.Context, id string, side domain.BodySide) (string, error) {
	var content []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT content FROM record_bodies WHERE record_id = ? AND side = ?`,
		id, string(side)).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("the %s body of %s: %w", side, id, domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading the %s body of %s: %w", side, id, err)
	}
	return string(content), nil
}

// QueueRecord offers a record to the server, once the code around it has reported: a push that
// went out before would send the record without the reports that hang off it.
func (s *Store) QueueRecord(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, fmt.Sprintf("offering record %s", id), func(tx *writeTx) error {
		return enqueue(ctx, tx, workspaceID, entityRecord, id, actionCreate)
	})
}

// SaveCookieWrites updates cookie write metadata for a record and enqueues an outbox update.
func (s *Store) SaveCookieWrites(
	ctx context.Context,
	workspaceID, id string,
	writes []domain.CookieWrite,
) error {
	encoded, err := json.Marshal(domain.OrEmpty(writes))
	if err != nil {
		return fmt.Errorf("saving the cookie writes of %s: %w", id, err)
	}
	return s.inTx(ctx, fmt.Sprintf("saving the cookie writes of %s", id), func(tx *writeTx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE records SET cookie_writes_json = ?, updated_at = ? WHERE id = ?`,
			string(encoded), time.Now().UnixMilli(), id)
		if err != nil {
			return fmt.Errorf("saving the cookie writes of %s: %w", id, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("saving the cookie writes of %s: %w", id, err)
		}
		if changed == 0 {
			return nil
		}
		return enqueue(ctx, tx, workspaceID, entityRecord, id, actionUpdate)
	})
}

// DeleteRecords soft-deletes records, removes associated bodies/scripts, and enqueues deletions.
func (s *Store) DeleteRecords(ctx context.Context, workspaceID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.inTx(ctx, "deleting records", func(tx *writeTx) error {
		for _, id := range ids {
			gone, err := deleteRecord(ctx, tx, id)
			if err != nil {
				return err
			}
			if !gone {
				continue
			}
			if err := enqueue(ctx, tx, workspaceID, entityRecord, id, actionDelete); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteAllRecords hard-deletes all records for a workspace without queueing outbox entries.
func (s *Store) DeleteAllRecords(ctx context.Context, workspaceID string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM records WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("clearing the history of %s: %w", workspaceID, err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("counting the cleared records of %s: %w", workspaceID, err)
	}
	if count > 0 {
		if _, err := s.db.ExecContext(ctx, vacuum); err != nil {
			return int(count), fmt.Errorf("reclaiming space: %w", err)
		}
	}
	return int(count), nil
}

// HistoryStats returns total active records count and aggregate body size in bytes for a workspace.
func (s *Store) HistoryStats(ctx context.Context, workspaceID string) (domain.HistoryStats, error) {
	var stats domain.HistoryStats
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM records
		         WHERE workspace_id = ? AND deleted_at IS NULL AND cached_at IS NULL),
		       (SELECT coalesce(sum(b.size), 0) FROM record_bodies b
		          JOIN records r ON r.id = b.record_id
		         WHERE r.workspace_id = ? AND r.deleted_at IS NULL)`, workspaceID, workspaceID).
		Scan(&stats.Count, &stats.Bytes)
	if err != nil {
		return domain.HistoryStats{}, fmt.Errorf("weighing the history of %s: %w", workspaceID, err)
	}
	return stats, nil
}

// Prune purges records and cached entries exceeding age or count retention limits.
func (s *Store) Prune(
	ctx context.Context,
	workspaceID string,
	opts domain.PruneOptions,
) (int, error) {
	total := 0
	drop := func(what, query string, args ...any) error {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("pruning records %s: %w", what, err)
		}
		count, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("counting records pruned %s: %w", what, err)
		}
		total += int(count)
		return nil
	}

	if opts.MaxAge > 0 {
		cutoff := time.Now().Add(-opts.MaxAge).UnixMilli()
		if err := drop("older than "+opts.MaxAge.String(),
			`DELETE FROM records
			  WHERE workspace_id = ? AND cached_at IS NULL AND started_at < ?`,
			workspaceID, cutoff); err != nil {
			return total, err
		}
	}
	if opts.MaxCount > 0 {
		if err := drop(fmt.Sprintf("beyond %d", opts.MaxCount),
			`DELETE FROM records WHERE seq IN (
			   SELECT seq FROM records WHERE workspace_id = ? AND cached_at IS NULL
			    ORDER BY started_at DESC, seq DESC LIMIT -1 OFFSET ?)`,
			workspaceID, opts.MaxCount); err != nil {
			return total, err
		}
	}

	if opts.CacheAge > 0 {
		cutoff := time.Now().Add(-opts.CacheAge).UnixMilli()
		if err := drop("read on request too long ago",
			`DELETE FROM records WHERE workspace_id = ? AND cached_at < ?`,
			workspaceID, cutoff); err != nil {
			return total, err
		}
	}
	if opts.CacheCount > 0 {
		if err := drop(fmt.Sprintf("read on request beyond %d", opts.CacheCount),
			`DELETE FROM records WHERE seq IN (
			   SELECT seq FROM records WHERE workspace_id = ? AND cached_at IS NOT NULL
			    ORDER BY cached_at DESC, seq DESC LIMIT -1 OFFSET ?)`,
			workspaceID, opts.CacheCount); err != nil {
			return total, err
		}
	}

	if total > 0 {
		if _, err := s.db.ExecContext(ctx, vacuum); err != nil {
			return total, fmt.Errorf("reclaiming space: %w", err)
		}
	}
	return total, nil
}

func nullIfZero(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

// SaveFetched writes fetched remote record body payloads into the database on conflict ignore.
func (s *Store) SaveFetched(
	ctx context.Context,
	workspaceID, id string,
	fetched domain.RecordFetched,
) error {
	return s.inTx(ctx, fmt.Sprintf("keeping a fetched record %s", id), func(tx *writeTx) error {
		for side, body := range fetched.Bodies {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO record_bodies (record_id, side, content, size, truncated)
				 VALUES (?, ?, ?, ?, ?)
				 ON CONFLICT(record_id, side) DO NOTHING`,
				id, string(side), []byte(body.Inline), body.Size,
				body.Truncated); err != nil {
				return fmt.Errorf("keeping the %s body of %s: %w", side, id, err)
			}
		}
		for _, report := range fetched.Scripts {
			if err := writeScriptRun(ctx, tx, workspaceID, report); err != nil {
				return err
			}
		}
		if fetched.Run != nil {
			if err := writeRun(ctx, tx, *fetched.Run); err != nil {
				return err
			}
		}
		return nil
	})
}
