package agent

import (
	"context"
	"errors"
	"slices"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
)

// The tools an agent is offered. Every one answers from the Tools port, and the policy (which
// workspaces, whether sending is allowed, whether a send is confirmed) is checked here where the
// answers are composed, so no port implementation has to know what an agent may do.
//
// Names and descriptions are for the agent's model, so they are English and say what the tool
// answers; what the person reads is the window's, worded in the catalogue.

const (
	// maxHistoryLimit bounds a history answer: an agent does not need the whole workspace, and the
	// bodies it asks for afterwards have a size of their own.
	maxHistoryLimit = 100
	defaultHistory  = 20
)

type workspacesOut struct {
	Workspaces []Workspace `json:"workspaces"`
}

type collectionsIn struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"an id from the workspaces tool; omit for all workspaces"`
}

type collectionsOut struct {
	Collections []Collection `json:"collections"`
}

type requestsIn struct {
	Collection string `json:"collection,omitempty" jsonschema:"a collection's id, lists its requests"`
	Request    string `json:"request,omitempty" jsonschema:"a request's id, reads it whole and masked"`
}

type requestsOut struct {
	Requests []Request `json:"requests,omitempty"`
	Request  *Request  `json:"request,omitempty"`
}

type historyIn struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"an id from the workspaces tool; omit for all workspaces"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many records, newest first (default 20)"`
	Record    string `json:"record,omitempty" jsonschema:"a record's id, reads what it answered"`
}

type historyOut struct {
	Records  []HistoryRow `json:"records,omitempty"`
	Response *Response    `json:"response,omitempty"`
}

type sendIn struct {
	Request string `json:"request" jsonschema:"id of the saved request to send"`
}

type sendOut struct {
	Response Response `json:"response"`
}

// errNoWorkspace is the answer to a by-id question about a workspace the agent may not reach: the id is
// real, and that is all it says. Whether the workspace exists for somebody else is not for an agent to
// learn.
var errNoWorkspace = errors.New("that workspace is not one this agent may reach")

// registerReadTools adds the four tools every agent is offered, whatever the policy is.
func (s *MCP) registerReadTools() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "workspaces",
		Description: "List the workspaces (workspaces) of the Schemo desktop client, with the id each " +
			"of them is addressed by. Only the workspaces the person allowed are answered.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolWorkspaces)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "collections",
		Description: "List the collections and folders of one workspace, or of every workspace the agent " +
			"may reach. Each row carries its id, name, description, the folder above it and how " +
			"many saved requests it holds.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCollections)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "requests",
		Description: "List the saved requests of a collection, or read one saved request whole. " +
			"Includes saved authorization. Protected variable values are masked; inline API credentials are readable.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolRequests)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "history",
		Description: "List the requests Schemo sent in a workspace, newest first, with what each of " +
			"them answered. With a record id, read that record's response body.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolHistory)
}

// setSendTool offers or withdraws the send tool. Adding a name that is already there replaces it,
// which is what a policy change comes to; the agent is told the list moved.
func (s *MCP) setSendTool(allowed bool) {
	if !allowed {
		s.mcp.RemoveTools("send_request")
		return
	}
	yes := true
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "send_request",
		Description: "Send one saved request with the person's environment: stored variables and " +
			"authorization are filled in by the app, and the response is answered back. Real calls " +
			"leave this machine — the tool is offered only when the person allows sending.",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &yes,
			OpenWorldHint:   &yes,
		},
	}, s.toolSend)
}

func (s *MCP) toolWorkspaces(
	ctx context.Context,
	req *sdk.CallToolRequest,
	_ struct{},
) (*sdk.CallToolResult, workspacesOut, error) {
	policy := s.opened(req)
	workspaces, err := s.tools.Workspaces(ctx)
	if err != nil {
		return nil, workspacesOut{}, err
	}
	out := workspacesOut{Workspaces: []Workspace{}}
	for _, workspace := range workspaces {
		if policy.Allows(workspace.ID) {
			out.Workspaces = append(out.Workspaces, workspace)
		}
	}
	return nil, out, nil
}

func (s *MCP) toolCollections(
	ctx context.Context,
	req *sdk.CallToolRequest,
	in collectionsIn,
) (*sdk.CallToolResult, collectionsOut, error) {
	policy := s.opened(req)

	out := collectionsOut{Collections: []Collection{}}
	if in.Workspace != "" {
		if err := s.requireWorkspace(ctx, policy, in.Workspace); err != nil {
			return nil, collectionsOut{}, err
		}
		rows, err := s.tools.Collections(ctx, in.Workspace)
		if err != nil {
			return nil, collectionsOut{}, err
		}
		out.Collections = append(out.Collections, rows...)
		return nil, out, nil
	}

	workspaces, err := s.reachable(ctx, policy)
	if err != nil {
		return nil, collectionsOut{}, err
	}
	for _, workspace := range workspaces {
		rows, err := s.tools.Collections(ctx, workspace.ID)
		if err != nil {
			return nil, collectionsOut{}, err
		}
		out.Collections = append(out.Collections, rows...)
	}
	return nil, out, nil
}
func (s *MCP) toolRequests(
	ctx context.Context,
	req *sdk.CallToolRequest,
	in requestsIn,
) (*sdk.CallToolResult, requestsOut, error) {
	policy := s.opened(req)

	switch {
	case in.Request != "" && in.Collection != "":
		return nil, requestsOut{}, errors.New("name either a collection or a request, not both")
	case in.Request != "":
		one, err := s.tools.Request(ctx, in.Request)
		if err != nil {
			return nil, requestsOut{}, err
		}
		if err := s.requireWorkspace(ctx, policy, one.Workspace); err != nil {
			return nil, requestsOut{}, err
		}
		return nil, requestsOut{Request: &one}, nil
	case in.Collection != "":
		workspaces, err := s.reachable(ctx, policy)
		if err != nil {
			return nil, requestsOut{}, err
		}
		rows, err := s.tools.Requests(ctx, in.Collection)
		if err != nil {
			return nil, requestsOut{}, err
		}
		out := requestsOut{Requests: []Request{}}
		for _, row := range rows {
			if slices.ContainsFunc(workspaces, func(workspace Workspace) bool { return workspace.ID == row.Workspace }) {
				out.Requests = append(out.Requests, row)
			}
		}
		return nil, out, nil
	default:
		return nil, requestsOut{}, errors.New("name a collection to list, or a request to read")
	}
}

func (s *MCP) toolHistory(
	ctx context.Context,
	req *sdk.CallToolRequest,
	in historyIn,
) (*sdk.CallToolResult, historyOut, error) {
	policy := s.opened(req)

	if in.Record != "" {
		response, err := s.tools.Response(ctx, in.Record)
		if err != nil {
			return nil, historyOut{}, err
		}
		if err := s.requireWorkspace(ctx, policy, response.Workspace); err != nil {
			return nil, historyOut{}, err
		}
		return nil, historyOut{Response: &response}, nil
	}

	if in.Workspace != "" && !policy.Allows(in.Workspace) {
		return nil, historyOut{}, errNoWorkspace
	}
	limit := min(max(in.Limit, 0), maxHistoryLimit)
	if limit == 0 {
		limit = defaultHistory
	}

	out := historyOut{Records: []HistoryRow{}}
	workspaces, err := s.reachable(ctx, policy)
	if err != nil {
		return nil, historyOut{}, err
	}
	for _, workspace := range workspaces {
		if in.Workspace != "" && workspace.ID != in.Workspace {
			continue
		}
		rows, err := s.tools.History(ctx, workspace.ID, limit)
		if err != nil {
			return nil, historyOut{}, err
		}
		out.Records = append(out.Records, rows...)
	}
	slices.SortFunc(out.Records, func(a, b HistoryRow) int { return int(b.StartedAt - a.StartedAt) })
	if len(out.Records) > limit {
		out.Records = out.Records[:limit]
	}
	return nil, out, nil
}

func (s *MCP) toolSend(
	ctx context.Context,
	req *sdk.CallToolRequest,
	in sendIn,
) (*sdk.CallToolResult, sendOut, error) {
	policy := s.opened(req)
	if policy.Mode != domain.MCPSend {
		return nil, sendOut{}, errors.New(
			"sending is not allowed: the MCP server is set to reading only")
	}
	if in.Request == "" {
		return nil, sendOut{}, errors.New("name the saved request to send")
	}

	agent := s.agents.track(req.Session)
	s.publishChanged()
	one, err := s.tools.Request(ctx, in.Request)
	if err != nil {
		return nil, sendOut{}, err
	}
	if err := s.requireWorkspace(ctx, policy, one.Workspace); err != nil {
		return nil, sendOut{}, err
	}

	if policy.Ask {
		allow, err := s.confirms.ask(ctx, ConfirmRequest{
			Agent:     agent.Name,
			Method:    one.Method,
			URL:       one.URL,
			Workspace: one.Workspace,
			Name:      one.Name,
		}, s.publishConfirm)
		if err != nil {
			return nil, sendOut{}, err
		}
		if !allow {
			return nil, sendOut{}, errors.New("the person did not allow this send")
		}
		// Approval may outlive a logout, account switch or a change to MCP permissions.
		current := s.policyNow()
		if !current.Enabled || current.Mode != domain.MCPSend {
			return nil, sendOut{}, errors.New("sending access was revoked while awaiting approval")
		}
		if err := s.requireWorkspace(ctx, current, one.Workspace); err != nil {
			return nil, sendOut{}, err
		}
	}

	response, err := s.tools.Send(ctx, in.Request, agent.Name)
	if err != nil {
		return nil, sendOut{}, err
	}
	return nil, sendOut{Response: response}, nil
}

// opened is the start of every tool call: the agent is noted, the window is told, and the policy
// this call runs under is read — once, at the top, so that one call cannot see two answers.
func (s *MCP) opened(req *sdk.CallToolRequest) domain.MCPSettings {
	s.agents.track(req.Session)
	s.publishChanged()
	return s.policyNow()
}

// reachable is the workspaces the agent may see, as the port answers them.
func (s *MCP) reachable(ctx context.Context, policy domain.MCPSettings) ([]Workspace, error) {
	workspaces, err := s.tools.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(workspaces, func(workspace Workspace) bool { return !policy.Allows(workspace.ID) }), nil
}

// requireWorkspace intersects MCP scope with the current account's visible local workspaces.
func (s *MCP) requireWorkspace(ctx context.Context, policy domain.MCPSettings, id string) error {
	if id == "" || !policy.Allows(id) {
		return errNoWorkspace
	}
	workspaces, err := s.reachable(ctx, policy)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(workspaces, func(workspace Workspace) bool { return workspace.ID == id }) {
		return errNoWorkspace
	}
	return nil
}
