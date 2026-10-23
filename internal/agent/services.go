package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// maxAgentBody bounds a response body handed to an agent. A body is read to be understood, and a
// hundred megabytes of it is neither; what was cut is said rather than silently dropped.
const maxAgentBody = 256 << 10

// appTools is what the MCP server sees of the app: the tree, the draft's masking and the history
// joined the way this layer joins features everywhere. It reads workspaces the window is not showing,
// so every read is addressed by a workspace rather than the scope on screen, and it masks what it hands
// over with the same machinery that keeps a secret out of the history: the marker is the app's one
// `••••`.
type appTools struct {
	workspaces   *workspace.Workspaces
	collections  *workspace.Collections
	runs         *request.Runs
	history      *request.History
	environments *workspace.Environments
}

var _ Tools = appTools{}

func NewTools(
	workspaces *workspace.Workspaces,
	collections *workspace.Collections,
	runs *request.Runs,
	history *request.History,
	environments *workspace.Environments,
) Tools {
	return appTools{
		workspaces: workspaces, collections: collections, runs: runs, history: history, environments: environments,
	}
}

func (t appTools) Workspaces(ctx context.Context) ([]Workspace, error) {
	state, err := t.workspaces.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(state.Workspaces))
	for _, workspace := range state.Workspaces {
		out = append(out, Workspace{ID: workspace.ID, Name: workspace.Name, Kind: string(workspace.Kind)})
	}
	return out, nil
}

func (t appTools) Collections(ctx context.Context, workspaceID string) ([]Collection, error) {
	tree, err := t.collections.TreeOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := []Collection{}
	var walk func(rows []domain.Collection)
	walk = func(rows []domain.Collection) {
		for _, row := range rows {
			out = append(out, Collection{
				ID:          row.ID,
				Workspace:   workspaceID,
				Name:        row.Name,
				Description: row.Description,
				Parent:      row.ParentID,
				Requests:    len(row.Items),
				Auth:        row.Auth,
			})
			walk(row.Children)
		}
	}
	walk(tree)
	texts := make([]string, len(out))
	for i := range out {
		texts[i] = out[i].Description
	}
	masked, err := t.environments.MaskTextsIn(ctx, workspaceID, nil, texts, "")
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Description = masked[i]
		out[i].Auth, err = t.agentAuth(ctx, workspaceID, out[i].Auth, "")
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (t appTools) Requests(ctx context.Context, collectionID string) ([]Request, error) {
	workspace, err := t.collections.WorkspaceOf(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	rows, err := t.collections.Contents(ctx, collectionID)
	if err != nil {
		return nil, err
	}

	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.URL
	}
	masked, err := t.environments.MaskTextsIn(ctx, workspace, nil, texts, "")
	if err != nil {
		return nil, err
	}

	out := make([]Request, 0, len(rows))
	for i, row := range rows {
		out = append(out, Request{
			ID:         row.ID,
			Workspace:  workspace,
			Collection: collectionID,
			Name:       row.Name,
			Method:     row.Method,
			URL:        masked[i],
		})
	}
	return out, nil
}

func (t appTools) Request(ctx context.Context, id string) (Request, error) {
	node, err := t.collections.Node(ctx, id)
	if err != nil {
		return Request{}, err
	}
	workspace, err := t.collections.WorkspaceOf(ctx, node.CollectionID)
	if err != nil {
		return Request{}, err
	}

	// Stored fields are readable; only values protected as secret variables are masked.
	rows := enabledHeaders(node.Headers)
	params := enabledHeaders(node.Params)
	texts := make([]string, 0, 2+2*len(rows))
	texts = append(texts, node.URL, node.Body)
	for _, row := range rows {
		texts = append(texts, row.Name, row.Value)
	}
	texts = append(texts, node.Description)
	for _, row := range params {
		texts = append(texts, row.Name, row.Value)
	}
	masked, err := t.environments.MaskTextsIn(ctx, workspace, nil, texts, node.EnvironmentID)
	if err != nil {
		return Request{}, err
	}

	out := Request{
		ID:          node.ID,
		Workspace:   workspace,
		Collection:  node.CollectionID,
		Name:        node.Name,
		Method:      node.Method,
		URL:         masked[0],
		Body:        masked[1],
		BodyKind:    string(node.BodyKind),
		Description: masked[2+2*len(rows)],
		Environment: node.EnvironmentID,
	}
	out.Auth, err = t.agentAuth(ctx, workspace, node.Auth, node.EnvironmentID)
	if err != nil {
		return Request{}, err
	}
	for i := range rows {
		out.Headers = append(out.Headers, Header{
			Name:  masked[2+2*i],
			Value: masked[2+2*i+1],
		})
	}
	for i := range params {
		out.Params = append(out.Params, Header{Name: masked[3+2*len(rows)+2*i], Value: masked[4+2*len(rows)+2*i]})
	}
	return out, nil
}

func (t appTools) History(
	ctx context.Context,
	workspaceID string,
	limit int,
) ([]HistoryRow, error) {
	history, err := t.history.WorkspaceRecords(ctx, workspaceID, limit)
	if err != nil {
		return nil, err
	}

	// A row of history is a text a value can sit in like any other: a captured address carries what
	// the page had in its query, and a failure quotes back the address that failed.
	texts := make([]string, 0, 2*len(history))
	for _, rec := range history {
		texts = append(texts, rec.URL, rec.Error)
	}
	masked, err := t.environments.MaskTextsIn(ctx, workspaceID, nil, texts, "")
	if err != nil {
		return nil, err
	}

	out := make([]HistoryRow, 0, len(history))
	for i, rec := range history {
		out = append(out, HistoryRow{
			ID:         rec.ID,
			Workspace:  workspaceID,
			Method:     rec.Method,
			URL:        masked[2*i],
			Status:     rec.Status,
			Error:      masked[2*i+1],
			DurationMs: rec.DurationUs / 1000,
			StartedAt:  rec.StartedAt,
		})
	}
	return out, nil
}

func (t appTools) Response(ctx context.Context, recordID string) (Response, error) {
	rec, err := t.history.Record(ctx, recordID)
	if err != nil {
		return Response{}, err
	}
	return t.responseOf(ctx, rec)
}

func (t appTools) Send(ctx context.Context, requestID, agent string) (Response, error) {
	node, err := t.collections.Node(ctx, requestID)
	if err != nil {
		return Response{}, err
	}
	workspace, err := t.collections.WorkspaceOf(ctx, node.CollectionID)
	if err != nil {
		return Response{}, err
	}
	// The send path is the app's own, the one a run and the window's own card both take: inherited
	// authorization and variables resolved, the pinned environment honored, and every secret left
	// as its mask in what is written down. The agent's name rides along so the history says whose
	// send it was.
	rec, err := t.runs.Send(ctx, workspace, requestID, agent)
	if err != nil {
		return Response{}, err
	}
	return t.responseOf(ctx, rec)
}

// responseOf bounds the response and masks protected variable values in headers, body and errors.
func (t appTools) responseOf(ctx context.Context, rec domain.Record) (Response, error) {
	body, err := t.history.Body(ctx, rec.ID, domain.SideResponse)
	if err != nil {
		return Response{}, err
	}

	out := Response{
		Record:     rec.ID,
		Status:     rec.Status,
		StatusText: rec.StatusText,
		DurationMs: rec.DurationUs / 1000,
		Workspace:  rec.WorkspaceID,
	}

	texts := make([]string, 0, 2+2*len(rec.ResponseHeaders))
	texts = append(texts, body, rec.Error)
	for _, header := range rec.ResponseHeaders {
		texts = append(texts, header.Name, header.Value)
	}
	masked, err := t.environments.MaskTextsIn(ctx, rec.WorkspaceID, nil, texts, "")
	if err != nil {
		return Response{}, err
	}

	out.Body, out.Error = masked[0], masked[1]
	// Redact before truncating so a secret crossing the cut cannot leak its prefix.
	if len(out.Body) > maxAgentBody {
		out.Body = strings.ToValidUTF8(out.Body[:maxAgentBody], "")
		out.Truncated = true
	}
	for i := range rec.ResponseHeaders {
		out.Headers = append(out.Headers, Header{Name: masked[2+2*i], Value: masked[3+2*i]})
	}
	return out, nil
}

func (t appTools) agentAuth(ctx context.Context, workspace string, auth *domain.Auth, environmentID string) (*domain.Auth, error) {
	if auth == nil {
		return nil, nil
	}
	fields := maps.Clone(auth.Fields)
	keys := slices.Sorted(maps.Keys(fields))
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = fields[key]
	}
	masked, err := t.environments.MaskTextsIn(ctx, workspace, nil, values, environmentID)
	if err != nil {
		return nil, err
	}
	for i, key := range keys {
		fields[key] = masked[i]
	}
	return &domain.Auth{Type: auth.Type, Fields: fields}, nil
}

// enabledHeaders are the rows a request would actually carry: a row switched off is not part of the
// request, and handing it to an agent would be handing over a value that goes nowhere.
func enabledHeaders(rows []domain.Row) []domain.Row {
	out := []domain.Row{}
	for _, row := range rows {
		if row.Enabled && strings.TrimSpace(row.Name) != "" {
			out = append(out, row)
		}
	}
	return out
}

func (t appTools) Environments(ctx context.Context, in WorkspaceInput) (domain.EnvState, error) {
	return t.environments.InWorkspace(in.Workspace).State(ctx)
}

func (t appTools) collectionIn(ctx context.Context, in IDInput) (Collection, error) {
	rows, err := t.Collections(ctx, in.Workspace)
	if err != nil {
		return Collection{}, err
	}
	for _, row := range rows {
		if row.ID == in.ID {
			return row, nil
		}
	}
	return Collection{}, domain.ErrNotFound
}

func (t appTools) requestIn(ctx context.Context, in IDInput) (domain.CollectionNode, error) {
	node, err := t.collections.Node(ctx, in.ID)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	workspace, err := t.collections.WorkspaceOf(ctx, node.CollectionID)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	if workspace != in.Workspace {
		return domain.CollectionNode{}, domain.ErrNotFound
	}
	return node, nil
}

func (t appTools) GetCollection(ctx context.Context, in IDInput) (CollectionDetails, error) {
	one, err := t.collectionIn(ctx, in)
	if err != nil {
		return CollectionDetails{}, err
	}
	requests, err := t.Requests(ctx, in.ID)
	return CollectionDetails{Collection: one, Requests: requests}, err
}

func (t appTools) CreateCollection(ctx context.Context, in CollectionInput) (IDOutput, error) {
	one, err := t.collections.InWorkspace(in.Workspace).CreateCollection(ctx, in.Name, in.Description, in.Parent)
	return IDOutput{ID: one.ID}, err
}

func (t appTools) UpdateCollection(ctx context.Context, in CollectionPatch) (IDOutput, error) {
	if _, err := t.collectionIn(ctx, in.IDInput); err != nil {
		return IDOutput{}, err
	}
	if err := validAgentAuth(in.Auth); err != nil {
		return IDOutput{}, err
	}
	err := t.collections.InWorkspace(in.Workspace).Update(ctx, in.ID, workspace.CollectionPatch{Name: in.Name, Description: in.Description, Auth: in.Auth})
	return IDOutput{ID: in.ID}, err
}

func (t appTools) DeleteCollection(ctx context.Context, in IDInput) (IDOutput, error) {
	if _, err := t.collectionIn(ctx, in); err != nil {
		return IDOutput{}, err
	}
	err := t.collections.InWorkspace(in.Workspace).Delete(ctx, in.ID)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) MoveCollection(ctx context.Context, in MoveInput) (IDOutput, error) {
	if _, err := t.collectionIn(ctx, in.IDInput); err != nil {
		return IDOutput{}, err
	}
	if in.Position < 0 {
		return IDOutput{}, errors.New("position must not be negative")
	}
	err := t.collections.InWorkspace(in.Workspace).Move(ctx, in.ID, in.Parent, in.Position)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) CreateRequest(ctx context.Context, in RequestInput) (IDOutput, error) {
	if _, err := t.collectionIn(ctx, IDInput{WorkspaceInput: in.WorkspaceInput, ID: in.Collection}); err != nil {
		return IDOutput{}, err
	}
	node := domain.CollectionNode{}
	if err := t.patchRequest(ctx, in.Workspace, &node, in.RequestFields); err != nil {
		return IDOutput{}, err
	}
	one, err := t.collections.InWorkspace(in.Workspace).CreateNode(ctx, workspace.RequestDraft{
		CollectionID: in.Collection, Name: node.Name, Method: node.Method, URL: node.URL,
		Description: node.Description, Headers: node.Headers, Params: node.Params,
		Body: node.Body, BodyKind: node.BodyKind, EnvironmentID: node.EnvironmentID, Auth: node.Auth,
	})
	return IDOutput{ID: one.ID}, err
}

func (t appTools) UpdateRequest(ctx context.Context, in RequestPatch) (IDOutput, error) {
	node, err := t.requestIn(ctx, in.IDInput)
	if err != nil {
		return IDOutput{}, err
	}
	if err := t.patchRequest(ctx, in.Workspace, &node, in.RequestFields); err != nil {
		return IDOutput{}, err
	}
	err = t.collections.InWorkspace(in.Workspace).SaveNode(ctx, node)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) DeleteRequest(ctx context.Context, in IDInput) (IDOutput, error) {
	if _, err := t.requestIn(ctx, in); err != nil {
		return IDOutput{}, err
	}
	err := t.collections.InWorkspace(in.Workspace).Delete(ctx, in.ID)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) MoveRequest(ctx context.Context, in MoveInput) (IDOutput, error) {
	if _, err := t.requestIn(ctx, in.IDInput); err != nil {
		return IDOutput{}, err
	}
	if in.Position < 0 {
		return IDOutput{}, errors.New("position must not be negative")
	}
	err := t.collections.InWorkspace(in.Workspace).Move(ctx, in.ID, in.Parent, in.Position)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) CreateEnvironment(ctx context.Context, in EnvironmentInput) (IDOutput, error) {
	id, err := t.environments.InWorkspace(in.Workspace).Create(ctx, workspace.EnvironmentInput{Name: in.Name, Color: in.Color})
	return IDOutput{ID: id}, err
}

func (t appTools) UpdateEnvironment(ctx context.Context, in EnvironmentPatch) (IDOutput, error) {
	err := t.editEnvironment(ctx, in.Workspace, in.ID, func(env *domain.Environment) {
		if in.Name != nil {
			env.Name = *in.Name
		}
		if in.Color != nil {
			env.Color = *in.Color
		}
	})
	return IDOutput{ID: in.ID}, err
}

func (t appTools) DeleteEnvironment(ctx context.Context, in IDInput) (IDOutput, error) {
	err := t.environments.InWorkspace(in.Workspace).Delete(ctx, in.ID)
	return IDOutput{ID: in.ID}, err
}

func (t appTools) SetVariable(ctx context.Context, in VariableInput) (IDOutput, error) {
	if strings.Contains(in.Value, domain.SecretMask) {
		return IDOutput{}, errors.New("a masked value cannot replace a variable")
	}
	id, err := t.environments.InWorkspace(in.Workspace).SetInScope(ctx, domain.EnvScope{Environment: in.Environment}, in.Name, in.Value, in.Secret)
	return IDOutput{ID: id}, err
}

func (t appTools) DeleteVariable(ctx context.Context, in VariableIDInput) (IDOutput, error) {
	environments := t.environments.InWorkspace(in.Workspace)
	if in.Environment == "" {
		state, err := environments.State(ctx)
		if err != nil {
			return IDOutput{}, err
		}
		kept, found := withoutVariable(state.Globals, in.ID)
		if !found {
			return IDOutput{}, domain.ErrNotFound
		}
		return IDOutput{ID: in.ID}, environments.SaveGlobals(ctx, kept)
	}
	found := false
	err := t.editEnvironment(ctx, in.Workspace, in.Environment, func(env *domain.Environment) {
		env.Vars, found = withoutVariable(env.Vars, in.ID)
	})
	if err == nil && !found {
		err = domain.ErrNotFound
	}
	return IDOutput{ID: in.ID}, err
}

// editEnvironment reads one environment of a workspace, lets edit change it and saves it whole. Secret
// values stay withheld in the read; Save keeps the stored ones.
func (t appTools) editEnvironment(ctx context.Context, workspace, id string, edit func(*domain.Environment)) error {
	environments := t.environments.InWorkspace(workspace)
	state, err := environments.State(ctx)
	if err != nil {
		return err
	}
	for _, env := range state.Environments {
		if env.ID == id {
			edit(&env)
			return environments.Save(ctx, env)
		}
	}
	return domain.ErrNotFound
}

func withoutVariable(vars []domain.Variable, id string) ([]domain.Variable, bool) {
	kept := make([]domain.Variable, 0, len(vars))
	for _, v := range vars {
		if v.ID != id {
			kept = append(kept, v)
		}
	}
	return kept, len(kept) < len(vars)
}

func (t appTools) patchRequest(ctx context.Context, workspace string, node *domain.CollectionNode, patch RequestFields) error {
	if err := validAgentAuth(patch.Auth); err != nil {
		return err
	}
	if patch.Auth != nil {
		node.Auth = patch.Auth.Normalized().Stored()
	}
	for field, value := range map[string]*string{
		"name": patch.Name, "method": patch.Method, "url": patch.URL, "description": patch.Description, "body": patch.Body,
	} {
		if value != nil && strings.Contains(*value, domain.SecretMask) {
			return fmt.Errorf("%s contains a masked value; omit it to preserve the original", field)
		}
	}
	if patch.Name != nil {
		node.Name = *patch.Name
	}
	if patch.Method != nil {
		node.Method = *patch.Method
	}
	if patch.URL != nil {
		node.URL = *patch.URL
	}
	if patch.Description != nil {
		node.Description = *patch.Description
	}
	if patch.Body != nil {
		node.Body = *patch.Body
	}
	if patch.BodyKind != nil {
		kind := domain.BodyKind(*patch.BodyKind)
		switch kind {
		case domain.BodyRaw, domain.BodyJSON, domain.BodyXML:
		default:
			return errors.New("bodyKind must be raw, json or xml")
		}
		node.BodyKind, node.BodyFile, node.Form = kind, "", nil
	}
	if patch.Environment != nil {
		if *patch.Environment != "" {
			state, err := t.environments.InWorkspace(workspace).State(ctx)
			if err != nil {
				return err
			}
			found := false
			for _, env := range state.Environments {
				if env.ID == *patch.Environment {
					found = true
					break
				}
			}
			if !found {
				return domain.ErrNotFound
			}
		}
		node.EnvironmentID = *patch.Environment
	}
	for _, rows := range []struct {
		from *[]Header
		to   *[]domain.Row
	}{{patch.Headers, &node.Headers}, {patch.Params, &node.Params}} {
		if rows.from == nil {
			continue
		}
		values := make([]domain.Row, 0, len(*rows.from))
		ids := platform.NewIDGen()
		for _, row := range *rows.from {
			if strings.Contains(row.Value, domain.SecretMask) {
				return errors.New("masked rows cannot be written; omit the field to preserve existing rows")
			}
			values = append(values, domain.Row{ID: ids(), Name: row.Name, Value: row.Value, Enabled: true})
		}
		*rows.to = values
	}
	return nil
}

func validAgentAuth(auth *domain.Auth) error {
	if auth == nil {
		return nil
	}
	if _, ok := domain.SchemeFor(auth.Type); !ok {
		return errors.New("unknown authorization type")
	}
	for _, value := range auth.Fields {
		if strings.Contains(value, domain.SecretMask) {
			return errors.New("masked authorization cannot be saved; omit auth to preserve it or use a variable reference")
		}
	}
	return nil
}
