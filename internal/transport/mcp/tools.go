package mcp

import (
	"context"
	"errors"
	"slices"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
)

// The tools an agent is offered. Every one of them answers from the Tools port, and the policy —
// which spaces, whether sending is allowed, whether a send is confirmed — is checked here, where
// the answers are composed, so that no port implementation has to know what an agent may do.
//
// The names and descriptions are for the agent's model, so they are English and say what the tool
// answers; what the person reads about all this is the window's, and it is worded in the catalogue.

const (
	// maxHistoryLimit bounds a history answer: an agent does not need the whole space, and the
	// bodies it asks for afterwards have a size of their own.
	maxHistoryLimit = 100
	defaultHistory  = 20
)

type spacesOut struct {
	Spaces []Space `json:"spaces"`
}

type collectionsIn struct {
	Space string `json:"space,omitempty" jsonschema:"an id from the spaces tool; omit for all spaces"`
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
	Space  string `json:"space,omitempty" jsonschema:"an id from the spaces tool; omit for all spaces"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many records, newest first (default 20)"`
	Record string `json:"record,omitempty" jsonschema:"a record's id, reads what it answered"`
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

// errNoSpace is the answer to a by-id question about a space the agent may not reach: the id is
// real, and that is all it says — whether the space exists for somebody else is not an agent's to
// learn.
var errNoSpace = errors.New("that space is not one this agent may reach")

// registerReadTools adds the four tools every agent is offered, whatever the policy is.
func (s *Server) registerReadTools() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "spaces",
		Description: "List the spaces (workspaces) of the Schemo desktop client, with the id each " +
			"of them is addressed by. Only the spaces the person allowed are answered.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolSpaces)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "collections",
		Description: "List the collections and folders of one space, or of every space the agent " +
			"may reach. Each row carries its id, name, description, the folder above it and how " +
			"many saved requests it holds.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCollections)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "requests",
		Description: "List the saved requests of a collection, or read one saved request whole. " +
			"Address, headers and body arrive masked — a secret's value is never handed over.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolRequests)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "history",
		Description: "List the requests Schemo sent in a space, newest first, with what each of " +
			"them answered. With a record id, read that record's response body.",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolHistory)
}

// setSendTool offers or withdraws the send tool. Adding a name that is already there replaces it,
// which is what a policy change comes to; the agent is told the list moved.
func (s *Server) setSendTool(allowed bool) {
	if !allowed {
		s.mcp.RemoveTools("send_request")
		return
	}
	no, yes := false, true
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name: "send_request",
		Description: "Send one saved request with the person's environment: stored variables and " +
			"authorization are filled in by the app, and the response is answered back. Real calls " +
			"leave this machine — the tool is offered only when the person allows sending.",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &no,
			OpenWorldHint:   &yes,
		},
	}, s.toolSend)
}

func (s *Server) toolSpaces(
	ctx context.Context,
	req *sdk.CallToolRequest,
	_ struct{},
) (*sdk.CallToolResult, spacesOut, error) {
	policy := s.opened(req)
	spaces, err := s.tools.Spaces(ctx)
	if err != nil {
		return nil, spacesOut{}, err
	}
	out := spacesOut{Spaces: []Space{}}
	for _, space := range spaces {
		if policy.Allows(space.ID) {
			out.Spaces = append(out.Spaces, space)
		}
	}
	return nil, out, nil
}

func (s *Server) toolCollections(
	ctx context.Context,
	req *sdk.CallToolRequest,
	in collectionsIn,
) (*sdk.CallToolResult, collectionsOut, error) {
	policy := s.opened(req)

	out := collectionsOut{Collections: []Collection{}}
	if in.Space != "" {
		if !policy.Allows(in.Space) {
			return nil, collectionsOut{}, errNoSpace
		}
		rows, err := s.tools.Collections(ctx, in.Space)
		if err != nil {
			return nil, collectionsOut{}, err
		}
		out.Collections = append(out.Collections, rows...)
		return nil, out, nil
	}

	spaces, err := s.reachable(ctx, policy)
	if err != nil {
		return nil, collectionsOut{}, err
	}
	for _, space := range spaces {
		rows, err := s.tools.Collections(ctx, space.ID)
		if err != nil {
			return nil, collectionsOut{}, err
		}
		out.Collections = append(out.Collections, rows...)
	}
	return nil, out, nil
}
func (s *Server) toolRequests(
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
		if !policy.Allows(one.Space) {
			return nil, requestsOut{}, errNoSpace
		}
		return nil, requestsOut{Request: &one}, nil
	case in.Collection != "":
		rows, err := s.tools.Requests(ctx, in.Collection)
		if err != nil {
			return nil, requestsOut{}, err
		}
		out := requestsOut{Requests: []Request{}}
		for _, row := range rows {
			// The collection is checked above; this filters rows of a request that arrived from
			// somewhere the listing did not name.
			if policy.Allows(row.Space) {
				out.Requests = append(out.Requests, row)
			}
		}
		return nil, out, nil
	default:
		return nil, requestsOut{}, errors.New("name a collection to list, or a request to read")
	}
}

func (s *Server) toolHistory(
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
		if !policy.Allows(response.Space) {
			return nil, historyOut{}, errNoSpace
		}
		return nil, historyOut{Response: &response}, nil
	}

	if in.Space != "" && !policy.Allows(in.Space) {
		return nil, historyOut{}, errNoSpace
	}
	limit := min(max(in.Limit, 0), maxHistoryLimit)
	if limit == 0 {
		limit = defaultHistory
	}

	out := historyOut{Records: []HistoryRow{}}
	spaces, err := s.reachable(ctx, policy)
	if err != nil {
		return nil, historyOut{}, err
	}
	for _, space := range spaces {
		if in.Space != "" && space.ID != in.Space {
			continue
		}
		rows, err := s.tools.History(ctx, space.ID, limit)
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

func (s *Server) toolSend(
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
	if !policy.Allows(one.Space) {
		return nil, sendOut{}, errNoSpace
	}

	if policy.Ask {
		allow, err := s.confirms.ask(ctx, ConfirmRequest{
			Agent:  agent.Name,
			Method: one.Method,
			URL:    one.URL,
			Space:  one.Space,
			Name:   one.Name,
		}, s.publishConfirm)
		if err != nil {
			return nil, sendOut{}, err
		}
		if !allow {
			return nil, sendOut{}, errors.New("the person did not allow this send")
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
func (s *Server) opened(req *sdk.CallToolRequest) domain.MCPSettings {
	s.agents.track(req.Session)
	s.publishChanged()
	return s.policyNow()
}

// reachable is the spaces the agent may see, as the port answers them.
func (s *Server) reachable(ctx context.Context, policy domain.MCPSettings) ([]Space, error) {
	spaces, err := s.tools.Spaces(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(spaces, func(space Space) bool { return !policy.Allows(space.ID) }), nil
}
