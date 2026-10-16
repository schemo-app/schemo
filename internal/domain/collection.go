package domain

// UntitledRequest names a request that an imported file names nowhere.
const UntitledRequest = "Untitled Request"

type Collection struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Position    int64            `json:"position"`
	CreatedAt   int64            `json:"createdAt"`
	UpdatedAt   int64            `json:"updatedAt"`
	ParentID    string           `json:"parentId,omitempty"`
	Items       []CollectionNode `json:"items"`
	Children    []Collection     `json:"children"`
	Auth        *Auth            `json:"auth,omitempty"`
	Variables   []Variable       `json:"variables,omitempty"`
	// A level's own code, carried by a file and written only where a tree is written whole — the
	// import and the duplicate. It is deliberately not written by SaveCollection: that is called
	// with rows read from the shallow tree, which carries no code, so writing it there would erase
	// the code of a level whenever anything about it is renamed.
	Scripts *Scripts `json:"scripts,omitempty"`
}

type LevelRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
}

type LevelEntry struct {
	Node       *CollectionNode
	Collection *Collection
}

func (c *Collection) Level() []LevelEntry {
	out := make([]LevelEntry, 0, len(c.Items)+len(c.Children))
	i, j := 0, 0
	for i < len(c.Items) || j < len(c.Children) {
		if j >= len(c.Children) || (i < len(c.Items) && c.Items[i].Position <= c.Children[j].Position) {
			out = append(out, LevelEntry{Node: &c.Items[i]})
			i++
			continue
		}
		out = append(out, LevelEntry{Collection: &c.Children[j]})
		j++
	}
	return out
}

type CollectionNode struct {
	ID            string    `json:"id"`
	CollectionID  string    `json:"collectionId"`
	Name          string    `json:"name"`
	Position      int64     `json:"position"`
	Method        string    `json:"method,omitempty"`
	URL           string    `json:"url,omitempty"`
	Params        []Row     `json:"params,omitempty"`
	Headers       []Row     `json:"headers,omitempty"`
	PathParams    []Row     `json:"pathParams,omitempty"`
	Body          string    `json:"body,omitempty"`
	BodyKind      BodyKind  `json:"bodyKind,omitempty"`
	Form          []FormRow `json:"form,omitempty"`
	BodyFile      string    `json:"bodyFile,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	Auth          *Auth     `json:"auth,omitempty"`
	Scripts       *Scripts  `json:"scripts,omitempty"`
	Description   string    `json:"description,omitempty"`
	// Responses are the example answers a file carried beside the request — a Postman example, a
	// document's declared response. They are read-only here: a request this app sends composes its
	// own answer, and an example is a note about what one answered once.
	Responses []ResponseExample `json:"responses,omitempty"`
	CreatedAt int64             `json:"createdAt"`
	UpdatedAt int64             `json:"updatedAt"`
}

// ResponseExample is one saved answer: what it is called, the status it carried, its headers and
// its body as they were written down.
type ResponseExample struct {
	Name    string       `json:"name,omitempty"`
	Status  int          `json:"status,omitempty"`
	Body    string       `json:"body,omitempty"`
	Headers []HeaderPair `json:"headers,omitempty"`
}

type CollectionRun struct {
	ID           string                `json:"id"`
	CollectionID string                `json:"collectionId"`
	NodeID       string                `json:"nodeId,omitempty"`
	Environment  string                `json:"environment,omitempty"`
	StartedAt    int64                 `json:"startedAt"`
	FinishedAt   int64                 `json:"finishedAt,omitempty"`
	Passed       int                   `json:"passed"`
	Failed       int                   `json:"failed"`
	DurationUs   int64                 `json:"durationUs"`
	Results      []CollectionRunResult `json:"results"`
}

type CollectionRunResult struct {
	Status           *int     `json:"status,omitempty"`
	Failure          *Failure `json:"failure,omitempty"`
	NodeID           string   `json:"nodeId"`
	Error            string   `json:"error,omitempty"`
	RecordID         string   `json:"recordId,omitempty"`
	Position         int64    `json:"position"`
	DurationUs       int64    `json:"durationUs"`
	AssertionsPassed int      `json:"assertionsPassed"`
	AssertionsTotal  int      `json:"assertionsTotal"`
	OK               bool     `json:"ok"`
	Skipped          bool     `json:"skipped,omitempty"`
}
