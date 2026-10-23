package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	historyv1 "github.com/schemo-app/proto/go/history/v1"
	"github.com/schemo-app/proto/secret"

	"schemo/internal/domain"
)

// One sent request as `history/v1` carries it: the record as a JSON document, its two bodies and,
// when the person lets runs out, what the code around it reported. The run that produced it is a
// document of its own.

// historyEntry reads a record and everything that hangs off it.
func historyEntry(
	ctx context.Context,
	q querier,
	record domain.Record,
	scopes domain.SyncScopes,
) (*historyv1.RecordEntry, error) {
	bodies, err := bodiesOf(ctx, q, record.ID)
	if err != nil {
		return nil, err
	}
	data, secrets, withheld, err := domain.RecordDocument(record)
	if err != nil {
		return nil, err
	}
	if !scopes.Secrets {
		var more []string
		if data, more, err = secret.Withhold(data, secrets); err != nil {
			return nil, fmt.Errorf("withholding the secrets of record %s: %w", record.ID, err)
		}
		withheld = append(withheld, more...)
	}

	entry := historyv1.RecordEntry_builder{
		Record: historyv1.Record_builder{
			Id:        record.ID,
			Source:    RecordSource(string(record.Source)),
			NodeId:    record.NodeID,
			RunId:     record.RunID,
			StartedAt: moment(record.StartedAt),
			Data:      data,
			Secrets:   secrets,
			Withheld:  withheld,
		}.Build(),
		Bodies: bodies,
	}
	// What the scripts reported is what a person lets out or keeps: the request itself is the record,
	// and it travels either way.
	if scopes.Runs {
		reports, err := scriptRuns(ctx, q, record.ID)
		if err != nil {
			return nil, err
		}
		for i := range reports {
			reports[i].RecordID = ""
		}
		if len(reports) > 0 {
			if entry.Scripts, err = json.Marshal(reports); err != nil {
				return nil, fmt.Errorf("encoding the reports of record %s: %w", record.ID, err)
			}
		}
	}
	return entry.Build(), nil
}

// runEntity is one collection run as `history/v1` carries it, once by its id however many records
// name it. Its rows name the records the run produced, and the records name it back.
func runEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	if !scopes.Runs {
		// A device that keeps its runs at home owes the server nothing about them.
		return domain.SyncEntity{}, false, nil
	}

	run := domain.CollectionRun{ID: id}
	err := q.QueryRowContext(ctx,
		`SELECT collection_id, ifnull(node_id, ''), environment, started_at, ifnull(finished_at, 0),
		        passed, failed, duration_us
		   FROM collection_runs WHERE id = ?`, id).
		Scan(&run.CollectionID, &run.NodeID, &run.Environment, &run.StartedAt, &run.FinishedAt,
			&run.Passed, &run.Failed, &run.DurationUs)
	if err != nil {
		return absent(err, "run", id)
	}
	if run.Results, err = resultsOf(ctx, q, id); err != nil {
		return domain.SyncEntity{}, false, err
	}
	data, err := json.Marshal(run)
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding run %s: %w", id, err)
	}
	payload, err := proto.Marshal(historyv1.Run_builder{
		Id:           id,
		CollectionId: run.CollectionID,
		StartedAt:    moment(run.StartedAt),
		Data:         data,
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding run %s: %w", id, err)
	}
	return envelope(domain.EntityRun, id, "", "run of "+run.CollectionID, 0, 0, false,
		run.StartedAt, run.StartedAt, payload), true, nil
}

// bodiesOf reads the two sides of a record. A body too large to keep whole was never kept whole,
// and the size beside it says what it really weighed.
func bodiesOf(ctx context.Context, q querier, recordID string) ([]*historyv1.RecordBody, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT side, content, size, truncated FROM record_bodies WHERE record_id = ?`, recordID)
	if err != nil {
		return nil, fmt.Errorf("reading the bodies of %s: %w", recordID, err)
	}
	defer rows.Close()

	out := []*historyv1.RecordBody{}
	for rows.Next() {
		var (
			side      string
			content   []byte
			size      int64
			truncated int
		)
		if err := rows.Scan(&side, &content, &size, &truncated); err != nil {
			return nil, fmt.Errorf("reading the bodies of %s: %w", recordID, err)
		}
		out = append(out, historyv1.RecordBody_builder{
			Side: BodySide(side), Content: content, Size: size, Truncated: truncated != 0,
		}.Build())
	}
	return out, rows.Err()
}

func resultsOf(ctx context.Context, q querier, runID string) ([]domain.CollectionRunResult, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT node_id, position, status, ok, duration_us, error, ifnull(record_id, ''),
		        assertions_passed, assertions_total, failure, skipped
		   FROM collection_run_results WHERE run_id = ? ORDER BY position`, runID)
	if err != nil {
		return nil, fmt.Errorf("reading the rows of run %s: %w", runID, err)
	}
	defer rows.Close()

	out := []domain.CollectionRunResult{}
	for rows.Next() {
		var (
			row     domain.CollectionRunResult
			status  sql.NullInt64
			failure string
		)
		if err := rows.Scan(&row.NodeID, &row.Position, &status, &row.OK, &row.DurationUs,
			&row.Error, &row.RecordID, &row.AssertionsPassed, &row.AssertionsTotal, &failure,
			&row.Skipped); err != nil {
			return nil, fmt.Errorf("reading the rows of run %s: %w", runID, err)
		}
		if status.Valid {
			code := int(status.Int64)
			row.Status = &code
		}
		if failure != "" {
			row.Failure = &domain.Failure{}
			if err := json.Unmarshal([]byte(failure), row.Failure); err != nil {
				return nil, fmt.Errorf("reading a failure of run %s: %w", runID, err)
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// moment is a moment kept as Unix milliseconds, as the contract carries it. A zero is left out
// rather than sent as 1970.
func moment(millis int64) *timestamppb.Timestamp {
	if millis == 0 {
		return nil
	}
	return timestamppb.New(time.UnixMilli(millis))
}
