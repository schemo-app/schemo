package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/sync/v1"

	"schemo/internal/domain"
)

// A row as the contract carries it, and back. This is the one place that knows both the columns and
// the payloads, which is the point of it: a field added to a collection is added here, beside the
// column it is read from, and nothing above this package has an opinion about either.
//
// The payload is bytes to everything else. The engine passes it through without looking, which is
// what keeps a change to a payload out of the engine, the window and every test of them.

// entityFor reads the row a change names as the entity that travels: the envelope out of the
// columns, and the payload out of the row's own shape.
//
// The second answer says whether there is anything to send at all. A row that is gone — pruned, or
// cleared with the history — is not a change anybody has to hear about, and a push that met one
// would be asking the server about a thing it never had.
func entityFor(
	ctx context.Context,
	q querier,
	space domain.SyncSpace,
	change queueRow,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	switch change.Kind {
	case domain.EntityWorkspace:
		return workspaceEntity(ctx, q, space)
	case domain.EntityCollection:
		return collectionEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityRequest:
		return requestEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityEnvironment:
		return environmentEntity(ctx, q, change.EntityID)
	case domain.EntityVariable:
		return variableEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityCookie:
		return cookieEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityJarDomain:
		return jarDomainEntity(ctx, q, change.EntityID)
	case domain.EntityRecord:
		return recordEntity(ctx, q, change.EntityID, scopes)
	case domain.EntityRun:
		return runEntity(ctx, q, change.EntityID, scopes)
	}
	return domain.SyncEntity{}, false, fmt.Errorf("sending a %s is not a thing this device does",
		change.Kind)
}

// absent is a row that is not here, which is an answer and not a failure: an entry of the outbox
// may name a row that was pruned or cleared before it was sent, and a push that met one would be
// asking the server about a thing it never had. Anything else is a real error and says which row.
func absent(err error, what, id string) (domain.SyncEntity, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SyncEntity{}, false, nil
	}
	return domain.SyncEntity{}, false, fmt.Errorf("reading %s %s: %w", what, id, err)
}

// envelope is the part of an entity every kind has: where the row stands on the server, when it
// changed here, and that it is gone.
//
// A tombstone carries no payload: what a deletion says is in the envelope, and a payload beside it
// would be read by a server that had just been told the row is not there.
func envelope(kind domain.EntityType, id, name string, version, position int64, deleted bool,
	createdAt, updatedAt int64, payload []byte) domain.SyncEntity {
	entity := domain.SyncEntity{
		Kind:      kind,
		ID:        id,
		Name:      name,
		Version:   int32(version),
		Deleted:   deleted,
		SortOrder: position,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if !deleted {
		entity.Payload = payload
	}
	return entity
}

// workspaceEntity is a space's own row. Its id on the server is not the id it has here — the server
// made its own when the space was created there, and the link is what remembers it — so what
// travels is the server's id and never the local one.
func workspaceEntity(
	ctx context.Context,
	q querier,
	space domain.SyncSpace,
) (domain.SyncEntity, bool, error) {
	var (
		name, colour         string
		version              int64
		createdAt, updatedAt int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT name, color, version, created_at, updated_at FROM workspaces WHERE id = ?`,
		space.ID).Scan(&name, &colour, &version, &createdAt, &updatedAt)
	if err != nil {
		return absent(err, "workspace", space.ID)
	}

	payload, err := proto.Marshal(sync.WorkspaceData_builder{
		Name: name, Colour: colour,
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding workspace %s: %w", space.ID, err)
	}
	// The space's place among a person's spaces is its own list's business and does not travel, so
	// the envelope's ordering is left empty.
	return envelope(domain.EntityWorkspace, space.RemoteID, name, version, 0, false,
		createdAt, updatedAt, payload), true, nil
}

func collectionEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	var (
		name, description    string
		auth, scripts, vars  sql.NullString
		parentID             sql.NullString
		position, version    int64
		createdAt, updatedAt int64
		deletedAt            sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT name, description, parent_id, auth_json, scripts_json, variables_json, position,
		        version, created_at, updated_at, deleted_at
		   FROM collections WHERE id = ?`, id).
		Scan(&name, &description, &parentID, &auth, &scripts, &vars, &position, &version,
			&createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return absent(err, "collection", id)
	}

	payload, err := proto.Marshal(sync.CollectionData_builder{
		ParentId:    parentID.String,
		Name:        name,
		Description: description,
		Auth:        authData(auth.String, scopes),
		Scripts:     scriptsData(scripts.String),
		Variables:   collectionVariables(vars.String, scopes),
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding collection %s: %w", id, err)
	}
	return envelope(domain.EntityCollection, id, name, version, position, deletedAt.Valid,
		createdAt, updatedAt, payload), true, nil
}

func requestEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	var (
		collectionID  string
		row           requestRow
		createdAt     int64
		updatedAt     int64
		deletedAt     sql.NullInt64
		description   sql.NullString
		auth, scripts sql.NullString
		method, url   sql.NullString
		bodyKind      sql.NullString
		bodyFile      sql.NullString
		envID         sql.NullString
		body          sql.NullString
		params        sql.NullString
		headers       sql.NullString
		pathParams    sql.NullString
		form          sql.NullString
		responses     sql.NullString
	)
	err := q.QueryRowContext(ctx,
		`SELECT collection_id, name, description, auth_json, scripts_json, method, url, params_json,
		        headers_json, path_params_json, body, body_kind, form_json, body_file, environment_id,
		        responses_json, position, version, created_at, updated_at, deleted_at
		   FROM collection_nodes WHERE id = ?`, id).
		Scan(&collectionID, &row.name, &description, &auth, &scripts, &method, &url, &params,
			&headers, &pathParams, &body, &bodyKind, &form, &bodyFile, &envID, &responses,
			&row.position, &row.version, &createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return absent(err, "request", id)
	}

	payload, err := proto.Marshal(sync.RequestData_builder{
		CollectionId:  collectionID,
		Name:          row.name,
		Description:   description.String,
		Method:        method.String,
		Url:           url.String,
		Params:        rowsData(params.String),
		Headers:       rowsData(headers.String),
		PathParams:    rowsData(pathParams.String),
		Body:          body.String,
		BodyKind:      bodyKind.String,
		Form:          formData(form.String),
		BodyFile:      bodyFile.String,
		Auth:          authData(auth.String, scopes),
		Scripts:       scriptsData(scripts.String),
		EnvironmentId: envID.String,
		Responses:     examplesData(responses.String),
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding request %s: %w", id, err)
	}
	return envelope(domain.EntityRequest, id, row.name, row.version, row.position, deletedAt.Valid,
		createdAt, updatedAt, payload), true, nil
}

// requestRow is the two columns of a request that are not its payload: where it stands among its
// siblings, and the version it agreed on.
type requestRow struct {
	name     string
	position int64
	version  int64
}

func environmentEntity(ctx context.Context, q querier, id string) (domain.SyncEntity, bool, error) {
	var (
		name, colour         string
		readonly             int
		position, version    int64
		createdAt, updatedAt int64
		deletedAt            sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT name, ifnull(color, ''), readonly, position, version, created_at, updated_at, deleted_at
		   FROM environments WHERE id = ?`, id).
		Scan(&name, &colour, &readonly, &position, &version, &createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return absent(err, "environment", id)
	}

	payload, err := proto.Marshal(sync.EnvironmentData_builder{
		Name:     name,
		Colour:   colour,
		Readonly: readonly != 0,
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding environment %s: %w", id, err)
	}
	return envelope(domain.EntityEnvironment, id, name, version, position, deletedAt.Valid,
		createdAt, updatedAt, payload), true, nil
}

func variableEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	var (
		scopeKind, name, value string
		kind                   string
		scopeID                sql.NullString
		enabled                int
		position, version      int64
		createdAt, updatedAt   int64
		deletedAt              sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT scope_kind, ifnull(scope_id, ''), name, value, kind, enabled, position, version,
		        created_at, updated_at, deleted_at
		   FROM variables WHERE id = ?`, id).
		Scan(&scopeKind, &scopeID, &name, &value, &kind, &enabled, &position, &version,
			&createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return absent(err, "variable", id)
	}

	held := sync.VariableData_builder{
		Scope:   scopeKind,
		ScopeId: scopeID.String,
		Name:    name,
		Kind:    kind,
		Enabled: enabled != 0,
	}
	// A secret's value leaves only when the person lets it out; the kind below still travels, so the
	// far side knows the variable is one and that it was given no value, and the flag says the
	// absence is a choice: the echo of this push must not take the value away from where it is kept.
	// Who may be given it back is decided there, from that kind — see docs/SYNC.md.
	if kind == string(domain.VariableSecret) && !scopes.Secrets {
		held.ValueWithheld = true
	} else {
		held.Value = value
	}
	payload, err := proto.Marshal(held.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding variable %s: %w", id, err)
	}
	return envelope(domain.EntityVariable, id, name, version, position, deletedAt.Valid,
		createdAt, updatedAt, payload), true, nil
}

func cookieEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	var (
		host, name, value, path, sameSite, origin string
		expiresAt                                 sql.NullInt64
		secure, httpOnly, hostOnly                int
		version, writtenAt                        int64
		deletedAt                                 sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT domain, name, value, path, expires_at, secure, http_only, same_site, host_only,
		        origin, version, written_at, deleted_at
		   FROM jar_cookies WHERE id = ?`, id).
		Scan(&host, &name, &value, &path, &expiresAt, &secure, &httpOnly, &sameSite, &hostOnly,
			&origin, &version, &writtenAt, &deletedAt)
	if err != nil {
		return absent(err, "cookie", id)
	}

	// A cookie's value is a credential as often as not, so it goes under the same switch a secret
	// variable's does.
	cookie := sync.CookieData_builder{
		Domain:    host,
		Name:      name,
		Path:      path,
		ExpiresAt: expiryOf(expiresAt),
		Secure:    secure != 0,
		HttpOnly:  httpOnly != 0,
		SameSite:  sameSite,
		HostOnly:  hostOnly != 0,
		Origin:    origin,
	}
	if scopes.Secrets {
		cookie.Value = value
	} else {
		cookie.ValueWithheld = true
	}
	payload, err := proto.Marshal(cookie.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding cookie %s: %w", id, err)
	}
	// A cookie's clock is `written_at`, and it is the only one the row has: the envelope's two
	// moments are the same one, because a jar entry is never edited into a second moment.
	return envelope(domain.EntityCookie, id, name+" · "+host, version, 0, deletedAt.Valid,
		writtenAt, writtenAt, payload), true, nil
}

func jarDomainEntity(ctx context.Context, q querier, id string) (domain.SyncEntity, bool, error) {
	var (
		host      string
		blocked   int
		version   int64
		writtenAt int64
		deletedAt sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT domain, blocked, version, written_at, deleted_at FROM jar_domains WHERE id = ?`, id).
		Scan(&host, &blocked, &version, &writtenAt, &deletedAt)
	if err != nil {
		return absent(err, "the flag of", id)
	}

	payload, err := proto.Marshal(sync.JarDomainData_builder{
		Domain:  host,
		Blocked: blocked != 0,
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding the flag of %s: %w", id, err)
	}
	return envelope(domain.EntityJarDomain, id, host, version, 0, deletedAt.Valid,
		writtenAt, writtenAt, payload), true, nil
}

// The lists a level carries are stored as JSON, one column each, and travel as repeated fields. The
// shapes are the app's own and the contract's: the two are the same fields under two names, and the
// translation is here rather than in two places.

// authData is an authorization as it leaves. Its secret answers stay home when the person keeps
// secrets there, and are named in `withheld_keys` so that the far side and the echo know the
// absence is a choice.
func authData(raw string, scopes domain.SyncScopes) *sync.AuthData {
	if raw == "" {
		return nil
	}
	var auth domain.Auth
	if err := json.Unmarshal([]byte(raw), &auth); err != nil {
		return nil
	}
	if auth.Type == "" && len(auth.Fields) == 0 {
		return nil
	}

	// Which of the answers are secrets is the client's knowledge and nobody else's: the schemes are
	// this app's table, and the server is told which keys to withhold rather than having to know what
	// a bearer token is.
	secrets := []string{}
	if scheme, ok := domain.SchemeFor(auth.Type); ok {
		for _, field := range scheme.Fields {
			if field.Secret {
				secrets = append(secrets, field.Key)
			}
		}
	}
	fields, withheld := auth.Fields, []string(nil)
	if !scopes.Secrets {
		fields = make(map[string]string, len(auth.Fields))
		for key, value := range auth.Fields {
			if !slices.Contains(secrets, key) {
				fields[key] = value
			} else if value != "" {
				withheld = append(withheld, key)
			}
		}
		slices.Sort(withheld)
	}
	return sync.AuthData_builder{
		Type:         string(auth.Type),
		Fields:       fields,
		SecretKeys:   secrets,
		WithheldKeys: withheld,
	}.Build()
}

func scriptsData(raw string) *sync.ScriptsData {
	if raw == "" {
		return nil
	}
	var scripts domain.Scripts
	if err := json.Unmarshal([]byte(raw), &scripts); err != nil {
		return nil
	}
	if scripts.Pre == "" && scripts.Post == "" && !scripts.PreOff && !scripts.PostOff &&
		len(scripts.Captures) == 0 {
		return nil
	}
	return sync.ScriptsData_builder{
		Pre:      scripts.Pre,
		Post:     scripts.Post,
		PreOff:   scripts.PreOff,
		PostOff:  scripts.PostOff,
		Captures: captureData(scripts.Captures),
	}.Build()
}

func captureData(rules []domain.Capture) []*sync.CaptureData {
	out := make([]*sync.CaptureData, 0, len(rules))
	for _, rule := range rules {
		out = append(out, sync.CaptureData_builder{
			Id: rule.ID, Name: rule.Name, Source: string(rule.Source), Path: rule.Path,
			Scope: string(rule.Scope), Secret: rule.Secret, Off: rule.Off,
		}.Build())
	}
	return out
}

// collectionVariables are the variables a level answers for `{{tokens}}` below it. They travel
// inside the collection rather than as rows of their own — the table keeps them in one JSON column,
// and a level's variables are meaningless apart from it.
//
// A secret's value is held back under the same switch the variables of an environment are: the
// flag says the absence is a choice, so the echo of this push leaves the value where it is kept.
func collectionVariables(raw string, scopes domain.SyncScopes) []*sync.LevelVariable {
	if raw == "" {
		return nil
	}
	var variables []domain.Variable
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil
	}
	out := make([]*sync.LevelVariable, 0, len(variables))
	for _, variable := range variables {
		one := sync.LevelVariable_builder{
			Id:      variable.ID,
			Name:    variable.Name,
			Kind:    string(variable.Kind),
			Enabled: variable.Enabled,
		}
		if variable.Kind == domain.VariableSecret && !scopes.Secrets {
			one.ValueWithheld = true
		} else {
			one.Value = variable.Value
		}
		out = append(out, one.Build())
	}
	return out
}

// examplesData is a request's saved answers as the contract carries them.
func examplesData(raw string) []*sync.ExampleData {
	if raw == "" {
		return nil
	}
	var examples []domain.ResponseExample
	if err := json.Unmarshal([]byte(raw), &examples); err != nil {
		return nil
	}
	out := make([]*sync.ExampleData, 0, len(examples))
	for _, example := range examples {
		headers := make([]*sync.HeaderData, 0, len(example.Headers))
		for _, header := range example.Headers {
			headers = append(headers, sync.HeaderData_builder{
				Name: header.Name, Value: header.Value,
			}.Build())
		}
		out = append(out, sync.ExampleData_builder{
			Name: example.Name, Status: int32(example.Status), Headers: headers, Body: example.Body,
		}.Build())
	}
	return out
}

func rowsData(raw string) []*sync.Row {
	if raw == "" {
		return nil
	}
	var rows []domain.Row
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]*sync.Row, 0, len(rows))
	for _, row := range rows {
		out = append(out, sync.Row_builder{
			Id: row.ID, Name: row.Name, Value: row.Value, Enabled: row.Enabled,
			Description: row.Description,
		}.Build())
	}
	return out
}

func formData(raw string) []*sync.FormRow {
	if raw == "" {
		return nil
	}
	var rows []domain.FormRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]*sync.FormRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, sync.FormRow_builder{
			Id: row.ID, Name: row.Name, Value: row.Value, FilePath: row.Src, Enabled: row.Enabled,
			File: row.File,
		}.Build())
	}
	return out
}

// expiryOf is a moment the jar keeps as a number, as the contract carries it. A session cookie has
// none, which is not the same fact as one that expired in 1970.
func expiryOf(at sql.NullInt64) *timestamppb.Timestamp {
	if !at.Valid {
		return nil
	}
	return timestamppb.New(time.UnixMilli(at.Int64))
}

// recordEntity is one sent request with everything that hangs off it: the bodies, the run that
// produced it and what the code around it reported.
//
// It is one payload because it is one document — a report whose record is missing is a report of
// nothing — and it is the only payload here that is heavy: two bodies at the size of a download.
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
		return envelope(domain.EntityRecord, id, record.Method+" "+record.URL, 0, 0, true,
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
	return envelope(domain.EntityRecord, id, record.Method+" "+record.URL, 0, 0, false,
		record.StartedAt, record.StartedAt, payload), true, nil
}
