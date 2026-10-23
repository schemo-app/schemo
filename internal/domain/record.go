package domain

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RecordSource defines the origin of a request record (manual builder or browser extension).
type RecordSource string

const (
	SourceManual  RecordSource = "manual"
	SourceBrowser RecordSource = "browser"
)

// BodySide indicates whether a record payload belongs to the request or response side.
type BodySide string

const (
	SideRequest  BodySide = "request"
	SideResponse BodySide = "response"
)

// InlineBodyLimit specifies the maximum body payload size (64KB) kept inline with a record.
const InlineBodyLimit = 64 << 10

// BodyRef points to a request/response body payload, either inline or referenced by size.
type BodyRef struct {
	Inline    string `json:"inline,omitempty"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

// RecordFetched contains request and response body references for remote records.
type RecordFetched struct {
	Bodies map[BodySide]BodyRef `json:"bodies,omitempty"`

	// Scripts are what the code around the record reported, and they come with it for the reason they
	// are stored beside it: a report whose record is missing is a report of nothing.
	Scripts []ScriptRun `json:"scripts,omitempty"`

	// Run is the collection run the record was sent by, when this device has not been told about it.
	// It is absent for a request sent on its own, and for a run the log already brought.
	Run *CollectionRun `json:"run,omitempty"`
}

// PruneOptions specifies retention limits for local history and cached remote records.
type PruneOptions struct {
	MaxCount   int
	MaxAge     time.Duration
	CacheCount int
	CacheAge   time.Duration
}

// HistoryStats provides historical record count and body storage metrics for a workspace.
type HistoryStats struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// RecordSummary contains lightweight metadata for rendering record lists.
type RecordSummary struct {
	ID          string       `json:"id"`
	WorkspaceID string       `json:"workspaceId,omitempty"`
	Source      RecordSource `json:"source"`
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	ContentType string       `json:"contentType,omitempty"`
	Error       string       `json:"error,omitempty"`
	DurationUs  int64        `json:"durationUs"`
	StartedAt   int64        `json:"startedAt"`
	NodeID      string       `json:"nodeId,omitempty"`
	// RunID is the collection run this record was sent by, empty for a request sent on its own. The
	// run travels as a document of its own, and the record names it rather than carrying a copy.
	RunID string `json:"runId,omitempty"`
	// Agent is who sent this through the MCP server when it was not the person. Empty for sends
	// made by the window, a run or the command line.
	Agent      string `json:"agent,omitempty"`
	TabID      int    `json:"tabId,omitempty"`
	TabTitle   string `json:"tabTitle,omitempty"`
	TabURL     string `json:"tabURL,omitempty"`
	FavIconURL string `json:"favIconUrl,omitempty"`
	IsRemote   bool   `json:"isRemote,omitempty"`
}

// Record represents full execution history, including timing phases, headers, and bodies.
type Record struct {
	RecordSummary

	Cancelled       bool          `json:"cancelled,omitempty"`
	DNSUs           *int64        `json:"dnsUs,omitempty"`
	ConnectUs       *int64        `json:"connectUs,omitempty"`
	TLSUs           *int64        `json:"tlsUs,omitempty"`
	WaitUs          *int64        `json:"waitUs,omitempty"`
	DownloadUs      *int64        `json:"downloadUs,omitempty"`
	RequestBytes    int64         `json:"requestBytes,omitempty"`
	ResponseBytes   int64         `json:"responseBytes,omitempty"`
	RequestHeaders  []HeaderPair  `json:"requestHeaders"`
	ResponseHeaders []HeaderPair  `json:"responseHeaders"`
	CookieWrites    []CookieWrite `json:"cookieWrites,omitempty"`
	RequestBody     *BodyRef      `json:"requestBody,omitempty"`
	ResponseBody    *BodyRef      `json:"responseBody,omitempty"`
	Skipped         bool          `json:"skipped,omitempty"`
}

// A record travels as a JSON document: the record itself, without the fields the envelope carries
// and without the bodies, which travel beside it. Its secret paths are the values of credential
// headers and of the cookies an answer wrote; the server withholds them from readers who may not
// see secrets, and a device that keeps secrets at home withholds them itself.

// secretHeaders are the headers whose value is a credential wherever a record carries it.
var secretHeaders = []string{"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization"}

func secretHeader(name string) bool {
	return slices.ContainsFunc(secretHeaders, func(secret string) bool {
		return strings.EqualFold(name, secret)
	})
}

// RecordDocument is a record as the wire carries it, and the paths of its secrets. Values this
// device never had (withheld from it by the server) are named as withheld.
func RecordDocument(record Record) (data []byte, secrets, withheld []string, err error) {
	doc := record
	doc.WorkspaceID, doc.IsRemote = "", false
	doc.RequestBody, doc.ResponseBody = nil, nil
	doc.CookieWrites = slices.Clone(record.CookieWrites)
	for i := range doc.CookieWrites {
		doc.CookieWrites[i].Withheld, doc.CookieWrites[i].PreviousWithheld = false, false
	}
	data, err = json.Marshal(doc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encoding record %s: %w", record.ID, err)
	}
	for side, headers := range map[string][]HeaderPair{
		"requestHeaders": record.RequestHeaders, "responseHeaders": record.ResponseHeaders,
	} {
		for i, header := range headers {
			if secretHeader(header.Name) {
				secrets = append(secrets, "/"+side+"/"+strconv.Itoa(i)+"/value")
			}
		}
	}
	for i, write := range record.CookieWrites {
		at := "/cookieWrites/" + strconv.Itoa(i)
		secrets = append(secrets, at+"/value")
		if write.Withheld {
			withheld = append(withheld, at+"/value")
		}
		if write.Previous != nil {
			secrets = append(secrets, at+"/previous/value")
			if write.PreviousWithheld {
				withheld = append(withheld, at+"/previous/value")
			}
		}
	}
	slices.Sort(secrets)
	return data, secrets, withheld, nil
}

// RecordFromDocument reads a record the server sent. A withheld cookie value is flagged, so the
// device keeps the one it holds instead of an empty one.
func RecordFromDocument(data []byte, withheld []string) (Record, error) {
	var record Record
	if len(data) > 0 {
		if err := json.Unmarshal(data, &record); err != nil {
			return Record{}, fmt.Errorf("reading a record: %w", err)
		}
	}
	for _, path := range withheld {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) < 3 || parts[0] != "cookieWrites" {
			continue
		}
		i, err := strconv.Atoi(parts[1])
		if err != nil || i < 0 || i >= len(record.CookieWrites) {
			continue
		}
		switch strings.Join(parts[2:], "/") {
		case "value":
			record.CookieWrites[i].Withheld = true
		case "previous/value":
			record.CookieWrites[i].PreviousWithheld = true
		}
	}
	return record, nil
}

type HeaderPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func PairsFromMap(headers map[string]string) []HeaderPair {
	out := make([]HeaderPair, 0, len(headers))
	for name, value := range headers {
		out = append(out, HeaderPair{Name: name, Value: value})
	}
	slices.SortFunc(out, func(a, b HeaderPair) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

type Response struct {
	DNSUs         *int64        `json:"dnsUs,omitempty"`
	ConnectUs     *int64        `json:"connectUs,omitempty"`
	TLSUs         *int64        `json:"tlsUs,omitempty"`
	WaitUs        *int64        `json:"waitUs,omitempty"`
	DownloadUs    *int64        `json:"downloadUs,omitempty"`
	StatusText    string        `json:"statusText"`
	Body          string        `json:"body"`
	FinalURL      string        `json:"finalUrl,omitempty"`
	ContentType   string        `json:"contentType"`
	Error         string        `json:"error,omitempty"`
	Headers       []HeaderPair  `json:"headers"`
	SentHeaders   []HeaderPair  `json:"sentHeaders,omitempty"`
	CookieWrites  []CookieWrite `json:"cookieWrites,omitempty"`
	Status        int           `json:"status"`
	DurationUs    int64         `json:"durationUs"`
	Cancelled     bool          `json:"cancelled,omitempty"`
	BodyTruncated bool          `json:"bodyTruncated,omitempty"`
}

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
