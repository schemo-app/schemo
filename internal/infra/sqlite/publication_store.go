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

// The local record of what this device has published. It is this device's own note and not a copy
// of the server's: it is written so that a window can say «Опубликована» and «Изменения · 1» before
// it reaches anybody, and it is not synchronized — a second device publishes for itself.

// publicationColumns is the shape every read here answers in, in one place so that a column added
// in one query is not forgotten in another.
const publicationColumns = `collection_id, workspace_id, publication_id, slug, public_url,
	visibility, status, revision, content_hash, environment_name, can_manage, pending_unpublish,
	updated_at, outline, changes, options`

// Publication is what is known about a collection's page, and whether anything is known at all. A
// collection nobody published answers nothing rather than an error: a question nobody asked is not
// a failure.
func (s *Store) Publication(
	ctx context.Context,
	collectionID string,
) (domain.Publication, bool, error) {
	query := `SELECT ` + publicationColumns + ` FROM publications WHERE collection_id = ?`

	row := s.db.QueryRowContext(ctx, query, collectionID)
	held, err := scannedPublication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Publication{}, false, nil
	}
	if err != nil {
		return domain.Publication{}, false, fmt.Errorf("reading a publication: %w", err)
	}
	return held, true, nil
}

// OfWorkspace is every page this device knows of in a space, newest first.
func (s *Store) OfWorkspace(ctx context.Context, workspace string) ([]domain.Publication, error) {
	query := `SELECT ` + publicationColumns + ` FROM publications WHERE workspace_id = ?
		ORDER BY updated_at DESC, collection_id`

	rows, err := s.db.QueryContext(ctx, query, workspace)
	if err != nil {
		return nil, fmt.Errorf("listing publications: %w", err)
	}
	defer rows.Close()
	return readPublications(rows)
}

// AwaitingUnpublish is what a takedown never reached the server about, oldest first: a page taken
// down off the network is one this device owes the server an answer for.
func (s *Store) AwaitingUnpublish(ctx context.Context) ([]domain.Publication, error) {
	query := `SELECT ` + publicationColumns + ` FROM publications
		 WHERE pending_unpublish = 1 AND publication_id <> ''
		ORDER BY updated_at`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing the takedowns owed: %w", err)
	}
	defer rows.Close()
	return readPublications(rows)
}

// SavePublication writes what the server answered, over whatever was known before. The row is the
// collection's and not the publication's: a collection republished at a new address replaces the
// old one, and the address it no longer answers at is not one this window can speak of.
func (s *Store) SavePublication(ctx context.Context, held domain.Publication) error {
	const query = `
		INSERT INTO publications (collection_id, workspace_id, publication_id, slug, public_url,
			visibility, status, revision, content_hash, environment_name, can_manage,
			pending_unpublish, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(collection_id) DO UPDATE SET
			workspace_id = excluded.workspace_id, publication_id = excluded.publication_id,
			slug = excluded.slug, public_url = excluded.public_url,
			visibility = excluded.visibility, status = excluded.status,
			revision = excluded.revision, content_hash = excluded.content_hash,
			environment_name = excluded.environment_name, can_manage = excluded.can_manage,
			pending_unpublish = excluded.pending_unpublish,
			updated_at = excluded.updated_at`

	if held.UpdatedAt == 0 {
		held.UpdatedAt = time.Now().UnixMilli()
	}
	if _, err := s.db.ExecContext(ctx, query,
		held.CollectionID, held.WorkspaceID, held.ID, held.Slug, held.URL, held.Visibility,
		held.Status, held.Revision, held.ContentHash, held.EnvironmentName, held.CanManage,
		held.PendingUnpublish, held.UpdatedAt); err != nil {
		return fmt.Errorf("saving a publication: %w", err)
	}
	return nil
}

// SavePublished keeps what this device alone knows about a page: the shape the last publish put up
// and what has moved at it since. It is a write of its own because every other write of a row here
// is the server's answer, and an answer says nothing about either of the two.
func (s *Store) SavePublished(
	ctx context.Context,
	collectionID string,
	outline []domain.OutlineItem,
	changes []domain.PageRelease,
	options domain.PublicationOptions,
) error {
	encodedOutline, err := json.Marshal(outline)
	if err != nil {
		return fmt.Errorf("writing the outline of publication %s: %w", collectionID, err)
	}
	encodedChanges, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("writing the changes at publication %s: %w", collectionID, err)
	}

	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("writing publication options: %w", err)
	}
	const query = `UPDATE publications SET outline = ?, changes = ?, options = ? WHERE collection_id = ?`
	_, err = s.db.ExecContext(ctx, query, string(encodedOutline), string(encodedChanges), string(encodedOptions), collectionID)
	if err != nil {
		return fmt.Errorf("saving the history of publication %s: %w", collectionID, err)
	}
	return nil
}

// ForgetPublication drops what is known about a collection's page. It is not how a page is taken
// down — the server is — and it is not offered to a person: a device that forgot a page it
// published would offer to publish it again at a second address.
func (s *Store) ForgetPublication(ctx context.Context, collectionID string) error {
	const query = `DELETE FROM publications WHERE collection_id = ?`

	if _, err := s.db.ExecContext(ctx, query, collectionID); err != nil {
		return fmt.Errorf("forgetting a publication: %w", err)
	}
	return nil
}

// scannedPublication reads one row of publicationColumns. The space is read back with the rest
// because a row read here is written again: a takedown is the same row with two fields changed.
func scannedPublication(row rowScanner) (domain.Publication, error) {
	var (
		held                      domain.Publication
		outline, changes, options string
	)
	err := row.Scan(&held.CollectionID, &held.WorkspaceID, &held.ID, &held.Slug, &held.URL,
		&held.Visibility, &held.Status, &held.Revision, &held.ContentHash, &held.EnvironmentName,
		&held.CanManage, &held.PendingUnpublish, &held.UpdatedAt, &outline, &changes, &options)
	if err != nil {
		return domain.Publication{}, err
	}

	// A row from a device that has not published through this build holds nothing here, and nothing
	// is not a document: a page with no history is drawn rather than one that refuses to open.
	for _, part := range []struct {
		raw  string
		into any
		what string
	}{
		{outline, &held.Outline, "the outline"},
		{changes, &held.Changes, "the changes"},
		{options, &held.Options, "the display options"},
	} {
		if part.raw == "" {
			continue
		}
		if err := json.Unmarshal([]byte(part.raw), part.into); err != nil {
			return domain.Publication{}, fmt.Errorf("reading %s of publication %s: %w",
				part.what, held.CollectionID, err)
		}
	}
	return held, nil
}

// readPublications reads every row of publicationColumns a query answered with.
func readPublications(rows *sql.Rows) ([]domain.Publication, error) {
	out := []domain.Publication{}
	for rows.Next() {
		held, err := scannedPublication(rows)
		if err != nil {
			return nil, fmt.Errorf("reading a publication: %w", err)
		}
		out = append(out, held)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading publications: %w", err)
	}
	return out, nil
}
