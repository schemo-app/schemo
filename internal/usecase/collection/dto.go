package collection

import "schemo/internal/domain"

// NodeDraft is what the tree asks for when a row is created: where it goes and what is already
// known about the request. A request made by hand arrives with its method and nothing else — the
// rest is filled in by the card that opens next — while one saved from the command line is a whole
// request, and building it empty first would be a node with no address if the second step failed.
type NodeDraft struct {
	CollectionID string `json:"collectionId"`
	Name         string `json:"name"`

	Method     string           `json:"method,omitempty"`
	URL        string           `json:"url,omitempty"`
	Params     []domain.Row     `json:"params,omitempty"`
	Headers    []domain.Row     `json:"headers,omitempty"`
	PathParams []domain.Row     `json:"pathParams,omitempty"`
	Body       string           `json:"body,omitempty"`
	BodyKind   domain.BodyKind  `json:"bodyKind,omitempty"`
	Form       []domain.FormRow `json:"form,omitempty"`
	BodyFile   string           `json:"bodyFile,omitempty"`
	Auth       *domain.Auth     `json:"auth,omitempty"`
	// EnvironmentID carries a command-line request's pin into the node "Save" makes of it — a
	// request that was pointed at a particular environment stays pointed at it once saved.
	EnvironmentID string `json:"environmentId,omitempty"`
}

// RunProgress is one request of a run finishing. The counters are here and not in the window: the
// window that opened the collections view while a run was going would otherwise count from the
// middle, and the request that just finished is a row it can draw at once.
type RunProgress struct {
	RunID string `json:"runId"`
	// What the run was started from, so a window that has since looked at another level can tell
	// which run a row belongs to: it is the same pair the finished run carries.
	CollectionID string `json:"collectionId"`
	NodeID       string `json:"nodeId"`
	// The environment the run is going out under, carried here for the same reason the page keeps it:
	// a window that draws rows as they arrive must not label them with whatever is selected by then.
	Environment string                     `json:"environment,omitempty"`
	Done        int                        `json:"done"`
	Total       int                        `json:"total"`
	Result      domain.CollectionRunResult `json:"result"`
}
