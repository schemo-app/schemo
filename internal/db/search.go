package db

import (
	"context"
	"fmt"
	"slices"

	"schemo/internal/domain"
)

// Matching belongs to the Search service, not this layer: SQLite folds case for ASCII alone, so a
// LIKE would miss a Cyrillic title typed in lower case. Each method hands over its whole area,
// which the schema keeps small on purpose.

func (s *Store) FindRequests(ctx context.Context, workspaceID string) ([]domain.SearchHit, error) {
	byID, err := s.treeRows(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	index := indexOf(byID)
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(domain.EntityRequest), "")
	if err != nil {
		return nil, err
	}
	out := []domain.SearchHit{}
	for _, row := range rows {
		n, err := nodeFrom(row)
		if err != nil {
			return nil, err
		}
		if _, live := index[n.CollectionID]; !live {
			continue
		}
		title := n.URL
		if title == "" {
			title = n.Name
		}
		trail := index.trailOf(n.CollectionID)
		if n.Name != "" && n.Name != title {
			trail = append(trail, n.Name)
		}
		out = append(out, domain.SearchHit{
			Kind:  domain.SearchRequest,
			ID:    n.ID,
			Title: title,
			Path:  trail,
			Badge: n.Method,
			Open:  domain.SearchOpen{Target: domain.TargetRequest, ID: n.ID},
		})
	}
	return out, nil
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

func (s *Store) FindEnvironments(ctx context.Context, workspaceID string) ([]domain.SearchHit, error) {
	state, err := s.EnvState(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := []domain.SearchHit{}
	variable := func(v domain.Variable, scope, scopeName string) domain.SearchHit {
		hit := domain.SearchHit{
			Kind:  domain.SearchEnvironment,
			ID:    v.ID,
			Title: v.Name,
			Open:  domain.SearchOpen{Target: domain.TargetVariable, ID: v.ID, Scope: scope},
		}
		if scope != "" {
			hit.Path = []string{scopeName}
		}
		if v.Kind == domain.VariableText {
			hit.MatchText = v.Value
		}
		return hit
	}
	for _, env := range state.Environments {
		note := domain.SearchNote{}
		if env.ID == state.ActiveID {
			note = domain.SearchNote{Kind: domain.NoteActive}
		}
		out = append(out, domain.SearchHit{
			Kind:  domain.SearchEnvironment,
			ID:    env.ID,
			Title: env.Name,
			Note:  note,
			Open:  domain.SearchOpen{Target: domain.TargetEnvironment, ID: env.ID},
		})
	}
	for _, env := range state.Environments {
		for _, v := range env.Vars {
			out = append(out, variable(v, env.ID, env.Name))
		}
	}
	for _, v := range state.Globals {
		out = append(out, variable(v, "", ""))
	}
	return out, nil
}

// FindHistory reads the calls that were made, newest first. It is the one area that answers an
// empty field ("what was I just doing"), so its order matters.
//
// Bodies are not read: a history row is drawn from its address alone, and a body is not worth
// pulling across for a keystroke.
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

func (s *Store) treeRows(ctx context.Context, workspaceID string) ([]treeRow, error) {
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(domain.EntityCollection), "")
	if err != nil {
		return nil, err
	}
	out := make([]treeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, treeRow{id: row.ID, name: row.Name, parentID: row.Parent})
	}
	return out, nil
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
// collections: moving a collection into itself is refused, but a loop here would hang the window
// rather than fail it.
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
