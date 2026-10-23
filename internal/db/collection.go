package db

import (
	"context"
	"errors"
	"fmt"

	"schemo/internal/domain"
)

func collectionRow(workspace string, c domain.Collection) (entity, error) {
	c.Items, c.Children = nil, nil
	c.Variables = append([]domain.Variable(nil), c.Variables...)
	for i := range c.Variables {
		c.Variables[i].HasValue = false
	}
	data, err := encodeData(c)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityCollection, ID: c.ID, Workspace: workspace, Parent: c.ParentID,
		Position: c.Position, Name: c.Name, Data: data}, nil
}

func collectionFrom(e entity) (domain.Collection, error) {
	c, err := decodeData[domain.Collection](e)
	c.ID, c.ParentID, c.Position, c.Name = e.ID, e.Parent, e.Position, e.Name
	c.CreatedAt, c.UpdatedAt = e.CreatedAt, e.UpdatedAt
	c.Items, c.Children = []domain.CollectionNode{}, []domain.Collection{}
	for i := range c.Variables {
		c.Variables[i].HasValue = c.Variables[i].Value != ""
	}
	return c, err
}

func nodeRow(workspace string, n domain.CollectionNode) (entity, error) {
	n.BodyKind = domain.KindOf(n.BodyKind)
	data, err := encodeData(n)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityRequest, ID: n.ID, Workspace: workspace, Parent: n.CollectionID,
		Position: n.Position, Name: n.Name, Data: data}, nil
}

func nodeFrom(e entity) (domain.CollectionNode, error) {
	n, err := decodeData[domain.CollectionNode](e)
	n.ID, n.CollectionID, n.Position, n.Name = e.ID, e.Parent, e.Position, e.Name
	n.CreatedAt, n.UpdatedAt = e.CreatedAt, e.UpdatedAt
	n.BodyKind = domain.KindOf(n.BodyKind)
	return n, err
}

// Collections reads the whole tree of a workspace. Tree rows carry what the sidebar draws and what
// inheritance needs (auth, variables), not the request payloads.
func (s *Store) Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error) {
	rows, err := listEntities(ctx, s.db, workspaceID,
		kinds(domain.EntityCollection, domain.EntityRequest), "")
	if err != nil {
		return nil, err
	}
	flat := []domain.Collection{}
	items := map[string][]domain.CollectionNode{}
	for _, row := range rows {
		if row.Kind == domain.EntityRequest {
			n, err := nodeFrom(row)
			if err != nil {
				return nil, err
			}
			items[n.CollectionID] = append(items[n.CollectionID], domain.CollectionNode{
				ID: n.ID, CollectionID: n.CollectionID, Name: n.Name, Position: n.Position,
				Method: n.Method, Description: n.Description, Auth: n.Auth,
			})
			continue
		}
		c, err := collectionFrom(row)
		if err != nil {
			return nil, err
		}
		flat = append(flat, c)
	}
	for i := range flat {
		if held := items[flat[i].ID]; len(held) > 0 {
			flat[i].Items = held
		}
	}
	return nestCollections(flat), nil
}

func nestCollections(flat []domain.Collection) []domain.Collection {
	byParent := map[string][]domain.Collection{}
	for _, c := range flat {
		byParent[c.ParentID] = append(byParent[c.ParentID], c)
	}
	// The depth limit guards against a parent cycle in a damaged database.
	var build func(parent string, depth int) []domain.Collection
	build = func(parent string, depth int) []domain.Collection {
		children := byParent[parent]
		if children == nil || depth > len(flat) {
			return []domain.Collection{}
		}
		for i := range children {
			children[i].Children = build(children[i].ID, depth+1)
		}
		return children
	}
	return build("", 0)
}

// WorkspaceOf is the workspace a collection belongs to.
func (s *Store) WorkspaceOf(ctx context.Context, collectionID string) (string, error) {
	e, err := getEntity(ctx, s.db, domain.EntityCollection, collectionID)
	if err != nil {
		return "", err
	}
	return e.Workspace, nil
}

func (s *Store) Node(ctx context.Context, id string) (domain.CollectionNode, error) {
	e, err := getEntity(ctx, s.db, domain.EntityRequest, id)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	return nodeFrom(e)
}

// subtree returns a collection and everything under it, depth first, each level in its own order.
func subtree(ctx context.Context, q querier, workspaceID, id string) ([]entity, error) {
	root, err := getEntity(ctx, q, domain.EntityCollection, id)
	if err != nil {
		return nil, err
	}
	all, err := listEntities(ctx, q, workspaceID, kinds(domain.EntityCollection, domain.EntityRequest), "")
	if err != nil {
		return nil, err
	}
	byParent := map[string][]entity{}
	for _, e := range all {
		byParent[e.Parent] = append(byParent[e.Parent], e)
	}
	out := []entity{}
	var walk func(e entity, depth int)
	walk = func(e entity, depth int) {
		out = append(out, e)
		if e.Kind != domain.EntityCollection || depth > len(all) {
			return
		}
		for _, child := range byParent[e.ID] {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return out, nil
}

// ContentRows lists every request inside a collection, folders included, in the order a run walks
// them: a level's requests and folders share one position sequence.
func (s *Store) ContentRows(ctx context.Context, collectionID string) ([]domain.LevelRow, error) {
	root, err := getEntity(ctx, s.db, domain.EntityCollection, collectionID)
	if errors.Is(err, domain.ErrNotFound) {
		return []domain.LevelRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	all, err := listEntities(ctx, s.db, root.Workspace,
		kinds(domain.EntityCollection, domain.EntityRequest), "")
	if err != nil {
		return nil, err
	}
	byParent := map[string][]entity{}
	for _, e := range all {
		byParent[e.Parent] = append(byParent[e.Parent], e)
	}
	out := []domain.LevelRow{}
	var walk func(parent string, depth int) error
	walk = func(parent string, depth int) error {
		if depth > len(all) {
			return nil
		}
		for _, e := range byParent[parent] {
			if e.Kind == domain.EntityCollection {
				if err := walk(e.ID, depth+1); err != nil {
					return err
				}
				continue
			}
			n, err := nodeFrom(e)
			if err != nil {
				return err
			}
			out = append(out, domain.LevelRow{ID: n.ID, Name: n.Name, Method: n.Method, URL: n.URL})
		}
		return nil
	}
	return out, walk(collectionID, 0)
}

func (s *Store) SaveCollection(ctx context.Context, workspaceID string, c domain.Collection) error {
	row, err := collectionRow(workspaceID, c)
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving collection "+c.ID, func(tx *writeTx) error {
		return putEntity(ctx, tx, row)
	})
}

func (s *Store) SaveNode(ctx context.Context, node domain.CollectionNode) error {
	return s.inTx(ctx, "saving node "+node.ID, func(tx *writeTx) error {
		parent, err := getEntity(ctx, tx, domain.EntityCollection, node.CollectionID)
		if err != nil {
			return fmt.Errorf("saving node %s: %w", node.ID, err)
		}
		row, err := nodeRow(parent.Workspace, node)
		if err != nil {
			return err
		}
		return putEntity(ctx, tx, row)
	})
}

// DeleteCollection marks a collection and its whole subtree gone, folders first, and drops the runs
// of every collection in it.
func (s *Store) DeleteCollection(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, "deleting collection "+id, func(tx *writeTx) error {
		rows, err := subtree(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		var folders, requests []entity
		var ids []any
		for _, row := range rows {
			if row.Kind == domain.EntityCollection {
				folders = append(folders, row)
				ids = append(ids, row.ID)
			} else {
				requests = append(requests, row)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM collection_runs WHERE collection_id IN (`+
			placeholders(len(ids))+`)`, ids...); err != nil {
			return fmt.Errorf("deleting the runs of %s: %w", id, err)
		}
		return deleteEntities(ctx, tx, workspaceID, append(folders, requests...))
	})
}

func (s *Store) DeleteNode(ctx context.Context, id string) error {
	return s.inTx(ctx, "deleting node "+id, func(tx *writeTx) error {
		row, err := getEntity(ctx, tx, domain.EntityRequest, id)
		if err != nil {
			return nil
		}
		return deleteEntities(ctx, tx, row.Workspace, []entity{row})
	})
}

func (s *Store) NextPosition(ctx context.Context, workspaceID, collectionID string) (int64, error) {
	var next int64
	err := s.db.QueryRowContext(ctx,
		`SELECT ifnull(max(position), -1) + 1 FROM entities
		  WHERE workspace_id = ? AND parent_id = ? AND kind IN (?, ?) AND deleted_at IS NULL`,
		workspaceID, collectionID, domain.EntityCollection, domain.EntityRequest).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("reading the next position in %s: %w", collectionID, err)
	}
	return next, nil
}

// MoveNode puts a request into a collection at a position and renumbers both levels.
func (s *Store) MoveNode(ctx context.Context, workspaceID, id, collectionID string, position int64) error {
	return s.move(ctx, workspaceID, domain.EntityRequest, id, collectionID, position)
}

// MoveCollection puts a folder under a parent (empty for the top) at a position.
func (s *Store) MoveCollection(ctx context.Context, workspaceID, id, parentID string, position int64) error {
	return s.move(ctx, workspaceID, domain.EntityCollection, id, parentID, position)
}

func (s *Store) move(
	ctx context.Context,
	workspaceID string,
	kind domain.EntityType,
	id, parent string,
	position int64,
) error {
	return s.inTx(ctx, fmt.Sprintf("moving %s %s", kind, id), func(tx *writeTx) error {
		row, err := getEntity(ctx, tx, kind, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE entities SET parent_id = ?, updated_at = ? WHERE id = ?`,
			parent, now(), id); err != nil {
			return fmt.Errorf("moving %s: %w", id, err)
		}
		if err := enqueue(ctx, tx, workspaceID, kind, id, actionUpdate); err != nil {
			return err
		}
		if row.Parent != parent {
			if err := renumber(ctx, tx, workspaceID, row.Parent, "", 0); err != nil {
				return err
			}
		}
		return renumber(ctx, tx, workspaceID, parent, id, position)
	})
}

// renumber gives a level's folders and requests positions 0..n-1, with moved (if any) placed at at.
func renumber(ctx context.Context, tx *writeTx, workspaceID, parent, moved string, at int64) error {
	level, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityCollection, domain.EntityRequest),
		`parent_id = ?`, parent)
	if err != nil {
		return err
	}
	if moved != "" {
		for _, row := range level {
			if row.ID == moved {
				level = placeAt(level, row, at)
				break
			}
		}
	}
	for i, row := range level {
		if row.Position == int64(i) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE entities SET position = ?, updated_at = ? WHERE id = ?`,
			i, now(), row.ID); err != nil {
			return fmt.Errorf("numbering %s: %w", row.ID, err)
		}
		if err := enqueue(ctx, tx, workspaceID, row.Kind, row.ID, actionUpdate); err != nil {
			return err
		}
	}
	return nil
}

// placeAt moves one row of a level to index at, clamped to the level.
func placeAt(level []entity, moved entity, at int64) []entity {
	others := make([]entity, 0, len(level))
	insert := 0
	for i, row := range level {
		if row.ID == moved.ID {
			continue
		}
		if int64(i) < at {
			insert++
		}
		others = append(others, row)
	}
	insert = min(max(insert, 0), len(others))
	out := append([]entity{}, others[:insert]...)
	out = append(out, moved)
	return append(out, others[insert:]...)
}
