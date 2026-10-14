package wails

import (
	"context"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/transport/mcp"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/workspace"
)

// maxAgentBody bounds a response body handed to an agent. A body is read to be understood, and a
// hundred megabytes of it is neither; what was cut is said rather than silently dropped.
const maxAgentBody = 256 << 10

// mcpTools is what the MCP server sees of the app: the use cases joined the way this layer joins
// them everywhere else — the tree, the draft's masking and the history are three features, and
// neither of them knows the two others. It reads spaces the window is not showing, which is why
// every read is addressed by a space rather than by the scope on screen, and it masks what it hands
// over with the same machinery that keeps a secret out of the history: the marker is the app's one
// `••••`, and there is no second way to hide a value.
type mcpTools struct {
	workspaces  *workspace.UseCase
	collections *collection.UseCase
	records     *record.UseCase
	environment *environment.UseCase
}

var _ mcp.Tools = mcpTools{}

func newMCPTools(
	workspaces *workspace.UseCase,
	collections *collection.UseCase,
	records *record.UseCase,
	environment *environment.UseCase,
) mcp.Tools {
	return mcpTools{
		workspaces: workspaces, collections: collections, records: records, environment: environment,
	}
}

func (t mcpTools) Spaces(ctx context.Context) ([]mcp.Space, error) {
	state, err := t.workspaces.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.Space, 0, len(state.Workspaces))
	for _, space := range state.Workspaces {
		out = append(out, mcp.Space{ID: space.ID, Name: space.Name, Kind: string(space.Kind)})
	}
	return out, nil
}

func (t mcpTools) Collections(ctx context.Context, spaceID string) ([]mcp.Collection, error) {
	tree, err := t.collections.TreeOf(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := []mcp.Collection{}
	var walk func(rows []domain.Collection)
	walk = func(rows []domain.Collection) {
		for _, row := range rows {
			out = append(out, mcp.Collection{
				ID:          row.ID,
				Space:       spaceID,
				Name:        row.Name,
				Description: row.Description,
				Parent:      row.ParentID,
				Requests:    len(row.Items),
			})
			walk(row.Children)
		}
	}
	walk(tree)
	return out, nil
}

func (t mcpTools) Requests(ctx context.Context, collectionID string) ([]mcp.Request, error) {
	space, err := t.collections.SpaceOf(ctx, collectionID)
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
	masked, err := t.environment.MaskTextsIn(ctx, space, nil, texts, "")
	if err != nil {
		return nil, err
	}

	out := make([]mcp.Request, 0, len(rows))
	for i, row := range rows {
		out = append(out, mcp.Request{
			ID:         row.ID,
			Space:      space,
			Collection: collectionID,
			Name:       row.Name,
			Method:     row.Method,
			URL:        masked[i],
		})
	}
	return out, nil
}

func (t mcpTools) Request(ctx context.Context, id string) (mcp.Request, error) {
	node, err := t.collections.Node(ctx, id)
	if err != nil {
		return mcp.Request{}, err
	}
	space, err := t.collections.SpaceOf(ctx, node.CollectionID)
	if err != nil {
		return mcp.Request{}, err
	}

	// What is masked is every text a value could sit in — the address, the body, and both halves of
	// every switched-on header row. The authorization itself is not here at all: a request's
	// credentials are the app's to fill in at the moment of sending, and an agent that needs them
	// gets the request sent rather than the secret.
	rows := enabledHeaders(node.Headers)
	texts := make([]string, 0, 2+2*len(rows))
	texts = append(texts, node.URL, node.Body)
	for _, row := range rows {
		texts = append(texts, row.Name, row.Value)
	}
	masked, err := t.environment.MaskTextsIn(ctx, space, nil, texts, node.EnvironmentID)
	if err != nil {
		return mcp.Request{}, err
	}

	out := mcp.Request{
		ID:         node.ID,
		Space:      space,
		Collection: node.CollectionID,
		Name:       node.Name,
		Method:     node.Method,
		URL:        masked[0],
		Body:       masked[1],
		BodyKind:   string(node.BodyKind),
	}
	for i := range rows {
		out.Headers = append(out.Headers, mcp.Header{
			Name:  masked[2+2*i],
			Value: masked[2+2*i+1],
		})
	}
	return out, nil
}

func (t mcpTools) History(
	ctx context.Context,
	spaceID string,
	limit int,
) ([]mcp.HistoryRow, error) {
	records, err := t.records.SpaceRecords(ctx, spaceID, limit)
	if err != nil {
		return nil, err
	}

	// A row of history is a text a value can sit in like any other: a captured address carries what
	// the page had in its query, and a failure quotes back the address that failed.
	texts := make([]string, 0, 2*len(records))
	for _, rec := range records {
		texts = append(texts, rec.URL, rec.Error)
	}
	masked, err := t.environment.MaskTextsIn(ctx, spaceID, nil, texts, "")
	if err != nil {
		return nil, err
	}

	out := make([]mcp.HistoryRow, 0, len(records))
	for i, rec := range records {
		out = append(out, mcp.HistoryRow{
			ID:         rec.ID,
			Space:      spaceID,
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

func (t mcpTools) Response(ctx context.Context, recordID string) (mcp.Response, error) {
	rec, err := t.records.Record(ctx, recordID)
	if err != nil {
		return mcp.Response{}, err
	}
	return t.responseOf(ctx, rec)
}

func (t mcpTools) Send(ctx context.Context, requestID, agent string) (mcp.Response, error) {
	node, err := t.collections.Node(ctx, requestID)
	if err != nil {
		return mcp.Response{}, err
	}
	space, err := t.collections.SpaceOf(ctx, node.CollectionID)
	if err != nil {
		return mcp.Response{}, err
	}
	// The send path is the app's own — the one a run and the window's own card both take: inherited
	// authorization and variables resolved, the pinned environment honored, and every secret left as
	// its mask in what is written down. The agent's name rides along, and the history it lands in
	// says whose send it was.
	rec, err := t.collections.Send(ctx, space, requestID, agent)
	if err != nil {
		return mcp.Response{}, err
	}
	return t.responseOf(ctx, rec)
}

// responseOf is what came back, as an agent reads it: the status, the headers the server sent, and
// the body — cut at a size an agent can read, with the cut said. Every text of it is masked: a
// Set-Cookie and a token an answer echoes back are the values the request side hides, and a read
// that skipped them would be the one way round the rule.
func (t mcpTools) responseOf(ctx context.Context, rec domain.Record) (mcp.Response, error) {
	body, err := t.records.Body(ctx, rec.ID, domain.SideResponse)
	if err != nil {
		return mcp.Response{}, err
	}

	out := mcp.Response{
		Status:     rec.Status,
		StatusText: rec.StatusText,
		DurationMs: rec.DurationUs / 1000,
		Space:      rec.WorkspaceID,
	}
	if len(body) > maxAgentBody {
		body, out.Truncated = body[:maxAgentBody], true
		// A cut in the middle of a character is not a character; the rest is what was read.
		body = strings.ToValidUTF8(body, "")
	}

	texts := make([]string, 0, 2+2*len(rec.ResponseHeaders))
	texts = append(texts, body, rec.Error)
	for _, header := range rec.ResponseHeaders {
		texts = append(texts, header.Name, header.Value)
	}
	masked, err := t.environment.MaskTextsIn(ctx, rec.WorkspaceID, nil, texts, "")
	if err != nil {
		return mcp.Response{}, err
	}

	out.Body, out.Error = masked[0], masked[1]
	for i := range rec.ResponseHeaders {
		out.Headers = append(out.Headers, mcp.Header{Name: masked[2+2*i], Value: masked[3+2*i]})
	}
	return out, nil
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
