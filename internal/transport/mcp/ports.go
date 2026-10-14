// Package mcp serves the app to MCP agents over a local HTTP port: the spaces, collections,
// requests and history, and — when the person allows it — sending a saved request. The port is
// loopback only, every call carries a token, and a secret leaves as its mask or not at all.
package mcp

import "context"

// Tools is what the server can ask the app. It is deliberately a port of small questions rather
// than the use cases: the answers here are already shaped for an agent — masked where a value would
// be a secret, named rather than addressed — so that the policy of what an agent may see lives
// where the policy lives, and not in the drawing of an answer.
type Tools interface {
	Spaces(ctx context.Context) ([]Space, error)
	Collections(ctx context.Context, spaceID string) ([]Collection, error)
	Requests(ctx context.Context, collectionID string) ([]Request, error)
	Request(ctx context.Context, id string) (Request, error)
	History(ctx context.Context, spaceID string, limit int) ([]HistoryRow, error)
	Response(ctx context.Context, recordID string) (Response, error)
	// Send makes a real call with the person's environment — inherited variables and authorization
	// resolved by the app — and answers with what came back. The agent's name travels with it: the
	// record it leaves in the history is marked with whose doing the send was.
	Send(ctx context.Context, requestID string, agent string) (Response, error)
}

// Space is one workspace as an agent addresses it.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Collection is a collection or a folder; ParentID is empty at the top of a space.
type Collection struct {
	ID          string `json:"id"`
	Space       string `json:"space"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Requests    int    `json:"requests"`
}

// Request is one saved request. The address, the headers and the body arrive with every secret left
// as its mask — there is no field on this shape that carries a secret's value — and the
// authorization itself is not here at all: an agent composes nothing, it names a request, and the
// app is the one that fills it in.
type Request struct {
	ID         string   `json:"id"`
	Space      string   `json:"space"`
	Collection string   `json:"collection"`
	Name       string   `json:"name"`
	Method     string   `json:"method"`
	URL        string   `json:"url"`
	Headers    []Header `json:"headers,omitempty"`
	Body       string   `json:"body,omitempty"`
	BodyKind   string   `json:"bodyKind,omitempty"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HistoryRow is one record of the history, without bodies.
type HistoryRow struct {
	ID         string `json:"id"`
	Space      string `json:"space"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	StartedAt  int64  `json:"startedAt"`
}

// Response is what came back. Space is not drawn: it is what the server checks a by-id read
// against, and the agent already knows which space it asked about.
type Response struct {
	Status     int      `json:"status,omitempty"`
	StatusText string   `json:"statusText,omitempty"`
	DurationMs int64    `json:"durationMs,omitempty"`
	Headers    []Header `json:"headers,omitempty"`
	Body       string   `json:"body,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	Error      string   `json:"error,omitempty"`
	Space      string   `json:"-"`
}
