package domain

// DocKind is the notation a response body was read as.
type DocKind string

const (
	DocJSON DocKind = "json"
	DocXML  DocKind = "xml"
	DocCSV  DocKind = "csv"
	DocHTML DocKind = "html"
)

// NodeKind is what a position in a document holds.
type NodeKind string

const (
	NodeObject    NodeKind = "object"
	NodeArray     NodeKind = "array"
	NodeString    NodeKind = "string"
	NodeNumber    NodeKind = "number"
	NodeBoolean   NodeKind = "boolean"
	NodeNull      NodeKind = "null"
	NodeElement   NodeKind = "element"
	NodeAttribute NodeKind = "attribute"
	NodeText      NodeKind = "text"
)

// Opened is a document that has been read: its handle and kind.
type Opened struct {
	ID   string  `json:"id"`
	Kind DocKind `json:"kind"`
}

// PathForm is a node's address in one notation.
type PathForm struct {
	Syntax string `json:"syntax"`
	Text   string `json:"text"`
}

// NodeView is what the inspector says about one position.
type NodeView struct {
	Kind NodeKind `json:"kind"`
	// Name is the node's key or tag; empty for the root and array items.
	Name string `json:"name"`
	// Path is the canonical address: JSONPath in JSON and CSV, XPath in markup.
	Path string `json:"path"`
	// Parent is the address one level up; empty at the root.
	Parent string `json:"parent"`
	// Preview is a scalar's text, cut short; empty for containers.
	Preview string `json:"preview"`
	// Count is a container's children or a string's characters.
	Count int `json:"count"`
	// Bytes is the size of the node's text.
	Bytes int `json:"bytes"`
	// Head is where the member begins (its key, if any); Start and End bound its value. All in
	// UTF-16 units.
	Head  int `json:"head"`
	Start int `json:"start"`
	End   int `json:"end"`
	// Forms is the address in each notation worth pasting.
	Forms []PathForm `json:"forms"`
	// Readings are the ways the value reads as something else.
	Readings []Reading `json:"readings"`
	// Crumbs is the way down from the root, the node itself last.
	Crumbs []Crumb `json:"crumbs"`
	// Children is a container's members, capped; Count says how many there really are.
	Children []NodeChild `json:"children"`
}

// Crumb is one step of the way down to a node.
type Crumb struct {
	// Name is empty for the root.
	Name string `json:"name"`
	Path string `json:"path"`
}

// NodeChild is a member of a container, as the inspector lists it without opening it.
type NodeChild struct {
	Kind NodeKind `json:"kind"`
	// Name is the key, or the index in brackets for an array item.
	Name    string `json:"name"`
	Path    string `json:"path"`
	Preview string `json:"preview"`
}

// Reading is a way a value reads beyond its text: a token, a moment, a colour.
type Reading struct {
	// Kind names the reading ("jwt", "timestamp", "uuid"); the window words it.
	Kind string `json:"kind"`
	// Rows are the facts found.
	Rows []ReadingRow `json:"rows"`
	// Nested is a document found inside the value, formatted.
	Nested string `json:"nested,omitempty"`
}

// ReadingRow is one fact of a reading. Value is data, never a sentence; Key is worded by the
// window.
type ReadingRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Shape says how to draw Value: "text", "code", "time" (RFC 3339) or "colour".
	Shape string `json:"shape"`
}

// TypeFormat is a notation types are written in.
type TypeFormat string

const (
	TypeTypeScript TypeFormat = "typescript"
	TypeGo         TypeFormat = "go"
	TypePython     TypeFormat = "python"
	TypeJSONSchema TypeFormat = "jsonschema"
)

// TypeFormats lists the notations in the order offered.
var TypeFormats = []TypeFormat{TypeTypeScript, TypeGo, TypePython, TypeJSONSchema}

// Diagnosis is what an error response says is wrong with the request, whatever its shape.
type Diagnosis struct {
	// Format names the shape read: "problem", "jsonapi", "validation", "spring" or "error".
	Format   string    `json:"format"`
	Problems []Problem `json:"problems"`
}

// Problem is one complaint and, when named, where in the request the fault is.
type Problem struct {
	Message string `json:"message"`
	// Where is the part of the request: "body", "query", "header" or empty.
	Where string `json:"where"`
	// Field is the member: a JSON Pointer in a body, a name elsewhere.
	Field string `json:"field"`
	// Path is where the message stands in the response.
	Path string `json:"path"`
}

// Span is a stretch of text in UTF-16 units.
type Span struct {
	Head  int `json:"head"`
	Start int `json:"start"`
	End   int `json:"end"`
}

// Suggestion is a collection request a value looks like it belongs in.
type Suggestion struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// Filled is the request's address with the value put in its matched place.
	Filled string `json:"filled"`
}
