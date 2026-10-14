package domain

import "slices"

// MCPTokenPrefix begins every access token this app hands out, so one found somewhere is recognised
// as this app's.
const MCPTokenPrefix = "sk-sch-"

const (
	DefaultMCPPort = 9300
	MinMCPPort     = 1024
	MaxMCPPort     = 65535
)

// MCPMode is how much an agent may do: read the spaces, collections, requests and history, or read
// and send — sending being real calls made with the person's environments.
type MCPMode string

const (
	MCPRead MCPMode = "read"
	MCPSend MCPMode = "send"
)

func (m MCPMode) Valid() bool { return m == MCPRead || m == MCPSend }

// MCPSettings is the server as it was left: off until somebody turns it on, because a port that
// opens by itself is a door nobody asked for. The token is minted at that moment — off is nothing
// to hand out — and the mode, the asking and the spaces are read on every call, so a change lands
// in the next one rather than in a round already under way. The value itself never leaves on its
// own: a window is told HasToken, and only an explicit reveal hands it over.
type MCPSettings struct {
	Token     string   `json:"token,omitempty"`
	TokenTail string   `json:"tokenTail,omitempty"`
	Mode      MCPMode  `json:"mode"`
	Spaces    []string `json:"spaces,omitempty"`
	Port      int      `json:"port"`
	Enabled   bool     `json:"enabled"`
	HasToken  bool     `json:"hasToken"`
	Ask       bool     `json:"ask"`
	AllSpaces bool     `json:"allSpaces"`
}

func DefaultMCP() MCPSettings {
	return MCPSettings{
		Enabled:   false,
		Port:      DefaultMCPPort,
		Mode:      MCPSend,
		Ask:       true,
		AllSpaces: true,
		Spaces:    []string{},
	}
}

func ValidMCPPort(port int) bool { return port >= MinMCPPort && port <= MaxMCPPort }

// Allows is whether a space is one the agent may reach. A space list without the flag is still a
// list: the two answers are kept apart, and only «all» makes the list moot.
func (m MCPSettings) Allows(spaceID string) bool {
	return m.AllSpaces || slices.Contains(m.Spaces, spaceID)
}
