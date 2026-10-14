package domain

// DraftID names one of the drafts the window is editing. It is a type of its own so that the one id
// written by hand — the command line's — reaches the window as a named constant rather than as a
// string spelled a second time, where a typo would be a draft that cannot be found at runtime.
type DraftID string

// DraftCommandLine is the id of the draft the window's command line edits. A collection node's
// draft is keyed by the node it came from, so the two are never the same draft.
const DraftCommandLine DraftID = "command-line"

type Row struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type RowKind string

const (
	RowParams     RowKind = "params"
	RowHeaders    RowKind = "headers"
	RowForm       RowKind = "form"
	RowPathParams RowKind = "pathParams"
)

type Draft struct {
	ID       DraftID `json:"id"`
	Revision int64   `json:"revision"`
	Method   string  `json:"method"`
	URL      string  `json:"url"`
	Params   []Row   `json:"params"`
	Headers  []Row   `json:"headers"`
	// PathParams holds one row for each `:name` the address carries and is derived from it: the
	// name is the address's, and what the row keeps is the value and the description.
	PathParams    []Row     `json:"pathParams"`
	Auth          Auth      `json:"auth"`
	Body          string    `json:"body"`
	BodyKind      BodyKind  `json:"bodyKind"`
	Form          []FormRow `json:"form"`
	BodyFile      string    `json:"bodyFile,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
}

func NewDraft() Draft {
	return Draft{
		Method:     "GET",
		Params:     []Row{},
		Headers:    []Row{},
		PathParams: []Row{},
		Auth:       Auth{Type: AuthNone},
		Form:       []FormRow{},
	}
}
