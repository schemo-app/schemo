package record

import (
	"context"
	"errors"
	"log"
	"time"

	"schemo/internal/domain"
)

// Ingest stores an incoming record and publishes a notification to update the UI.
func (u *UseCase) Ingest(ctx context.Context, in IngestInput) (domain.Record, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	if in, err = u.maskedCapture(ctx, workspace, in); err != nil {
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
			ID:          u.ids(),
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
			domain.HostOf(in.URL)),
		RequestBody:   bodyRef(in.RequestBody, false),
		ResponseBody:  bodyRef(in.ResponseBody, false),
		RequestBytes:  int64(len(in.RequestBody)),
		ResponseBytes: int64(len(in.ResponseBody)),
		// Nil indicates the timing phase was not reported (distinguishes absent from 0ms).
		WaitUs:     millisToMicros(in.WaitMs),
		DownloadUs: millisToMicros(in.DownloadMs),
	}

	if err := u.save(ctx, workspace, rec); err != nil {
		return domain.Record{}, err
	}
	u.offer(ctx, workspace, rec.ID)
	u.notifier.Publish(TopicRecordAdded, rec)
	return rec, nil
}

// List returns the latest records for a source without bodies for performance.
func (u *UseCase) List(
	ctx context.Context,
	source domain.RecordSource,
	limit int,
) ([]domain.Record, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = domain.HistoryKeepCount
	}
	return u.store.Records(ctx, workspace, source, limit)
}

// SpaceRecords is the history of one named space, for a reader that is not the window: the MCP
// server reads the history of spaces the window is not showing. Every source is read — an agent
// asking what happened here last is asking about the space, not about who sent it — and only what
// this device holds: reaching the server for older rows is the history page's own errand.
func (u *UseCase) SpaceRecords(
	ctx context.Context,
	workspace string,
	limit int,
) ([]domain.Record, error) {
	if limit <= 0 {
		limit = domain.HistoryKeepCount
	}
	return u.store.Records(ctx, workspace, "", limit)
}

// Page returns records before a given timestamp, fetching and caching remote data if needed.
func (u *UseCase) Page(
	ctx context.Context,
	before int64,
	limit int,
) ([]domain.Record, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = domain.HistoryKeepCount
	}
	if before <= 0 {
		before = time.Now().UnixMilli()
	}

	local, err := u.store.RecordsBefore(ctx, workspace, before, limit)
	if err != nil || len(local) >= limit {
		return local, err
	}

	// Fetch remote records continuing from the oldest local record timestamp.
	from := before
	if len(local) > 0 {
		from = local[len(local)-1].StartedAt
	}
	older, err := u.source.Older(ctx, workspace, from, limit-len(local))
	if err != nil {
		// Return local records on network failure to support offline view.
		log.Printf("[record] older history: %v", err)
		return local, nil
	}
	if len(older) == 0 {
		return local, nil
	}
	if err := u.store.SaveCached(ctx, workspace, older); err != nil {
		return nil, err
	}
	return u.store.RecordsBefore(ctx, workspace, before, limit)
}

// Record returns a single record by ID including its bodies.
func (u *UseCase) Record(ctx context.Context, id string) (domain.Record, error) {
	return u.store.Record(ctx, id)
}

// Body retrieves a record body, fetching and caching it from the remote source if missing.
func (u *UseCase) Body(ctx context.Context, id string, side domain.BodySide) (string, error) {
	body, err := u.store.ReadBody(ctx, id, side)
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		return body, err
	}

	// Local records always contain their bodies; missing body means it was never present.
	record, err := u.store.Record(ctx, id)
	if err != nil {
		return "", err
	}
	if !record.IsRemote {
		return "", nil
	}

	fetched, err := u.source.Fetched(ctx, record.WorkspaceID, id)
	if err != nil {
		return "", err
	}
	if err := u.store.SaveFetched(ctx, record.WorkspaceID, id, fetched); err != nil {
		return "", err
	}
	return fetched.Bodies[side].Inline, nil
}

// Clear deletes records by their IDs.
func (u *UseCase) Clear(ctx context.Context, ids []string) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return u.store.DeleteRecords(ctx, workspace, ids)
}

// ClearAll removes all history for the active workspace and notifies observers.
func (u *UseCase) ClearAll(ctx context.Context) (int, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := u.store.DeleteAllRecords(ctx, workspace)
	if err != nil {
		return removed, err
	}
	u.notifier.Publish(TopicHistoryCleared, HistoryCleared{WorkspaceID: workspace, Removed: removed})
	return removed, nil
}

// History returns summary statistics for the active workspace.
func (u *UseCase) History(ctx context.Context) (domain.HistoryStats, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.HistoryStats{}, err
	}
	return u.store.HistoryStats(ctx, workspace)
}

func bodyRef(text string, truncated bool) *domain.BodyRef {
	return &domain.BodyRef{Inline: text, Size: int64(len(text)), Truncated: truncated}
}

// maskedCapture is a capture as history keeps it: everything the browser reported about the request
// has gone through the same masking a send from this app does — the address, the tab it came from,
// the body and the value of every header row — so a value this device holds as a secret is not
// written down in the clear because it passed through a page rather than through the builder. What
// came back is left as it is, exactly as a send leaves it.
//
// Only the values are looked at, so nothing changes for a capture that carries none of them.
func (u *UseCase) maskedCapture(
	ctx context.Context,
	workspace string,
	in IngestInput,
) (IngestInput, error) {
	if u.mask == nil {
		return in, nil
	}
	texts := make([]string, 0, 3+len(in.RequestHeaders))
	texts = append(texts, in.URL, in.TabURL, in.RequestBody)
	for _, header := range in.RequestHeaders {
		texts = append(texts, header.Value)
	}

	masked, err := u.mask.MaskTexts(ctx, workspace, texts)
	if err != nil {
		return in, err
	}
	in.URL, in.TabURL, in.RequestBody = masked[0], masked[1], masked[2]
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
