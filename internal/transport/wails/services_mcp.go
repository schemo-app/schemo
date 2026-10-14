package wails

import (
	"context"
	"log"

	"schemo/internal/domain"
	"schemo/internal/transport/mcp"
	"schemo/internal/usecase/settings"
)

// McpService exposes the MCP server to the Wails window: its state, the token, and the two things a
// person decides about a connection — who is disconnected, and whether a send may go.
type McpService struct {
	server   *mcp.Server
	settings *settings.UseCase
}

func NewMcpService(server *mcp.Server, settings *settings.UseCase) *McpService {
	return &McpService{server: server, settings: settings}
}

// Status is what the section draws at the top and in the rail: whether the server listens and
// where, and who has called. It is a reading rather than an answer — a window that opens after an
// agent's call has nothing coming to it.
func (s *McpService) Status() mcp.Status {
	return s.server.Status()
}

// Rotate mints a fresh token and ends every agent that was speaking through the old one. The token
// is written down first and the guard re-keyed in the same call: an agent that meets the new one
// before it is stored would be an agent the next restart forgets. The value goes from the use case
// straight to the guard — it is in no answer, so there is nothing to carry it here.
func (s *McpService) Rotate(ctx context.Context) (domain.Settings, error) {
	return s.settings.RotateMCPToken(ctx, s.server.Rotate)
}

// RevealToken hands the token over for the eye beside it. It is the one call that does: the section
// is told HasToken like every other answer, and the value arrives only when it is asked for.
func (s *McpService) RevealToken(ctx context.Context) (string, error) {
	return s.settings.RevealMCPToken(ctx)
}

// Disconnect ends one agent's sessions and forgets it. The token stays valid: this is about the
// client that is speaking now, not about the door.
func (s *McpService) Disconnect(id string) bool {
	return s.server.Disconnect(id)
}

// Answer delivers the window's decision on a send that is waiting for it. The request is worded by
// the section; Go only carries the answer.
func (s *McpService) Answer(id string, allow bool) {
	s.server.Answer(id, allow)
}

func (s *McpService) start(ctx context.Context) error {
	held, err := s.settings.MCPSettings(ctx)
	if err != nil {
		log.Printf("[mcp] reading the settings: %v", err)
		return nil
	}
	if !held.Enabled {
		return nil
	}
	if err := s.server.Apply(held); err != nil {
		log.Printf("[mcp] starting: %v", err)
	}
	return nil
}

// ServiceShutdown stops the listener within its own bounded time, so a client holding a call open
// does not hold the app's exit.
func (s *McpService) ServiceShutdown() error {
	return s.server.Shutdown(context.Background())
}
