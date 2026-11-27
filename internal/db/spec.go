package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"schemo/internal/domain"
)

// kindSpec is where this device reads a collection's API description. Like a draft it never travels:
// a file path means nothing on another device.
const kindSpec domain.EntityType = "spec"

// maxSnapshot bounds what a snapshot unpacks to: it arrives from other devices.
const maxSnapshot = 64 << 20

func specID(workspaceID, collectionID string) string {
	return "spec/" + workspaceID + "/" + collectionID
}

// SpecLink is where this device reads a collection's description; false when it keeps no such place.
func (s *Store) SpecLink(ctx context.Context, workspaceID, collectionID string) (domain.SpecLink, bool, error) {
	row, err := getEntity(ctx, s.db, kindSpec, specID(workspaceID, collectionID))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.SpecLink{}, false, nil
	}
	if err != nil {
		return domain.SpecLink{}, false, err
	}
	link, err := decodeData[domain.SpecLink](row)
	if err != nil {
		return domain.SpecLink{}, false, err
	}
	return link, true, nil
}

func putSpecLink(ctx context.Context, tx *writeTx, workspaceID string, link domain.SpecLink) error {
	data, err := encodeData(link)
	if err != nil {
		return err
	}
	return putEntity(ctx, tx, entity{Kind: kindSpec, ID: specID(workspaceID, link.CollectionID),
		Workspace: workspaceID, Parent: link.CollectionID, Data: data})
}

func dropSpecLinks(ctx context.Context, tx *writeTx, workspaceID string, collections []string) error {
	for _, id := range collections {
		if _, err := tx.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, specID(workspaceID, id)); err != nil {
			return fmt.Errorf("unlinking collection %s: %w", id, err)
		}
	}
	return nil
}

// SpecSnapshot is what a collection's description said at the last update; nil when none is kept.
func (s *Store) SpecSnapshot(ctx context.Context, collectionID string) (json.RawMessage, error) {
	packed, err := packedSnapshot(ctx, s.db, collectionID)
	if err != nil || len(packed) == 0 {
		return nil, err
	}
	return unpack(packed)
}

// packedSnapshot is the snapshot as a collection's row holds it; nil for none or a new collection.
func packedSnapshot(ctx context.Context, q querier, collectionID string) ([]byte, error) {
	row, err := getEntity(ctx, q, domain.EntityCollection, collectionID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record, err := decodeData[collectionRecord](row)
	return record.SpecSnapshot, err
}

// keepSnapshot writes what a collection's description said into its row. An unchanged one owes the
// server nothing.
func keepSnapshot(ctx context.Context, tx *writeTx, collectionID string, snapshot json.RawMessage) error {
	row, err := getEntity(ctx, tx, domain.EntityCollection, collectionID)
	if err != nil {
		return err
	}
	record, err := decodeData[collectionRecord](row)
	if err != nil {
		return err
	}
	packed, err := pack(snapshot)
	if err != nil {
		return err
	}
	if bytes.Equal(record.SpecSnapshot, packed) {
		return nil
	}
	record.SpecSnapshot = packed
	if row.Data, err = encodeData(record); err != nil {
		return err
	}
	row.UpdatedAt = 0
	return putEntity(ctx, tx, row)
}

// pack gzips a snapshot, which mirrors the collection's requests and travels in its record. The same
// text packs to the same bytes.
func pack(data []byte) ([]byte, error) {
	var out bytes.Buffer
	zipped := gzip.NewWriter(&out)
	if _, err := zipped.Write(data); err != nil {
		return nil, fmt.Errorf("packing a snapshot: %w", err)
	}
	if err := zipped.Close(); err != nil {
		return nil, fmt.Errorf("packing a snapshot: %w", err)
	}
	return out.Bytes(), nil
}

func unpack(packed []byte) ([]byte, error) {
	zipped, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("unpacking a snapshot: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(zipped, maxSnapshot+1))
	if err != nil {
		return nil, fmt.Errorf("unpacking a snapshot: %w", err)
	}
	if len(data) > maxSnapshot {
		return nil, fmt.Errorf("unpacking a snapshot: more than %d bytes", maxSnapshot)
	}
	return data, nil
}

// SaveTree writes an update from a description whole: new folders parents first, changed levels,
// requests, deletions, what the description said and where this device reads it.
func (s *Store) SaveTree(ctx context.Context, workspaceID string, update domain.TreeUpdate) error {
	return s.inTx(ctx, "updating collection "+update.Link.CollectionID, func(tx *writeTx) error {
		for _, level := range update.Levels {
			if err := putCollection(ctx, tx, workspaceID, level); err != nil {
				return err
			}
		}
		for _, patch := range update.Patches {
			if err := patchLevel(ctx, tx, workspaceID, patch); err != nil {
				return err
			}
		}
		for _, node := range update.Requests {
			parent, err := getEntity(ctx, tx, domain.EntityCollection, node.CollectionID)
			if err != nil {
				return fmt.Errorf("saving node %s: %w", node.ID, err)
			}
			row, err := nodeRow(parent.Workspace, node)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, row); err != nil {
				return err
			}
		}
		deleted := make([]entity, 0, len(update.Deleted))
		for _, id := range update.Deleted {
			row, err := getEntity(ctx, tx, domain.EntityRequest, id)
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			deleted = append(deleted, row)
		}
		if err := deleteEntities(ctx, tx, workspaceID, deleted); err != nil {
			return err
		}
		// An update that says nothing of the description must not erase what was kept.
		if update.Snapshot != nil {
			if err := keepSnapshot(ctx, tx, update.Link.CollectionID, update.Snapshot); err != nil {
				return err
			}
		}
		if update.Link.Source.Location == "" {
			return nil
		}
		return putSpecLink(ctx, tx, workspaceID, update.Link)
	})
}

func patchLevel(ctx context.Context, tx *writeTx, workspaceID string, patch domain.LevelPatch) error {
	row, err := getEntity(ctx, tx, domain.EntityCollection, patch.ID)
	if err != nil {
		return err
	}
	level, err := collectionFrom(row)
	if err != nil {
		return err
	}
	level.Name, level.Description = patch.Name, patch.Description
	level.Auth, level.Variables = patch.Auth, patch.Variables
	return putCollection(ctx, tx, workspaceID, level)
}
