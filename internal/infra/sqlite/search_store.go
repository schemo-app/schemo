package sqlite

import (
	"context"
	"fmt"
	"slices"

	"schemo/internal/domain"
)

// Matching is the search use case's, not this layer's: SQLite folds case for ASCII alone, so a LIKE
// here would leave a Cyrillic title unfindable by the same title typed in lower case: what is
// searched is whatever a person typed, and can be in any script. Each method therefore hands over
// its whole area, which the schema keeps small on purpose.

// FindRequests reads every saved request with the collections above it, so that a row can name the
// way down to itself.
func (s *Store) FindRequests(ctx context.Context, workspaceID string) ([]domain.SearchHit, error) {
	byID, err := s.treeRows(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	index := indexOf(byID)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, collection_id, name, ifnull(method, ''), ifnull(url, '')
		   FROM collection_nodes
		  WHERE deleted_at IS NULL
		    AND collection_id IN (SELECT id FROM collections
		                           WHERE workspace_id = ? AND deleted_at IS NULL)
		  ORDER BY collection_id, position, created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searching collection requests: %w", err)
	}
	defer rows.Close()

	out := []domain.SearchHit{}
	for rows.Next() {
		var id, collectionID, name, method, url string
		if err := rows.Scan(&id, &collectionID, &name, &method, &url); err != nil {
			return nil, fmt.Errorf("searching collection requests: %w", err)
		}
		// A request is drawn by its address when it has one, and by its name when it does not: a
		// request made a moment ago is named and not yet addressed, and a row drawn by an empty
		// address is a row that says nothing but its method. Whichever of the two is not the title
		// is the last step of the trail, so neither is drawn twice and both are searchable.
		title := url
		if title == "" {
			title = name
		}
		trail := index.trailOf(collectionID)
		if name != "" && name != title {
			trail = append(trail, name)
		}
		out = append(out, domain.SearchHit{
			Kind:  domain.SearchRequest,
			ID:    id,
			Title: title,
			Path:  trail,
			Badge: method,
			Open:  domain.SearchOpen{Target: domain.TargetRequest, ID: id},
		})
	}
	return out, rows.Err()
}

// FindCollections reads every collection and the collections above it. A folder is a collection
// with a parent — the schema has never told the two apart — so both are one area here.
func (s *Store) FindCollections(
	ctx context.Context,
	workspaceID string,
) ([]domain.SearchHit, error) {
	rows, err := s.treeRows(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	index := indexOf(rows)

	out := make([]domain.SearchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.SearchHit{
			Kind: domain.SearchCollection,
			ID:   row.id,
			// The name is the last step of the way down, so the trail above it is what is left.
			Title: row.name,
			Path:  index.trailOf(row.parentID),
			Open:  domain.SearchOpen{Target: domain.TargetCollection, ID: row.id},
		})
	}
	return out, nil
}

// FindEnvironments reads the environments and the variables inside them. A variable answers with
// the name it is drawn by, and is matched against its value as well — but only when it is text: a
// secret's value is not put next to a query, not even to compare it, so a secret is found by its
// name and never by what it holds.
func (s *Store) FindEnvironments(
	ctx context.Context,
	workspaceID string,
) ([]domain.SearchHit, error) {
	// The same read the workspace's own side makes, and it answers the same way: a workspace that is
	// not there is not found rather than a failure of the database, and one column read two ways is
	// one column that can disagree with itself.
	active, err := s.activeEnvironment(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	environments, err := s.db.QueryContext(ctx,
		`SELECT id, name FROM environments
		  WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY position`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searching environments: %w", err)
	}
	defer environments.Close()

	out := []domain.SearchHit{}
	names := map[string]string{}
	for environments.Next() {
		var id, name string
		if err := environments.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("searching environments: %w", err)
		}
		names[id] = name
		note := domain.SearchNote{}
		if id == active {
			note = domain.SearchNote{Kind: domain.NoteActive}
		}
		out = append(out, domain.SearchHit{
			Kind:  domain.SearchEnvironment,
			ID:    id,
			Title: name,
			Note:  note,
			Open:  domain.SearchOpen{Target: domain.TargetEnvironment, ID: id},
		})
	}
	if err := environments.Err(); err != nil {
		return nil, fmt.Errorf("searching environments: %w", err)
	}

	variables, err := s.db.QueryContext(ctx,
		`SELECT id, ifnull(scope_id, ''), name, value, kind
		   FROM variables WHERE workspace_id = ? AND deleted_at IS NULL
		  ORDER BY scope_kind, position`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searching variables: %w", err)
	}
	defer variables.Close()

	for variables.Next() {
		var id, scope, name, value, kind string
		if err := variables.Scan(&id, &scope, &name, &value, &kind); err != nil {
			return nil, fmt.Errorf("searching variables: %w", err)
		}
		hit := domain.SearchHit{
			Kind:  domain.SearchEnvironment,
			ID:    id,
			Title: name,
			Open:  domain.SearchOpen{Target: domain.TargetVariable, ID: id, Scope: scope},
		}
		// A global has no environment above it, and the empty trail is the honest drawing of that.
		if scope != "" {
			hit.Path = []string{names[scope]}
		}
		if kind == string(domain.VariableText) {
			hit.MatchText = value
		}
		out = append(out, hit)
	}
	return out, variables.Err()
}

// FindHistory reads the calls that were made, newest first. It is the one area that answers an
// empty field — "what was I just doing" — which is why the order it keeps is the one that matters.
//
// The bodies are not read: a history row is drawn from its address alone, and a body is the one
// thing in this database that is worth not pulling across for a keystroke.
func (s *Store) FindHistory(ctx context.Context, workspaceID string) ([]domain.SearchHit, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, method, url, started_at, ifnull(tab_title, '')
		   FROM records WHERE workspace_id = ? AND deleted_at IS NULL
		  ORDER BY started_at DESC, seq DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("searching records: %w", err)
	}
	defer rows.Close()

	out := []domain.SearchHit{}
	for rows.Next() {
		var id, method, url, tabTitle string
		var startedAt int64
		if err := rows.Scan(&id, &method, &url, &startedAt, &tabTitle); err != nil {
			return nil, fmt.Errorf("searching records: %w", err)
		}
		var path []string
		if tabTitle != "" {
			path = []string{tabTitle}
		}
		out = append(out, domain.SearchHit{
			Kind:  domain.SearchHistory,
			ID:    id,
			Title: url,
			Path:  path,
			Badge: method,
			At:    startedAt,
			Open:  domain.SearchOpen{Target: domain.TargetHistory, ID: id},
		})
	}
	return out, rows.Err()
}

type treeRow struct {
	id       string
	name     string
	parentID string
}

// treeRows reads every collection of a workspace in the order the tree keeps them. The order is
// what the caller draws when nothing else separates two rows, so it is read here rather than left
// to a map — which would shuffle equal rows between one keystroke and the next.
func (s *Store) treeRows(ctx context.Context, workspaceID string) ([]treeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, ifnull(parent_id, '') FROM collections
		  WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	out := []treeRow{}
	for rows.Next() {
		var row treeRow
		if err := rows.Scan(&row.id, &row.name, &row.parentID); err != nil {
			return nil, fmt.Errorf("listing collections: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// treeIndex is the collections by id, which is what naming a way down costs: the tree is read whole
// and walked in memory rather than asking the database for each parent.
type treeIndex map[string]treeRow

func indexOf(rows []treeRow) treeIndex {
	index := make(treeIndex, len(rows))
	for _, row := range rows {
		index[row.id] = row
	}
	return index
}

// trailOf names the way down to a collection, outermost first. The walk is bounded by the number of
// collections rather than by trust: moving a collection into itself is refused, and a loop here
// would hang the window rather than fail it.
func (t treeIndex) trailOf(id string) []string {
	trail := []string{}
	for id != "" && len(trail) <= len(t) {
		row, ok := t[id]
		if !ok {
			break
		}
		trail = append(trail, row.name)
		id = row.parentID
	}
	slices.Reverse(trail)
	return trail
}
