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

// Collections assembles the tree here rather than in the window: the order rows are drawn in is
// the order this table keeps, and a tree assembled in two places is a tree that can disagree with
// itself.
func (s *Store) Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, position, created_at, updated_at, auth_json, ifnull(parent_id, ''),
		        variables_json, docs_authorization, docs_errors, docs_limits
		   FROM collections WHERE workspace_id = ? AND deleted_at IS NULL
		  ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	flat := []domain.Collection{}
	for rows.Next() {
		var (
			c         domain.Collection
			auth      sql.NullString
			variables sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Position, &c.CreatedAt, &c.UpdatedAt,
			&auth, &c.ParentID, &variables, &c.Docs.Authorization, &c.Docs.Errors,
			&c.Docs.Limits); err != nil {
			return nil, fmt.Errorf("listing collections: %w", err)
		}
		if auth.Valid {
			var value domain.Auth
			if err := json.Unmarshal([]byte(auth.String), &value); err != nil {
				return nil, fmt.Errorf("reading the auth of collection %s: %w", c.ID, err)
			}
			c.Auth = &value
		}
		// The variables travel with the tree: every request inside asks for them on the way out, and
		// a second query per send would be a query per keystroke of a preview.
		if c.Variables, err = decodeVariables(variables, "collection "+c.ID); err != nil {
			return nil, err
		}
		// Empty rather than nil: a level with nothing in it is drawn as nothing, and the window
		// would have to guard every walk otherwise.
		c.Items = []domain.CollectionNode{}
		c.Children = []domain.Collection{}
		flat = append(flat, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	if len(flat) == 0 {
		return flat, nil
	}

	nodes, err := s.nodes(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	// Requests are hung on the collection that holds them, which is a lookup rather than a walk: the
	// rows already came in the order the tree keeps.
	byCollection := map[string][]domain.CollectionNode{}
	for _, node := range nodes {
		byCollection[node.CollectionID] = append(byCollection[node.CollectionID], node)
	}
	for i := range flat {
		if items := byCollection[flat[i].ID]; len(items) > 0 {
			flat[i].Items = items
		}
	}
	return nestCollections(flat), nil
}

// The requests and the collections of a level stay apart although they share one number line:
// merging the two lists by position is drawing a row, and that is the window's.
func nestCollections(flat []domain.Collection) []domain.Collection {
	byParent := map[string][]domain.Collection{}
	for _, c := range flat {
		byParent[c.ParentID] = append(byParent[c.ParentID], c)
	}

	// The walk carries how deep it has gone, and stops at the number of rows there are. A tree is at
	// most that deep, so a walk that is deeper is one going round: a `parent_id` that names one of its
	// own descendants — a hand-edited database, a migration that went wrong — would otherwise recurse
	// until the stack ran out, which is a crash rather than an answer. Nothing legitimate reaches the
	// limit, and the branch beyond it is what keeps the app standing.
	var build func(parent string, depth int) []domain.Collection
	build = func(parent string, depth int) []domain.Collection {
		if depth > len(flat) {
			return []domain.Collection{}
		}
		children := byParent[parent]
		if children == nil {
			return []domain.Collection{}
		}
		for i := range children {
			children[i].Children = build(children[i].ID, depth+1)
		}
		return children
	}
	return build("", 0)
}

// SpaceOf is the space a collection belongs to, for a caller that reached the id from outside the
// window — the MCP server reads spaces the window is not showing, and its policy is about spaces. A
// request is reached through its node's CollectionID, which the node itself carries.
func (s *Store) SpaceOf(ctx context.Context, collectionID string) (string, error) {
	var workspace string
	err := s.db.QueryRowContext(ctx,
		`SELECT workspace_id FROM collections WHERE id = ? AND deleted_at IS NULL`, collectionID).
		Scan(&workspace)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("collection %s: %w", collectionID, domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading the space of collection %s: %w", collectionID, err)
	}
	return workspace, nil
}

// Node reads one node with everything a request is made of, which is what opening it needs and what
// the tree deliberately left out.
func (s *Store) Node(ctx context.Context, id string) (domain.CollectionNode, error) {
	var (
		node                        domain.CollectionNode
		params, headers             sql.NullString
		pathParams                  sql.NullString
		auth, scripts               sql.NullString
		description                 sql.NullString
		url, body, method, bodyKind sql.NullString
		form, bodyFile              sql.NullString
		environmentID               sql.NullString
		responses                   sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, collection_id, name, position, description, auth_json, scripts_json,
		        method, url, params_json, headers_json, path_params_json, body, body_kind, form_json,
		        body_file, environment_id, created_at, updated_at, responses_json
		   FROM collection_nodes WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&node.ID, &node.CollectionID, &node.Name, &node.Position, &description,
			&auth, &scripts, &method, &url, &params, &headers, &pathParams, &body, &bodyKind, &form,
			&bodyFile, &environmentID, &node.CreatedAt, &node.UpdatedAt, &responses)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CollectionNode{}, fmt.Errorf("collection node %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return domain.CollectionNode{}, fmt.Errorf("reading collection node %s: %w", id, err)
	}

	node.Description = description.String
	node.Method = method.String
	node.URL = url.String
	node.Body = body.String
	// A node that has no kind yet holds nothing, and nothing is text: the same normalising the wire
	// gets, so an unsaved node cannot reach a caller as a kind that has no meaning.
	node.BodyKind = domain.KindOf(domain.BodyKind(bodyKind.String))
	node.BodyFile = bodyFile.String
	node.EnvironmentID = environmentID.String
	if form.Valid {
		if err := json.Unmarshal([]byte(form.String), &node.Form); err != nil {
			return domain.CollectionNode{}, fmt.Errorf("reading the form of node %s: %w", id, err)
		}
	}
	for _, part := range []struct {
		raw  sql.NullString
		into any
		what string
	}{
		{params, &node.Params, "params"},
		{headers, &node.Headers, "headers"},
		{pathParams, &node.PathParams, "path parameters"},
		{responses, &node.Responses, "responses"},
	} {
		// A row written by something other than this app holds nothing in a column the schema allows
		// to be empty, and nothing is not a document: the node keeps no rows of that kind rather than
		// failing to open. The same reading form, auth and scripts get above.
		if !part.raw.Valid || part.raw.String == "" {
			continue
		}
		if err := json.Unmarshal([]byte(part.raw.String), part.into); err != nil {
			return domain.CollectionNode{}, fmt.Errorf("reading the %s of node %s: %w", part.what, id, err)
		}
	}
	if err := readAuth(auth, &node.Auth, "node "+id); err != nil {
		return domain.CollectionNode{}, err
	}
	if scripts.Valid {
		var value domain.Scripts
		if err := json.Unmarshal([]byte(scripts.String), &value); err != nil {
			return domain.CollectionNode{}, fmt.Errorf("reading the scripts of node %s: %w", id, err)
		}
		node.Scripts = &value
	}
	return node, nil
}

// readAuth is the auth a row carries. An absent one is not a failure: it is how a level says it has
// none of its own, and the request below it reads that as "take yours".
func readAuth(raw sql.NullString, into **domain.Auth, what string) error {
	if !raw.Valid {
		return nil
	}
	var value domain.Auth
	if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
		return fmt.Errorf("reading the auth of %s: %w", what, err)
	}
	*into = &value
	return nil
}

func (s *Store) nodes(ctx context.Context, workspaceID string) ([]domain.CollectionNode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, collection_id, name, position, method, description, auth_json
		   FROM collection_nodes
		  WHERE deleted_at IS NULL
		    AND collection_id IN (SELECT id FROM collections WHERE workspace_id = ?)
		  ORDER BY collection_id, position, created_at`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing collection nodes: %w", err)
	}
	defer rows.Close()

	out := []domain.CollectionNode{}
	for rows.Next() {
		var (
			node        domain.CollectionNode
			method      sql.NullString
			description sql.NullString
			auth        sql.NullString
		)
		if err := rows.Scan(&node.ID, &node.CollectionID, &node.Name, &node.Position,
			&method, &description, &auth); err != nil {
			return nil, fmt.Errorf("listing collection nodes: %w", err)
		}
		node.Method = method.String
		// The description travels with the row: it is the line the overview draws under the title.
		node.Description = description.String
		// Auth travels with it as well, although no row draws it: inheritance is a property of the
		// tree, and a request has to be able to ask what the levels above it answered.
		if err := readAuth(auth, &node.Auth, "node "+node.ID); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

// ContentRows is the collection page's read: every request inside one collection, the folders
// inside it included, with the address each of them goes to. It is deliberately not the tree's
// query — that one carries what the panel draws and no request payload at all — and it is
// deliberately not one node at a time, which is what an export does, because an export needs the
// bodies.
//
// The walk is the one a run makes: a level's requests and its folders are numbered in one sequence,
// so the order is the tree's own and the rows come out in it. That order is built here as a sort
// key rather than read from a column — each level appends its position to the key of the level
// above — which is what makes the recursive read interleave a folder with the requests beside it
// instead of listing every folder after every request.
func (s *Store) ContentRows(ctx context.Context, collectionID string) ([]domain.LevelRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE level(id, key) AS (
		    SELECT id, printf('%08d', position)
		      FROM collections WHERE id = ? AND deleted_at IS NULL
		    UNION ALL
		    SELECT c.id,
		           level.key || '/' || printf('%08d', c.position)
		      FROM collections c JOIN level ON c.parent_id = level.id
		     WHERE c.deleted_at IS NULL
		)
		SELECT n.id, n.name, n.method, n.url
		  FROM level JOIN collection_nodes n ON n.collection_id = level.id
		 WHERE n.deleted_at IS NULL
		 ORDER BY level.key || '/' || printf('%08d', n.position)`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("listing the requests of %s: %w", collectionID, err)
	}
	defer rows.Close()

	out := []domain.LevelRow{}
	for rows.Next() {
		var (
			row    domain.LevelRow
			method sql.NullString
			url    sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Name, &method, &url); err != nil {
			return nil, fmt.Errorf("listing the requests of %s: %w", collectionID, err)
		}
		row.Method = method.String
		row.URL = url.String
		out = append(out, row)
	}
	return out, rows.Err()
}

// SaveCollection writes parent_id on the insert and leaves it alone on the update, the way a
// node's collection is: a rename saves the row it read, and moving is a gesture of its own.
func (s *Store) SaveCollection(ctx context.Context, workspaceID string, c domain.Collection) error {
	auth, err := encodeAuth(c.Auth)
	if err != nil {
		return fmt.Errorf("saving collection %s: %w", c.ID, err)
	}
	variables, err := encodeVariables(c.Variables)
	if err != nil {
		return fmt.Errorf("saving collection %s: %w", c.ID, err)
	}

	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("saving collection %s", c.ID), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "collections", c.ID)
		if err != nil {
			return fmt.Errorf("saving collection %s: %w", c.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO collections (id, workspace_id, name, description, position, parent_id, auth_json,
			                          variables_json, created_at, updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   name = excluded.name, description = excluded.description,
			   position = excluded.position, auth_json = excluded.auth_json,
			   variables_json = excluded.variables_json,
			   updated_at = excluded.updated_at`,
			c.ID, workspaceID, c.Name, c.Description, c.Position, nullIfEmpty(c.ParentID), auth,
			variables, now, now, versionUnagreed); err != nil {
			return fmt.Errorf("saving collection %s: %w", c.ID, err)
		}
		return enqueue(ctx, tx, workspaceID, entityCollection, c.ID, action)
	})
}

// SaveDocs writes the prose a collection carries for the page it is published as. It is an edit of
// its own rather than a field of SaveCollection, which is handed a row read from the shallow tree
// and would write three empty columns over prose it never read. It is not enqueued for sync.
func (s *Store) SaveDocs(ctx context.Context, id string, docs domain.Docs) error {
	now := time.Now().UnixMilli()
	return s.inTx(ctx, "saving collection docs "+id, func(tx *writeTx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE collections SET docs_authorization = ?, docs_errors = ?, docs_limits = ?,
			        updated_at = ?
			  WHERE id = ? AND deleted_at IS NULL`,
			docs.Authorization, docs.Errors, docs.Limits, now, id); err != nil {
			return fmt.Errorf("saving collection docs %s: %w", id, err)
		}
		return nil
	})
}

// encodeAuth keeps "not set here" apart from "explicitly nothing": the column is NULL for the
// first and a JSON object for the second, which is what makes inheritance expressible.
func encodeAuth(auth *domain.Auth) (sql.NullString, error) {
	if auth == nil {
		return sql.NullString{}, nil
	}
	encoded, err := json.Marshal(auth)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encoding auth: %w", err)
	}
	return sql.NullString{String: string(encoded), Valid: true}, nil
}

// A collection's variables are stored as a blob for the reason auth is: they are read and
// written as a whole set, and nothing queries one of them. NULL is a level that set none.
func decodeVariables(raw sql.NullString, what string) ([]domain.Variable, error) {
	if !raw.Valid {
		return nil, nil
	}
	var value []domain.Variable
	if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
		return nil, fmt.Errorf("reading the variables of %s: %w", what, err)
	}
	return value, nil
}

func encodeVariables(variables []domain.Variable) (sql.NullString, error) {
	if len(variables) == 0 {
		return sql.NullString{}, nil
	}
	encoded, err := json.Marshal(variables)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encoding variables: %w", err)
	}
	return sql.NullString{String: string(encoded), Valid: true}, nil
}

// SaveNode writes one node whole: a node is edited in one place — the card in "Collections" — and
// saved by one gesture.
func (s *Store) SaveNode(ctx context.Context, node domain.CollectionNode) error {
	params, err := json.Marshal(domain.OrEmpty(node.Params))
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	headers, err := json.Marshal(domain.OrEmpty(node.Headers))
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	pathParams, err := json.Marshal(domain.OrEmpty(node.PathParams))
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	auth, err := encodeAuth(node.Auth)
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	scripts, err := encodeScripts(node.Scripts)
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	form, err := json.Marshal(domain.OrEmpty(node.Form))
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}
	responses, err := json.Marshal(domain.OrEmpty(node.Responses))
	if err != nil {
		return fmt.Errorf("saving node %s: %w", node.ID, err)
	}

	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("saving node %s", node.ID), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "collection_nodes", node.ID)
		if err != nil {
			return fmt.Errorf("saving node %s: %w", node.ID, err)
		}
		// A request carries no workspace of its own — it inherits the collection it sits in — so the
		// space the entry is filed under is read from that collection.
		var workspaceID string
		if err := tx.QueryRowContext(ctx,
			`SELECT workspace_id FROM collections WHERE id = ?`, node.CollectionID).
			Scan(&workspaceID); err != nil {
			return fmt.Errorf("reading the workspace of node %s: %w", node.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO collection_nodes (id, collection_id, name, position, description,
			                               auth_json, scripts_json, method, url, params_json,
			                               headers_json, path_params_json, body, body_kind, form_json,
			                               body_file, responses_json, environment_id, created_at,
			                               updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   name = excluded.name, position = excluded.position, description = excluded.description,
			   auth_json = excluded.auth_json, scripts_json = excluded.scripts_json,
			   method = excluded.method, url = excluded.url,
			   params_json = excluded.params_json, headers_json = excluded.headers_json,
			   path_params_json = excluded.path_params_json,
			   body = excluded.body, body_kind = excluded.body_kind, form_json = excluded.form_json,
			   body_file = excluded.body_file, responses_json = excluded.responses_json,
			   environment_id = excluded.environment_id, updated_at = excluded.updated_at`,
			node.ID, node.CollectionID, node.Name, node.Position,
			nullIfEmpty(node.Description), auth, scripts, nullIfEmpty(node.Method), nullIfEmpty(node.URL),
			string(params), string(headers), string(pathParams), node.Body,
			string(domain.KindOf(node.BodyKind)), string(form), node.BodyFile, string(responses),
			nullIfEmpty(node.EnvironmentID), now, now, versionUnagreed); err != nil {
			return fmt.Errorf("saving node %s: %w", node.ID, err)
		}
		return enqueue(ctx, tx, workspaceID, entityRequest, node.ID, action)
	})
}

// The subtree a delete reaches: the collection itself and everything nested in it, however deep.
// A tree may hold a tree of its own, so the walk is the recursive one the content read makes.
const collectionTree = `WITH RECURSIVE tree(id) AS (
	    SELECT id FROM collections WHERE workspace_id = ? AND id = ?
	    UNION ALL
	    SELECT c.id FROM collections c JOIN tree ON c.parent_id = tree.id)`

// DeleteCollection marks a collection and its whole tree gone. The rows a person could look at stay
// where they are: a deletion is a write, because it is one of the things the other side has to be
// told, and a row that was taken out of the table would be a deletion nobody could hear about.
//
// The tree is marked whole rather than left to the cascade that used to do it. The far side applies
// markers one row at a time, and a request still saying it is alive under a collection that is not
// is a request that comes back the next time the space is pulled.
func (s *Store) DeleteCollection(ctx context.Context, workspaceID, id string) error {
	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("deleting collection %s", id), func(tx *writeTx) error {
		// Who goes is read first, because the marker overwrites the very thing that says which rows
		// were alive, and one entry per row is what a delete of a tree owes: the far side applies
		// markers one at a time.
		nodes, err := idsOf(ctx, tx, `SELECT id FROM collection_nodes
			 WHERE deleted_at IS NULL AND collection_id IN (`+collectionTree+` SELECT id FROM tree)
			 ORDER BY id`, workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting collection %s: %w", id, err)
		}
		collections, err := idsOf(ctx, tx, `SELECT id FROM collections
			 WHERE deleted_at IS NULL AND id IN (`+collectionTree+` SELECT id FROM tree)
			 ORDER BY id`, workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting collection %s: %w", id, err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE collection_nodes SET deleted_at = ?
			  WHERE deleted_at IS NULL AND collection_id IN (`+collectionTree+` SELECT id FROM tree)`,
			now, workspaceID, id); err != nil {
			return fmt.Errorf("deleting collection %s: %w", id, err)
		}
		// The runs of those requests go outright, and their results follow through the foreign key. A
		// run is nothing anybody deletes on its own and nothing anybody opens on its own — it is read
		// through the collection that holds it — so a collection that is gone has no runs left to
		// keep, and the other side derives the same from the collection's own marker.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM collection_runs WHERE collection_id IN (`+collectionTree+` SELECT id FROM tree)`,
			workspaceID, id); err != nil {
			return fmt.Errorf("deleting collection %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE collections SET deleted_at = ?
			  WHERE deleted_at IS NULL AND id IN (`+collectionTree+` SELECT id FROM tree)`,
			now, workspaceID, id); err != nil {
			return fmt.Errorf("deleting collection %s: %w", id, err)
		}

		// The levels first and their requests after: a tree says what it could not keep that way,
		// and the entries read in the order the thing fell apart.
		if err := queueAll(ctx, tx, workspaceID, entityCollection, actionDelete,
			collections); err != nil {
			return err
		}
		return queueAll(ctx, tx, workspaceID, entityRequest, actionDelete, nodes)
	})
}

// DeleteNode marks one request gone. Its scripts and its draft stay where they are: they are read
// through the node, and nothing reaches them once the node is not drawn.
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	return s.inTx(ctx, fmt.Sprintf("deleting node %s", id), func(tx *writeTx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE collection_nodes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
			time.Now().UnixMilli(), id)
		if err != nil {
			return fmt.Errorf("deleting node %s: %w", id, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting node %s: %w", id, err)
		}
		if changed == 0 {
			return nil
		}
		var workspaceID string
		if err := tx.QueryRowContext(ctx,
			`SELECT c.workspace_id FROM collection_nodes n JOIN collections c ON c.id = n.collection_id
			  WHERE n.id = ?`, id).Scan(&workspaceID); err != nil {
			return fmt.Errorf("reading the workspace of node %s: %w", id, err)
		}
		return enqueue(ctx, tx, workspaceID, entityRequest, id, actionDelete)
	})
}

func (s *Store) NextPosition(ctx context.Context, workspaceID, collectionID string) (int64, error) {
	var next int64
	err := s.db.QueryRowContext(ctx,
		`SELECT ifnull(max(position), -1) + 1 FROM (
		   SELECT position FROM collection_nodes
		    WHERE collection_id = ? AND deleted_at IS NULL
		    UNION ALL
		   SELECT position FROM collections
		    WHERE workspace_id = ? AND ifnull(parent_id, '') = ? AND deleted_at IS NULL)`,
		collectionID, workspaceID, collectionID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("reading the next position in %s: %w", collectionID, err)
	}
	return next, nil
}
