package agent

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent is one MCP client as the window draws it: what it calls itself, when it last asked for
// something and how much it has asked for today. A client that never said its name is drawn by what
// little is known of it rather than hidden.
type Agent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	LastAt int64  `json:"lastAt"`
	Today  int    `json:"today"`
}

// tracked is one agent's row plus what the agentRegistry needs to drop it: the sessions it is speaking
// through right now, so that disconnecting closes them rather than merely forgetting them.
type tracked struct {
	Agent
	day      string
	sessions map[string]*mcp.ServerSession
}

// agentRegistry is who has called, and through which sessions. It is in memory on purpose: a
// connection's history is about right now, and a restart ends every connection there was.
type agentRegistry struct {
	mu      sync.Mutex
	agents  map[string]*tracked
	bySess  map[string]string
	changed func()
}

func newAgentRegistry(changed func()) *agentRegistry {
	return &agentRegistry{agents: map[string]*tracked{}, bySess: map[string]string{}, changed: changed}
}

// track records a call: the agent appears on its first one, which is what the window promises, and
// every later call moves it up.
func (r *agentRegistry) track(session *mcp.ServerSession) Agent {
	name, id := clientName(session)
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.agents[id]
	if !ok {
		row = &tracked{Agent: Agent{ID: id, Name: name}, sessions: map[string]*mcp.ServerSession{}}
		r.agents[id] = row
	}
	row.Name = name
	row.LastAt = now.UnixMilli()
	day := now.Format("2006-01-02")
	if row.day != day {
		row.day, row.Today = day, 0
	}
	row.Today++

	if session != nil {
		if sid := session.ID(); sid != "" && row.sessions[sid] == nil {
			row.sessions[sid] = session
			r.bySess[sid] = id
			// The session's own end is the moment to stop holding it: without this, a client that
			// reconnects would leave one more pointer behind every time.
			go func() {
				_ = session.Wait()
				r.sessionEnded(sid)
			}()
		}
	}
	return row.Agent
}

func (r *agentRegistry) sessionEnded(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agent, ok := r.bySess[id]; ok {
		delete(r.bySess, id)
		if row := r.agents[agent]; row != nil {
			delete(row.sessions, id)
		}
	}
}

// list is the agents as the window draws them, most recent first.
func (r *agentRegistry) list() []Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Agent, 0, len(r.agents))
	for _, row := range r.agents {
		out = append(out, row.Agent)
	}
	slices.SortFunc(out, func(a, b Agent) int { return cmp.Compare(b.LastAt, a.LastAt) })
	return out
}

// forget ends an agent's sessions and drops its row. The token stays: disconnecting concerns the
// client speaking now, and a fresh call with the same token is a fresh introduction, so the row
// comes back when it asks again.
func (r *agentRegistry) forget(id string) bool {
	r.mu.Lock()
	row, ok := r.agents[id]
	if ok {
		delete(r.agents, id)
		for sid, session := range row.sessions {
			delete(r.bySess, sid)
			go func() { _ = session.Close() }()
		}
	}
	r.mu.Unlock()
	return ok
}

// reset drops everyone — what reissuing the token comes to, since every agent holding the old one
// is no longer an agent at all.
func (r *agentRegistry) reset() {
	r.mu.Lock()
	rows := r.agents
	r.agents, r.bySess = map[string]*tracked{}, map[string]string{}
	r.mu.Unlock()

	for _, row := range rows {
		for _, session := range row.sessions {
			go func() { _ = session.Close() }()
		}
	}
}

// clientName is what an agent calls itself. The title is the human name ("Claude Desktop"), the
// name the programmatic one, and a client that sent neither is still a client.
func clientName(session *mcp.ServerSession) (name, id string) {
	if session != nil {
		if params := session.InitializeParams(); params != nil && params.ClientInfo != nil {
			info := params.ClientInfo
			switch {
			case info.Title != "":
				return info.Title, info.Title
			case info.Name != "":
				return info.Name, info.Name
			}
		}
		if sid := session.ID(); sid != "" {
			return "MCP client", "session:" + sid
		}
	}
	return "MCP client", "unknown"
}
