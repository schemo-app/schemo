package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"schemo/internal/domain"
)

// entityFor reads the row a queued change names as the entity that travels. The second result is
// false when the row is gone (pruned, or cleared with the history): nobody has to hear about it.
func entityFor(
	ctx context.Context,
	q querier,
	workspace domain.SyncWorkspace,
	change queueRow,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	switch change.Kind {
	case domain.EntityWorkspace:
		return workspaceEntity(ctx, q, workspace)
	case domain.EntityRecord:
		return recordEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityRun:
		return runEntity(ctx, q, change.EntityID, scopes)
	}
	if _, synced := syncKinds[change.Kind]; !synced {
		return domain.SyncEntity{}, false, fmt.Errorf("sending a %s is not a thing this device does",
			change.Kind)
	}
	row, err := anyEntity(ctx, q, change.EntityID)
	if err != nil {
		return absent(err, string(change.Kind), change.EntityID)
	}
	entity := envelope(row.Kind, row.ID, row.Parent, row.Name, row.Version, row.Position, row.Deleted,
		row.CreatedAt, row.UpdatedAt, nil)
	if !row.Deleted {
		entity.Data, entity.Secrets, entity.Withheld, err = outgoing(row, scopes)
		if err != nil {
			return domain.SyncEntity{}, false, err
		}
	}
	return entity, true, nil
}

// absent is a row that is not here, which is an answer and not a failure: an outbox entry may name
// a row pruned or cleared before it was sent. Anything else is a real error and says which row.
func absent(err error, what, id string) (domain.SyncEntity, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SyncEntity{}, false, nil
	}
	return domain.SyncEntity{}, false, fmt.Errorf("reading %s %s: %w", what, id, err)
}

// envelope is the part of an entity every kind has. A tombstone carries no data: what a deletion
// says is in the envelope.
func envelope(kind domain.EntityType, id, parent, name string, version, position int64, deleted bool,
	createdAt, updatedAt int64, data []byte) domain.SyncEntity {
	entity := domain.SyncEntity{
		Kind:      kind,
		ID:        id,
		Parent:    parent,
		Name:      name,
		Version:   int32(version),
		Deleted:   deleted,
		Position:  position,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if !deleted {
		entity.Data = data
	}
	return entity
}

// workspaceEntity is a workspace's own row. The server made its own id when the workspace was created there
// and the link remembers it, so the server's id travels, never the local one.
func workspaceEntity(
	ctx context.Context,
	q querier,
	workspace domain.SyncWorkspace,
) (domain.SyncEntity, bool, error) {
	var (
		name, colour         string
		version              int64
		createdAt, updatedAt int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT name, color, version, created_at, updated_at FROM workspaces WHERE id = ?`,
		workspace.ID).Scan(&name, &colour, &version, &createdAt, &updatedAt)
	if err != nil {
		return absent(err, "workspace", workspace.ID)
	}

	var payload []byte
	if colour != "" {
		payload, err = encodeData(map[string]string{"color": colour})
		if err != nil {
			return domain.SyncEntity{}, false, err
		}
	}
	// The workspace's place among a person's workspaces is its own list's business and does not travel, so
	// the envelope's ordering is left empty.
	return envelope(domain.EntityWorkspace, workspace.RemoteID, "", name, version, 0, false,
		createdAt, updatedAt, payload), true, nil
}

// recordEntity is one sent request with everything that hangs off it: the bodies, the run that
// produced it and what the code around it reported.
//
// It is one payload because it is one document (a report without its record is a report of nothing)
// and the only heavy one: two bodies at the size of a download.
func recordEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	record, err := scanRecord(q.QueryRowContext(ctx,
		`SELECT `+recordColumns+` FROM records WHERE id = ?`, id))
	if err != nil {
		return absent(err, "record", id)
	}

	var deletedAt sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT deleted_at FROM records WHERE id = ?`, id).
		Scan(&deletedAt); err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("reading record %s: %w", id, err)
	}
	if deletedAt.Valid {
		// A record somebody took back is a `history/v1.Delete`, and what that call needs is the id
		// alone: the bodies are already gone from this device.
		return envelope(domain.EntityRecord, id, "", record.Method+" "+record.URL, 0, 0, true,
			record.StartedAt, record.StartedAt, nil), true, nil
	}

	entry, err := historyEntry(ctx, q, record, scopes)
	if err != nil {
		return domain.SyncEntity{}, false, err
	}
	payload, err := proto.Marshal(entry)
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding record %s: %w", id, err)
	}
	return envelope(domain.EntityRecord, id, "", record.Method+" "+record.URL, 0, 0, false,
		record.StartedAt, record.StartedAt, payload), true, nil
}
