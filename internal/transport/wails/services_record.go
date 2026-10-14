package wails

import (
	"context"
	"errors"

	"schemo/internal/domain"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/record"
)

// RecordsService exposes history, draft execution, and record management to the UI layer.
type RecordsService struct {
	records     *record.UseCase
	drafts      *draft.UseCase
	collections *collection.UseCase
	host        *desktop.Host
}

func NewRecordsService(
	records *record.UseCase,
	drafts *draft.UseCase,
	collections *collection.UseCase,
	host *desktop.Host,
) *RecordsService {
	return &RecordsService{records: records, drafts: drafts, collections: collections, host: host}
}

// Send executes a draft request with inherited collection auth/variables and returns execution ID.
func (s *RecordsService) Send(ctx context.Context, draftID domain.DraftID) (string, error) {
	above, err := s.above(ctx, draftID)
	if err != nil {
		return "", err
	}
	prepared, err := s.drafts.Prepared(ctx, draftID, above.Auth, above.Variables)
	if err != nil {
		return "", err
	}

	input := recordInput(prepared)
	input.Node = string(draftID)
	return s.records.Send(ctx, input)
}

// above returns inherited collection context for a draft, returning empty context if
// omitted/missing.
func (s *RecordsService) above(
	ctx context.Context,
	id domain.DraftID,
) (collection.Above, error) {
	above, err := s.collections.Above(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return collection.Above{}, nil
	}
	return above, err
}

// SendSpec executes an ad-hoc draft seed without modifying active workspace draft state.
func (s *RecordsService) SendSpec(ctx context.Context, seed draft.Seed) (string, error) {
	prepared, err := s.drafts.Prepare(ctx, seed, nil)
	if err != nil {
		return "", err
	}
	return s.records.Send(ctx, recordInput(prepared))
}

// recordInput converts a prepared draft into a record execution input payload.
func recordInput(prepared draft.Prepared) record.SendInput {
	return record.SendInput{
		Method:        prepared.Method,
		URL:           prepared.URL,
		Headers:       prepared.Headers,
		Body:          prepared.Body,
		BodyKind:      prepared.BodyKind,
		Form:          prepared.Form,
		BodyFile:      prepared.BodyFile,
		MaskedURL:     prepared.MaskedURL,
		MaskedHeaders: prepared.MaskedHeaders,
		MaskedBody:    prepared.MaskedBody,
		Digest:        prepared.Digest,
	}
}

// Cancel interrupts an ongoing request attempt by ID.
func (s *RecordsService) Cancel(ctx context.Context, id string) (bool, error) {
	return s.records.Cancel(id), nil
}

// List returns lightweight record summaries for a source without body content.
func (s *RecordsService) List(
	ctx context.Context,
	source domain.RecordSource,
	limit int,
) ([]domain.Record, error) {
	return s.records.List(ctx, source, limit)
}

// Page retrieves historical records prior to a given timestamp, newest first.
func (s *RecordsService) Page(
	ctx context.Context,
	before int64,
	limit int,
) ([]domain.Record, error) {
	return s.records.Page(ctx, before, limit)
}

// Record returns a single detailed record by ID including headers and body refs.
func (s *RecordsService) Record(ctx context.Context, id string) (domain.Record, error) {
	return s.records.Record(ctx, id)
}

// Body retrieves the request or response body content for a record side. A record that is gone is
// answered as gone rather than as an empty body: the window holds rows a deletion elsewhere has
// already taken away, and an empty string tells it the record is here with nothing in it.
func (s *RecordsService) Body(
	ctx context.Context,
	id string,
	side domain.BodySide,
) (string, error) {
	return s.records.Body(ctx, id, side)
}

// UndoCookieWrites reverts cookie mutations produced by a recorded response.
func (s *RecordsService) UndoCookieWrites(ctx context.Context, id string) error {
	return s.records.UndoCookieWrites(ctx, id)
}

// Ingest imports an externally captured record into workspace history.
func (s *RecordsService) Ingest(ctx context.Context, in record.IngestInput) (domain.Record, error) {
	return s.records.Ingest(ctx, in)
}

// Clear deletes history records matching the specified IDs.
func (s *RecordsService) Clear(ctx context.Context, ids []string) error {
	return s.records.Clear(ctx, ids)
}

// ClearAll removes all history records for the current active workspace.
func (s *RecordsService) ClearAll(ctx context.Context) (int, error) {
	return s.records.ClearAll(ctx)
}

// History returns summary metrics for history in the active workspace.
func (s *RecordsService) History(ctx context.Context) (domain.HistoryStats, error) {
	return s.records.History(ctx)
}

// Prune manually executes retention cleanup rules on the active workspace.
func (s *RecordsService) Prune(ctx context.Context) (int, error) {
	return s.records.Prune(ctx)
}
