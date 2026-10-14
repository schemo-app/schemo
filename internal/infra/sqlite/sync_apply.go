package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/sync/v1"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// ApplyPage applies a page of remote changes and advances the sync cursor in a single transaction.
func (s *Store) ApplyPage(
	ctx context.Context,
	spaceID string,
	changes []domain.SyncChange,
	cursor int64,
) (int, error) {
	skipped := 0

	err := s.inTx(ctx, "applying what the server sent", func(tx *writeTx) error {
		left, err := unsettledRows(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		// A page is in the order the server last wrote the rows, so a child can come before a parent
		// that was edited later and be refused by the foreign key. What fails is tried again after
		// the rest, for as long as a pass writes anything: only a row that nothing makes writable is
		// skipped, and the cursor moves past it.
		pending := changes
		for len(pending) > 0 {
			var refused []domain.SyncChange
			for _, change := range pending {
				if left.holds(spaceID, change.Entity) {
					continue
				}
				if _, err := tx.ExecContext(ctx, `SAVEPOINT row`); err != nil {
					return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
				}
				if err := applyEntity(ctx, tx, spaceID, change.Entity); err != nil {
					if _, err := tx.ExecContext(ctx, `ROLLBACK TO row`); err != nil {
						return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
					}
					refused = append(refused, change)
					continue
				}
				if _, err := tx.ExecContext(ctx, `RELEASE row`); err != nil {
					return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
				}
			}
			if len(refused) == len(pending) {
				skipped = len(refused)
				break
			}
			pending = refused
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_sync_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE
			   SET last_sync_seq = max(workspace_sync.last_sync_seq, excluded.last_sync_seq)`,
			spaceID, cursor); err != nil {
			return fmt.Errorf("moving the cursor of %s: %w", spaceID, err)
		}
		return nil
	})
	return skipped, err
}

// unsettled is the rows of a space that the device holds a change of the server has not settled.
type unsettled map[[2]string]struct{}

// holds reports whether the row a pulled entity is about is one of them. The space's own row is
// queued under its local id, which is the one a page is applied under.
func (u unsettled) holds(spaceID string, entity domain.SyncEntity) bool {
	id := entity.ID
	if entity.Kind == domain.EntityWorkspace {
		id = spaceID
	}
	_, held := u[[2]string{string(entity.Kind), id}]
	return held
}

// unsettledRows reads, once for a page, the rows a pull must leave alone: those whose change was
// queued by an edit. Parked entries, entries made by offering the space again and every entry of a
// space this person may not write in are not held; docs/SYNC.md has the reasons.
func unsettledRows(ctx context.Context, tx *writeTx, spaceID string) (unsettled, error) {
	var access string
	err := tx.QueryRowContext(ctx,
		`SELECT access_json FROM workspaces WHERE id = ?`, spaceID).Scan(&access)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the access to %s: %w", spaceID, err)
	}
	held := unsettled{}
	if err == nil && !accessOf(access).Write {
		return held, nil
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT entity_type, entity_id FROM sync_queue
		  WHERE workspace_id = ? AND status <> 'parked' AND offered = 0
		    AND entity_type NOT IN (?, ?)`, spaceID, entityRecord, entityRun)
	if err != nil {
		return nil, fmt.Errorf("reading what %s still owes: %w", spaceID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, fmt.Errorf("reading what %s still owes: %w", spaceID, err)
		}
		held[[2]string{kind, id}] = struct{}{}
	}
	return held, rows.Err()
}

func tombstone(
	ctx context.Context,
	tx *writeTx,
	table, at string,
	entity domain.SyncEntity,
) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE `+table+` SET version = ?, deleted_at = ?, `+at+` = ? WHERE id = ?`,
		entity.Version, now(), entity.UpdatedAt, entity.ID)
	if err != nil {
		return fmt.Errorf("deleting %s %s: %w", table, entity.ID, err)
	}
	return nil
}

func applyEntity(ctx context.Context, tx *writeTx, spaceID string, entity domain.SyncEntity) error {
	switch entity.Kind {
	case domain.EntityWorkspace:
		return applyWorkspace(ctx, tx, spaceID, entity)
	case domain.EntityCollection:
		return applyCollection(ctx, tx, spaceID, entity)
	case domain.EntityRequest:
		return applyRequest(ctx, tx, spaceID, entity)
	case domain.EntityEnvironment:
		return applyEnvironment(ctx, tx, spaceID, entity)
	case domain.EntityVariable:
		return applyVariable(ctx, tx, spaceID, entity)
	case domain.EntityCookie:
		return applyCookie(ctx, tx, spaceID, entity)
	case domain.EntityJarDomain:
		return applyJarDomain(ctx, tx, spaceID, entity)
	case domain.EntityRecord:
		return nil
	}
	return fmt.Errorf("applying a %s is not a thing this device does", entity.Kind)
}

func applyWorkspace(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return nil
	}

	if len(entity.Payload) == 0 {
		_, err := tx.ExecContext(ctx,
			`UPDATE workspaces SET version = ?, updated_at = ? WHERE id = ?`,
			entity.Version, entity.UpdatedAt, spaceID)
		if err != nil {
			return fmt.Errorf("moving the space to the server's version: %w", err)
		}
		return nil
	}

	var payload sync.WorkspaceData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading the space the server sent: %w", err)
	}

	_, err := tx.ExecContext(ctx,
		`UPDATE workspaces SET name = ?, color = ?, version = ?, updated_at = ? WHERE id = ?`,
		payload.GetName(), payload.GetColour(), entity.Version, entity.UpdatedAt, spaceID)
	if err != nil {
		return fmt.Errorf("writing the space the server sent: %w", err)
	}
	return nil
}

func applyCollection(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "collections", "updated_at", entity)
	}

	var payload sync.CollectionData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading collection %s: %w", entity.ID, err)
	}
	variables, err := keptVariables(ctx, tx, entity.ID, payload.GetVariables())
	if err != nil {
		return err
	}
	auth, err := keptAuth(ctx, tx, "collections", entity.ID, payload.GetAuth())
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO collections (id, workspace_id, name, description, parent_id, auth_json,
		                         scripts_json, variables_json, position, version, created_at,
		                         updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name, description = excluded.description, parent_id = excluded.parent_id,
		  auth_json = excluded.auth_json, scripts_json = excluded.scripts_json,
		  variables_json = excluded.variables_json, position = excluded.position,
		  version = excluded.version, updated_at = excluded.updated_at, deleted_at = NULL`,
		entity.ID, spaceID, payload.GetName(), payload.GetDescription(),
		nullIfEmpty(payload.GetParentId()), auth,
		scriptsJSON(payload.GetScripts()), variables, entity.SortOrder, entity.Version,
		entity.CreatedAt, entity.UpdatedAt)
	if err != nil {
		return fmt.Errorf("writing collection %s: %w", entity.ID, err)
	}
	return nil
}

func applyRequest(ctx context.Context, tx *writeTx, spaceID string, entity domain.SyncEntity) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "collection_nodes", "updated_at", entity)
	}

	var payload sync.RequestData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading request %s: %w", entity.ID, err)
	}
	auth, err := keptAuth(ctx, tx, "collection_nodes", entity.ID, payload.GetAuth())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO collection_nodes (id, collection_id, name, position, description, auth_json,
		                              scripts_json, method, url, params_json, headers_json,
		                              path_params_json, body, body_kind, form_json, body_file,
		                              responses_json, environment_id, version, created_at,
		                              updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  collection_id = excluded.collection_id, name = excluded.name, position = excluded.position,
		  description = excluded.description, auth_json = excluded.auth_json,
		  scripts_json = excluded.scripts_json, method = excluded.method, url = excluded.url,
		  params_json = excluded.params_json, headers_json = excluded.headers_json,
		  path_params_json = excluded.path_params_json,
		  body = excluded.body, body_kind = excluded.body_kind, form_json = excluded.form_json,
		  body_file = excluded.body_file, responses_json = excluded.responses_json,
		  environment_id = excluded.environment_id,
		  version = excluded.version, updated_at = excluded.updated_at, deleted_at = NULL`,
		entity.ID, payload.GetCollectionId(), payload.GetName(), entity.SortOrder,
		nullIfEmpty(payload.GetDescription()), auth,
		scriptsJSON(payload.GetScripts()), nullIfEmpty(payload.GetMethod()),
		nullIfEmpty(payload.GetUrl()), rowsJSON(payload.GetParams()), rowsJSON(payload.GetHeaders()),
		rowsJSON(payload.GetPathParams()),
		nullIfEmpty(payload.GetBody()), nullIfEmpty(payload.GetBodyKind()),
		formJSON(payload.GetForm()), nullIfEmpty(payload.GetBodyFile()),
		examplesJSON(payload.GetResponses()),
		nullIfEmpty(payload.GetEnvironmentId()), entity.Version, entity.CreatedAt, entity.UpdatedAt)
	if err != nil {
		return fmt.Errorf("writing request %s: %w", entity.ID, err)
	}
	return nil
}

func applyEnvironment(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "environments", "updated_at", entity)
	}

	var payload sync.EnvironmentData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading environment %s: %w", entity.ID, err)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO environments (id, workspace_id, name, color, readonly, position, version,
		                          created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name, color = excluded.color, readonly = excluded.readonly,
		  position = excluded.position, version = excluded.version,
		  updated_at = excluded.updated_at, deleted_at = NULL`,
		entity.ID, spaceID, payload.GetName(), nullIfEmpty(payload.GetColour()),
		payload.GetReadonly(), entity.SortOrder, entity.Version, entity.CreatedAt,
		entity.UpdatedAt)
	if err != nil {
		return fmt.Errorf("writing environment %s: %w", entity.ID, err)
	}
	return nil
}

func applyVariable(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "variables", "updated_at", entity)
	}

	var payload sync.VariableData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading variable %s: %w", entity.ID, err)
	}

	value := payload.GetValue()
	if payload.GetValueWithheld() {
		// The row is found by its id first, so a secret renamed on the other device keeps its value
		// here, and by its name for one this device knew under another id.
		held, err := heldValue(ctx, tx, `SELECT value FROM variables
		                                 WHERE id = ? AND deleted_at IS NULL`, entity.ID)
		if err == nil && held == "" {
			held, err = heldValue(ctx, tx, `SELECT value FROM variables
			                                WHERE workspace_id = ? AND scope_kind = ?
			                                  AND ifnull(scope_id, '') = ? AND name = ?
			                                  AND deleted_at IS NULL`,
				spaceID, payload.GetScope(), payload.GetScopeId(), payload.GetName())
		}
		if err != nil {
			return err
		}
		value = held
	}

	if err := makeRoom(ctx, tx, `DELETE FROM variables
	                            WHERE workspace_id = ? AND scope_kind = ? AND ifnull(scope_id, '') = ?
	                              AND name = ? AND id <> ?`,
		spaceID, payload.GetScope(), payload.GetScopeId(), payload.GetName(), entity.ID); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO variables (id, workspace_id, scope_kind, scope_id, name, value, kind, enabled,
		                       position, version, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  scope_kind = excluded.scope_kind, scope_id = excluded.scope_id, name = excluded.name,
		  value = excluded.value,
		  kind = excluded.kind, enabled = excluded.enabled,
		  position = excluded.position, version = excluded.version,
		  updated_at = excluded.updated_at, deleted_at = NULL`,
		entity.ID, spaceID, payload.GetScope(), nullIfEmpty(payload.GetScopeId()),
		payload.GetName(), value, payload.GetKind(), payload.GetEnabled(),
		entity.SortOrder, entity.Version, entity.CreatedAt, entity.UpdatedAt)
	if err != nil {
		return fmt.Errorf("writing variable %s: %w", entity.ID, err)
	}
	return nil
}

func applyCookie(ctx context.Context, tx *writeTx, spaceID string, entity domain.SyncEntity) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "jar_cookies", "written_at", entity)
	}

	var payload sync.CookieData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading cookie %s: %w", entity.ID, err)
	}

	value := payload.GetValue()
	if payload.GetValueWithheld() {
		held, err := heldValue(ctx, tx, `SELECT value FROM jar_cookies
		                                 WHERE workspace_id = ? AND domain = ? AND path = ? AND name = ?
		                                   AND deleted_at IS NULL`,
			spaceID, payload.GetDomain(), payload.GetPath(), payload.GetName())
		if err != nil {
			return err
		}
		value = held
	}

	if err := makeRoom(ctx, tx, `DELETE FROM jar_cookies
	                            WHERE workspace_id = ? AND domain = ? AND path = ? AND name = ?
	                              AND id <> ?`,
		spaceID, payload.GetDomain(), payload.GetPath(), payload.GetName(), entity.ID); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO jar_cookies (id, workspace_id, domain, name, value, path, expires_at, secure,
		                         http_only, same_site, host_only, origin, written_at, version,
		                         deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  domain = excluded.domain, name = excluded.name, value = excluded.value,
		  path = excluded.path, expires_at = excluded.expires_at, secure = excluded.secure,
		  http_only = excluded.http_only, same_site = excluded.same_site,
		  host_only = excluded.host_only, origin = excluded.origin,
		  written_at = excluded.written_at, version = excluded.version, deleted_at = NULL`,
		entity.ID, spaceID, payload.GetDomain(), payload.GetName(), value,
		payload.GetPath(), expiryMillis(payload.GetExpiresAt()), payload.GetSecure(),
		payload.GetHttpOnly(), payload.GetSameSite(), payload.GetHostOnly(),
		payload.GetOrigin(), entity.UpdatedAt, entity.Version)
	if err != nil {
		return fmt.Errorf("writing cookie %s: %w", entity.ID, err)
	}
	return nil
}

func applyJarDomain(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return tombstone(ctx, tx, "jar_domains", "written_at", entity)
	}

	var payload sync.JarDomainData
	if err := proto.Unmarshal(entity.Payload, &payload); err != nil {
		return fmt.Errorf("reading the flag of %s: %w", entity.ID, err)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO jar_domains (id, workspace_id, domain, blocked, written_at, version, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(id) DO UPDATE SET
		  domain = excluded.domain, blocked = excluded.blocked, written_at = excluded.written_at,
		  version = excluded.version, deleted_at = NULL`,
		entity.ID, spaceID, payload.GetDomain(), payload.GetBlocked(), entity.UpdatedAt,
		entity.Version)
	if err != nil {
		return fmt.Errorf("writing the flag of %s: %w", entity.ID, err)
	}
	return nil
}

func heldValue(ctx context.Context, tx *writeTx, query string, args ...any) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, query, args...).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the value this device holds: %w", err)
	}
	return value, nil
}

func makeRoom(ctx context.Context, tx *writeTx, query string, args ...any) error {
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("making room for a row the server sent: %w", err)
	}
	return nil
}

func keptAuth(
	ctx context.Context,
	tx *writeTx,
	table, id string,
	auth *sync.AuthData,
) (any, error) {
	withheld := auth.GetWithheldKeys()
	if len(withheld) == 0 {
		return authJSON(auth), nil
	}

	var stored []byte
	err := tx.QueryRowContext(ctx, `SELECT auth_json FROM `+table+` WHERE id = ?`, id).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the auth of %s: %w", id, err)
	}

	answers := auth.GetFields()
	fields := make(map[string]string, len(answers)+len(withheld))
	for key, value := range answers {
		fields[key] = value
	}
	if len(stored) > 0 {
		var held domain.Auth
		if err := json.Unmarshal(stored, &held); err == nil {
			for _, key := range withheld {
				if _, told := fields[key]; !told {
					fields[key] = held.Fields[key]
				}
			}
		}
	}
	return authJSON(sync.AuthData_builder{
		Type:       auth.GetType(),
		Fields:     fields,
		SecretKeys: auth.GetSecretKeys(),
	}.Build()), nil
}

func authJSON(auth *sync.AuthData) any {
	if auth == nil {
		return nil
	}
	encoded, err := json.Marshal(domain.Auth{
		Type:   domain.AuthType(auth.GetType()),
		Fields: auth.GetFields(),
	})
	if err != nil {
		return nil
	}
	return string(encoded)
}

func scriptsJSON(scripts *sync.ScriptsData) any {
	if scripts == nil {
		return nil
	}
	encoded, err := json.Marshal(domain.Scripts{
		Pre:      scripts.GetPre(),
		Post:     scripts.GetPost(),
		PreOff:   scripts.GetPreOff(),
		PostOff:  scripts.GetPostOff(),
		Captures: capturesOf(scripts.GetCaptures()),
	})
	if err != nil {
		return nil
	}
	return string(encoded)
}

func capturesOf(rules []*sync.CaptureData) []domain.Capture {
	var out []domain.Capture
	for _, rule := range rules {
		out = append(out, domain.Capture{
			ID: rule.GetId(), Name: rule.GetName(), Source: domain.CaptureSource(rule.GetSource()),
			Path: rule.GetPath(), Scope: domain.VarScope(rule.GetScope()), Secret: rule.GetSecret(),
			Off: rule.GetOff(),
		})
	}
	return out
}

func keptVariables(
	ctx context.Context,
	tx *writeTx,
	collectionID string,
	variables []*sync.LevelVariable,
) (any, error) {
	hide := false
	for _, one := range variables {
		hide = hide || one.GetValueWithheld()
	}
	if !hide {
		return variablesJSON(variables)
	}

	var stored []byte
	err := tx.QueryRowContext(ctx, `SELECT variables_json FROM collections WHERE id = ?`,
		collectionID).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the variables of %s: %w", collectionID, err)
	}

	byID, byName := map[string]string{}, map[string]string{}
	if len(stored) > 0 {
		var rows []domain.Variable
		if err := json.Unmarshal(stored, &rows); err == nil {
			for _, row := range rows {
				byID[row.ID], byName[row.Name] = row.Value, row.Value
			}
		}
	}

	kept := make([]*sync.LevelVariable, 0, len(variables))
	for _, one := range variables {
		if !one.GetValueWithheld() {
			kept = append(kept, one)
			continue
		}
		// The value is where this device keeps it: found by the variable's id, and by its name where
		// the device that wrote it has been renamed or has never had the id.
		value, ok := byID[one.GetId()]
		if !ok {
			value = byName[one.GetName()]
		}
		kept = append(kept, sync.LevelVariable_builder{
			Id: one.GetId(), Name: one.GetName(), Value: value, Kind: one.GetKind(),
			Enabled: one.GetEnabled(),
		}.Build())
	}
	return variablesJSON(kept)
}

func variablesJSON(variables []*sync.LevelVariable) (any, error) {
	if len(variables) == 0 {
		return nil, nil
	}
	out := make([]domain.Variable, 0, len(variables))
	for at, variable := range variables {
		id := variable.GetId()
		if id == "" {
			id = newID()
		}
		out = append(out, domain.Variable{
			ID:       id,
			Name:     variable.GetName(),
			Value:    variable.GetValue(),
			Kind:     domain.VariableKind(variable.GetKind()),
			Enabled:  variable.GetEnabled(),
			Position: at,
			HasValue: variable.GetValue() != "",
		})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encoding the variables of a collection: %w", err)
	}
	return string(encoded), nil
}

func rowsJSON(rows []*sync.Row) any {
	if len(rows) == 0 {
		return nil
	}
	out := make([]domain.Row, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Row{
			ID: row.GetId(), Name: row.GetName(), Value: row.GetValue(), Enabled: row.GetEnabled(),
			Description: row.GetDescription(),
		})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func examplesJSON(examples []*sync.ExampleData) any {
	if len(examples) == 0 {
		return nil
	}
	out := make([]domain.ResponseExample, 0, len(examples))
	for _, example := range examples {
		headers := make([]domain.HeaderPair, 0, len(example.GetHeaders()))
		for _, header := range example.GetHeaders() {
			headers = append(headers, domain.HeaderPair{Name: header.GetName(), Value: header.GetValue()})
		}
		out = append(out, domain.ResponseExample{
			Name: example.GetName(), Status: int(example.GetStatus()), Body: example.GetBody(),
			Headers: headers,
		})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func formJSON(rows []*sync.FormRow) any {
	if len(rows) == 0 {
		return nil
	}
	out := make([]domain.FormRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.FormRow{
			ID: row.GetId(), Name: row.GetName(), Value: row.GetValue(), Src: row.GetFilePath(),
			File: row.GetFile(), Enabled: row.GetEnabled(),
		})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func expiryMillis(at *timestamppb.Timestamp) any {
	if at == nil || !at.IsValid() {
		return nil
	}
	return at.AsTime().UnixMilli()
}

func now() int64 { return time.Now().UnixMilli() }

var newID = platform.NewIDGen()

// ApplyHistoryPage applies a page of remote execution history entries and moves the history cursor.
func (s *Store) ApplyHistoryPage(
	ctx context.Context,
	spaceID string,
	changes []domain.RecordChange,
	cursor int64,
) (int, error) {
	skipped := 0

	err := s.inTx(ctx, "applying the history the server sent", func(tx *writeTx) error {
		for _, change := range changes {
			if _, err := tx.ExecContext(ctx, `SAVEPOINT row`); err != nil {
				return fmt.Errorf("applying record %s: %w", change.RecordID, err)
			}
			if err := applyRecord(ctx, tx, spaceID, change); err != nil {
				if _, err := tx.ExecContext(ctx, `ROLLBACK TO row`); err != nil {
					return fmt.Errorf("applying record %s: %w", change.RecordID, err)
				}
				skipped++
				continue
			}
			if _, err := tx.ExecContext(ctx, `RELEASE row`); err != nil {
				return fmt.Errorf("applying record %s: %w", change.RecordID, err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_history_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE
			   SET last_history_seq = max(workspace_sync.last_history_seq, excluded.last_history_seq)`,
			spaceID, cursor); err != nil {
			return fmt.Errorf("moving the history cursor of %s: %w", spaceID, err)
		}
		return nil
	})
	return skipped, err
}

func applyRecord(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	change domain.RecordChange,
) error {
	switch {
	case change.Run != nil:
		// A run is an entry of the log with nothing beside it: it is written once by its id, and the
		// records that name it arrive under their own numbers. Nothing marks it as owed — a run this
		// device was given is one the server already holds.
		return writeRun(ctx, tx, *change.Run)
	case change.Deleted:
		_, err := deleteRecord(ctx, tx, change.RecordID)
		return err
	default:
		return writeRecord(ctx, tx, spaceID, change.Record, change.Seq, nil)
	}
}

// deleteRecord marks a record gone and clears what hangs off it. The bodies and the reports are the
// record's own, and a request that no longer exists is not a request whose body should stay here:
// the lines and the assertions go with their report. The run stays — it is a document of its own,
// and the other records of it are still there.
func deleteRecord(ctx context.Context, tx *writeTx, id string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE records SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now(), id)
	if err != nil {
		return false, fmt.Errorf("taking back record %s: %w", id, err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("taking back record %s: %w", id, err)
	}
	if changed == 0 {
		return false, nil
	}
	for _, orphan := range []string{
		`DELETE FROM record_bodies WHERE record_id = ?`,
		`DELETE FROM script_runs WHERE record_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, orphan, id); err != nil {
			return false, fmt.Errorf("clearing what belongs to record %s: %w", id, err)
		}
	}
	return true, nil
}

func writeRecord(
	ctx context.Context,
	tx *writeTx,
	spaceID string,
	record domain.Record,
	seq int64,
	cachedAt *int64,
) error {
	headers, err := json.Marshal(domain.OrEmpty(record.RequestHeaders))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	back, err := json.Marshal(domain.OrEmpty(record.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	writes, err := cookieWritesJSON(ctx, tx, record.ID, record.CookieWrites)
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}

	// A write through here says the server holds the record: the cache mark comes off a row the log or
	// a snapshot has named, and the server's cookie writes are taken over the local ones — unless this
	// device still owes it a change, because an undo is its own act and has to outlive the echo.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO records (id, workspace_id, source, method, url, status, status_text,
		                      content_type, error, cancelled, duration_us, dns_us, connect_us,
		                      tls_us, wait_us, download_us, request_bytes, response_bytes,
		                      request_headers_json, response_headers_json, cookie_writes_json,
		                      started_at, finished_at, node_id, run_id, agent, tab_id, tab_title,
		                      tab_url, favicon_url, updated_at, remote_seq, cached_at, is_remote)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         ?, ?, ?, ?, ?, ?, 1)
		 ON CONFLICT(id) DO UPDATE SET
		     remote_seq = max(records.remote_seq, excluded.remote_seq),
		     cookie_writes_json = CASE
		         WHEN records.is_remote = 1 AND excluded.remote_seq > records.remote_seq
		              AND NOT EXISTS (SELECT 1 FROM sync_queue
		                              WHERE workspace_id = records.workspace_id
		                                AND entity_type = ? AND entity_id = records.id)
		         THEN excluded.cookie_writes_json ELSE records.cookie_writes_json END,
		     cached_at = CASE WHEN excluded.cached_at IS NULL THEN NULL ELSE records.cached_at END`,
		record.ID, spaceID, string(record.Source), record.Method, record.URL, record.Status,
		record.StatusText, record.ContentType, record.Error, record.Cancelled,
		record.DurationUs, record.DNSUs, record.ConnectUs, record.TLSUs, record.WaitUs,
		record.DownloadUs, record.RequestBytes, record.ResponseBytes,
		string(headers), string(back), writes, record.StartedAt, finishedAt(record),
		record.NodeID, nullIfEmpty(record.RunID), record.Agent, nullIfZero(record.TabID),
		nullIfEmpty(record.TabTitle), nullIfEmpty(record.TabURL), nullIfEmpty(record.FavIconURL),
		now(), seq, cachedAt, string(entityRecord))
	if err != nil {
		return fmt.Errorf("applying record %s: %w", record.ID, err)
	}
	return nil
}

// cookieWritesJSON is what the record's row keeps of the mutation its answer made. A value the
// server did not hand over is one it does not have or one this reader may not see, and either way
// the copy this device holds is the better one: it is what the mutation really was.
func cookieWritesJSON(
	ctx context.Context,
	tx *writeTx,
	recordID string,
	writes []domain.CookieWrite,
) (string, error) {
	held, err := localWrites(ctx, tx, recordID, writes)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(domain.OrEmpty(held))
	if err != nil {
		return "", fmt.Errorf("encoding the cookie writes: %w", err)
	}
	return string(encoded), nil
}

// localWrites answers what the arriving record left out. Nothing is read unless a value arrived
// flagged, which is what keeps this free for every record this device made itself.
func localWrites(
	ctx context.Context,
	tx *writeTx,
	recordID string,
	writes []domain.CookieWrite,
) ([]domain.CookieWrite, error) {
	wanted := false
	for _, write := range writes {
		wanted = wanted || write.Withheld || write.PreviousWithheld
	}
	if !wanted {
		return writes, nil
	}

	var stored []byte
	err := tx.QueryRowContext(ctx, `SELECT cookie_writes_json FROM records WHERE id = ?`,
		recordID).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the cookie writes of %s: %w", recordID, err)
	}
	// A cookie is found by what names it — the name, the domain and the path — and the jar holds no
	// two alike.
	byCookie := map[string]domain.CookieWrite{}
	if len(stored) > 0 {
		var rows []domain.CookieWrite
		if err := json.Unmarshal(stored, &rows); err == nil {
			for _, row := range rows {
				byCookie[cookieKey(row)] = row
			}
		}
	}

	kept := make([]domain.CookieWrite, 0, len(writes))
	for _, write := range writes {
		local, found := byCookie[cookieKey(write)]
		if !found {
			kept = append(kept, write)
			continue
		}
		// A device told only what the cookie became, or only what it was, cannot say the other: the
		// side that came flagged comes back whole, and stays flagged where the copy here is missing
		// it too.
		if write.Withheld {
			write.Value, write.Withheld = local.Value, local.Withheld
		}
		if write.PreviousWithheld {
			write.Previous, write.PreviousWithheld = local.Previous, local.PreviousWithheld
		}
		kept = append(kept, write)
	}
	return kept, nil
}

// cookieKey is what tells two cookies apart, and it is the tuple the jar itself keys them by. NUL
// sits in none of the three, so no two cookies can be spelt into one key.
func cookieKey(write domain.CookieWrite) string {
	return write.Name + "\x00" + write.Domain + "\x00" + write.Path
}

func finishedAt(record domain.Record) int64 {
	if record.DurationUs <= 0 {
		return record.StartedAt
	}
	return record.StartedAt + record.DurationUs/1000
}

// ApplyHistorySnapshot writes a full snapshot of remote execution history within a range.
func (s *Store) ApplyHistorySnapshot(
	ctx context.Context,
	spaceID string,
	records []domain.Record,
	from, head int64,
) error {
	return s.inTx(ctx, "applying a snapshot of the history the server holds", func(tx *writeTx) error {
		bound, held := from, make([]any, 0, len(records)+3)
		for _, record := range records {
			if err := writeRecord(ctx, tx, spaceID, record, 0, nil); err != nil {
				return err
			}
			held = append(held, record.ID)
			if bound == 0 || record.StartedAt < bound {
				bound = record.StartedAt
			}
		}

		query := `UPDATE records SET deleted_at = ?
		           WHERE workspace_id = ? AND is_remote = 1 AND cached_at IS NULL
		             AND deleted_at IS NULL AND started_at >= ?`
		args := []any{now(), spaceID, bound}
		if len(held) > 0 {
			query += ` AND id NOT IN (` + placeholders(len(held)) + `)`
			args = append(args, held...)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("dropping what the server no longer holds: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_history_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE SET last_history_seq = excluded.last_history_seq`,
			spaceID, head); err != nil {
			return fmt.Errorf("moving the history cursor of %s: %w", spaceID, err)
		}
		return nil
	})
}
