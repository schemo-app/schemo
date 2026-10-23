package request

import (
	"context"
	"errors"
	"log"
	"slices"
	"time"

	"schemo/internal/domain"
)

// Ingest stores an incoming record and publishes a notification to update the UI.
//
//wails:ignore
func (s *History) Ingest(ctx context.Context, in IngestInput) (domain.Record, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	if in, err = s.maskedCapture(ctx, workspace, in); err != nil {
		return domain.Record{}, err
	}

	started := in.StartedAt
	if started == 0 {
		started = time.Now().UnixMilli()
	}

	source := in.Source
	if source == "" {
		source = domain.SourceBrowser
	}

	rec := domain.Record{
		RecordSummary: domain.RecordSummary{
			ID:          s.ids(),
			WorkspaceID: workspace,
			Source:      source,
			Method:      in.Method,
			URL:         in.URL,
			Status:      in.Status,
			StatusText:  in.StatusText,
			ContentType: in.ContentType,
			DurationUs:  in.DurationMs * 1000,
			StartedAt:   started,
			TabID:       in.TabID,
			TabTitle:    in.TabTitle,
			TabURL:      in.TabURL,
			FavIconURL:  in.FavIconURL,
		},
		RequestHeaders:  domain.OrEmpty(in.RequestHeaders),
		ResponseHeaders: domain.OrEmpty(in.ResponseHeaders),
		// External requests are not executed by this app, so cookie writes are marked as not saved.
		CookieWrites: domain.NotSavedWrites(
			domain.ParseResponseCookies(in.ResponseHeaders, time.UnixMilli(started)),
			in.URL),
		RequestBody:   bodyRef(in.RequestBody, false),
		ResponseBody:  bodyRef(in.ResponseBody, false),
		RequestBytes:  int64(len(in.RequestBody)),
		ResponseBytes: int64(len(in.ResponseBody)),
		// Nil indicates the timing phase was not reported (distinguishes absent from 0ms).
		WaitUs:     millisToMicros(in.WaitMs),
		DownloadUs: millisToMicros(in.DownloadMs),
	}

	if err := s.save(ctx, workspace, rec); err != nil {
		return domain.Record{}, err
	}
	s.offer(ctx, workspace, rec.ID)
	s.notifier.Publish(TopicRecordAdded, rec)
	return rec, nil
}

// List returns the latest records for a source without bodies for performance.
func (s *History) List(
	ctx context.Context,
	source domain.RecordSource,
	limit int,
) ([]domain.Record, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.Records(ctx, workspace, source, orDefaultLimit(limit))
}

// WorkspaceRecords is the history of one named workspace, for a reader that is not the window: the MCP
// server reads workspaces the window is not showing. Every source is read, since an agent asking what
// happened here last is asking about the workspace, and only what this device holds: reaching the
// server for older rows is the history page's errand.
//
//wails:ignore
func (s *History) WorkspaceRecords(
	ctx context.Context,
	workspace string,
	limit int,
) ([]domain.Record, error) {
	return s.store.Records(ctx, workspace, "", orDefaultLimit(limit))
}

// Page returns records before a given timestamp, fetching and caching remote data if needed.
func (s *History) Page(
	ctx context.Context,
	before int64,
	limit int,
) ([]domain.Record, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	limit = orDefaultLimit(limit)
	if before <= 0 {
		before = time.Now().UnixMilli()
	}

	local, err := s.store.RecordsBefore(ctx, workspace, before, limit)
	if err != nil || len(local) >= limit {
		return local, err
	}

	// Fetch remote records continuing from the oldest local record timestamp.
	from := before
	if len(local) > 0 {
		from = local[len(local)-1].StartedAt
	}
	older, err := s.source.Older(ctx, workspace, from, limit-len(local))
	if err != nil {
		// Return local records on network failure to support offline view.
		log.Printf("[record] older history: %v", err)
		return local, nil
	}
	if len(older) == 0 {
		return local, nil
	}
	if err := s.store.SaveCached(ctx, workspace, older); err != nil {
		return nil, err
	}
	return s.store.RecordsBefore(ctx, workspace, before, limit)
}

// Record returns a single record by ID including its bodies.
func (s *History) Record(ctx context.Context, id string) (domain.Record, error) {
	return s.store.Record(ctx, id)
}

// Body retrieves a record body, fetching and caching it from the remote source if missing.
func (s *History) Body(ctx context.Context, id string, side domain.BodySide) (string, error) {
	body, err := s.store.ReadBody(ctx, id, side)
	if !errors.Is(err, domain.ErrNotFound) {
		return body, err
	}

	// Local records always contain their bodies; missing body means it was never present.
	record, err := s.store.Record(ctx, id)
	if err != nil {
		return "", err
	}
	if !record.IsRemote {
		return "", nil
	}

	fetched, err := s.source.Fetched(ctx, record.WorkspaceID, id)
	if err != nil {
		return "", err
	}
	if err := s.store.SaveFetched(ctx, record.WorkspaceID, id, fetched); err != nil {
		return "", err
	}
	return fetched.Bodies[side].Inline, nil
}

// Clear deletes records by their IDs.
func (s *History) Clear(ctx context.Context, ids []string) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.DeleteRecords(ctx, workspace, ids)
}

// ClearAll removes all history for the active workspace and notifies observers.
func (s *History) ClearAll(ctx context.Context) (int, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := s.store.DeleteAllRecords(ctx, workspace)
	if err != nil {
		return removed, err
	}
	s.notifier.Publish(TopicHistoryCleared, HistoryCleared{WorkspaceID: workspace, Removed: removed})
	return removed, nil
}

// Stats returns summary statistics for the active workspace.
func (s *History) Stats(ctx context.Context) (domain.HistoryStats, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.HistoryStats{}, err
	}
	return s.store.HistoryStats(ctx, workspace)
}

// orDefaultLimit is how many records a reader gets when it did not say.
func orDefaultLimit(limit int) int {
	if limit <= 0 {
		return domain.HistoryKeepCount
	}
	return limit
}

func bodyRef(text string, truncated bool) *domain.BodyRef {
	return &domain.BodyRef{Inline: text, Size: int64(len(text)), Truncated: truncated}
}

// maskedCapture is a capture as history keeps it: everything the browser reported about the request
// goes through the same masking a send from this app does (the address, the tab it came from, the
// body and the value of every header row), so a value this device holds as a secret is not written
// down in the clear because it passed through a page. What came back is left as it is, as a send
// leaves it.
//
// Only the values are looked at, so nothing changes for a capture that carries none of them.
func (s *History) maskedCapture(
	ctx context.Context,
	workspace string,
	in IngestInput,
) (IngestInput, error) {
	if s.mask == nil {
		return in, nil
	}
	texts := make([]string, 0, 3+len(in.RequestHeaders))
	texts = append(texts, in.URL, in.TabURL, in.RequestBody)
	for _, header := range in.RequestHeaders {
		texts = append(texts, header.Value)
	}

	masked, err := s.mask.MaskTexts(ctx, workspace, texts)
	if err != nil {
		return in, err
	}
	in.URL, in.TabURL, in.RequestBody = masked[0], masked[1], masked[2]
	in.RequestHeaders = slices.Clone(in.RequestHeaders)
	for i := range in.RequestHeaders {
		in.RequestHeaders[i].Value = masked[3+i]
	}
	return in, nil
}

// millisToMicros converts milliseconds to microseconds, returning nil for non-positive values.
func millisToMicros(ms int64) *int64 {
	if ms <= 0 {
		return nil
	}
	us := ms * 1000
	return &us
}

// UndoCookieWrites takes back what one answer wrote into the jar. The record knows what that answer
// changed (the rows it replaced or deleted) and the jar knows how to put them back, so the two meet
// here.
//
// Undoing twice is not an error: the second puts back what the first already did, and a row already
// the way the write found it is the state the write asked for.
func (s *History) UndoCookieWrites(ctx context.Context, id string) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	rec, err := s.store.Record(ctx, id)
	if err != nil {
		return err
	}
	if len(rec.CookieWrites) == 0 || s.jar == nil {
		return nil
	}
	if err := s.jar.Undo(ctx, workspace, rec.CookieWrites); err != nil {
		return err
	}
	// The record stops claiming what has been taken back. A write that never happened (a blocked
	// domain, an answer this app did not send) stays: it is a fact about that answer, with nothing
	// to undo.
	kept := make([]domain.CookieWrite, 0, len(rec.CookieWrites))
	for _, write := range rec.CookieWrites {
		if write.State == domain.CookieNotSaved {
			kept = append(kept, write)
		}
	}
	if len(kept) == len(rec.CookieWrites) {
		return nil
	}
	return s.store.SaveCookieWrites(ctx, workspace, id, kept)
}

const (
	// cacheAge is the maximum retention duration for cached remote records.
	cacheAge = 14 * 24 * time.Hour
	// cacheCount limits the maximum number of cached remote records preserved.
	cacheCount = 1000
)

// pruneEvery specifies the interval (in saves) for executing background retention pruning.
const pruneEvery = 20

// Prune applies retention count and age rules to the active workspace.
//
//wails:ignore
func (s *History) Prune(ctx context.Context) (int, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	return s.prune(ctx, workspace)
}

func (s *History) prune(ctx context.Context, workspace string) (int, error) {
	opts := domain.PruneOptions{
		MaxCount: domain.HistoryKeepCount, CacheAge: cacheAge, CacheCount: cacheCount,
	}
	if s.retention != nil {
		retention, err := s.retention.Retention(ctx)
		if err != nil {
			return 0, err
		}
		opts.MaxAge = retention.Age()
	}
	return s.store.Prune(ctx, workspace, opts)
}

// offer hands a saved record to the server's queue. The record is already kept and the request it
// records is already answered, so a queue that cannot be written is logged and not reported.
func (s *History) offer(ctx context.Context, workspace, id string) {
	if err := s.store.QueueRecord(ctx, workspace, id); err != nil {
		log.Printf("[record] offering %s: %v", id, err)
	}
}

// save stores a record and periodically triggers background pruning based on save frequency.
func (s *History) save(ctx context.Context, workspace string, rec domain.Record) error {
	if err := s.store.SaveRecord(ctx, workspace, rec); err != nil {
		return err
	}

	if s.sincePrune.Add(1) < pruneEvery {
		return nil
	}
	s.sincePrune.Store(0)
	if _, err := s.prune(ctx, workspace); err != nil {
		// Log retention errors without failing the save operation itself.
		log.Printf("[record] retention: %v", err)
	}
	return nil
}
