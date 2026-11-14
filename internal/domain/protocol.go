package domain

import "net/http"

// Protocol selects a transport or request format. The zero value is HTTP.
type Protocol string

const (
	ProtocolGraphQL   Protocol = "gql"
	ProtocolSSE       Protocol = "sse"
	ProtocolWebSocket Protocol = "ws"
	ProtocolGRPC      Protocol = "grpc"
)

func (p Protocol) Valid() bool {
	return p == "" || p == ProtocolGraphQL || p == ProtocolGRPC || p.Live()
}

func (p Protocol) Live() bool {
	return p == ProtocolSSE || p == ProtocolWebSocket
}

func (p Protocol) HTTPMethod(method string) string {
	switch p {
	case ProtocolGraphQL, ProtocolGRPC:
		return http.MethodPost
	case ProtocolSSE, ProtocolWebSocket:
		return http.MethodGet
	default:
		return method
	}
}

// HasStatus distinguishes a gRPC OK (zero) from an exchange with no response.
func (p Protocol) HasStatus(status int, text string) bool {
	return status != 0 || (p == ProtocolGRPC && text != "")
}

// ValidStatus reports whether a status can be used in a saved response example.
func (p Protocol) ValidStatus(status int) bool {
	if p == ProtocolGRPC {
		return status >= 0 && status <= 16
	}
	return status >= 100 && status <= 599
}

type ProtocolOptions struct {
	Variables     string `json:"variables,omitempty"`
	OperationName string `json:"operationName,omitempty"`
	RPCMethod     string `json:"rpcMethod,omitempty"`
	// Descriptors is the schema merged from SchemaSources when they were last read: a gRPC file
	// descriptor set, or a GraphQL schema's type definitions. The request works from it offline.
	Descriptors   string         `json:"descriptors,omitempty"`
	SchemaSources []SchemaSource `json:"schemaSources,omitempty"`
	MessageFormat MessageFormat  `json:"messageFormat,omitempty"`
	Messages      []SavedMessage `json:"messages,omitempty"`
}

type SchemaSourceKind string

const (
	SchemaSourceServer SchemaSourceKind = "server"
	SchemaSourceFile   SchemaSourceKind = "file"
	SchemaSourceFolder SchemaSourceKind = "folder"
)

// SchemaSource is one place a gRPC or GraphQL schema is read from. A request merges all of its
// sources, so a module's files can describe what the server does not reflect.
type SchemaSource struct {
	Kind SchemaSourceKind `json:"kind"`
	// Path is the file or folder on disk; a server is read at the request's own address.
	Path string `json:"path,omitempty"`
	// Skipped are the files under a folder taken out of the schema, relative to it with forward
	// slashes. Files added to the folder later are read.
	Skipped []string `json:"skipped,omitempty"`
	// What the source gave when it was last read, so the list of sources shows it without reading.
	LoadedAt int64        `json:"loadedAt,omitempty"`
	Methods  int          `json:"methods,omitempty"`
	Files    []SchemaFile `json:"files,omitempty"`
}

type SchemaFile struct {
	Name    string `json:"name"`
	Methods int    `json:"methods,omitempty"`
}

type MessageFormat string

const (
	MessageJSON   MessageFormat = "json"
	MessageText   MessageFormat = "text"
	MessageBinary MessageFormat = "binary"
)

type SavedMessage struct {
	Data   string        `json:"data"`
	Format MessageFormat `json:"format"`
}

type StreamMessage struct {
	Sequence  int    `json:"sequence"`
	At        int64  `json:"at"`
	Direction string `json:"direction"`
	Name      string `json:"name,omitempty"`
	ID        string `json:"id,omitempty"`
	Data      string `json:"data"`
	Binary    bool   `json:"binary,omitempty"`
	Bytes     int    `json:"bytes"`
}

// ProtocolRecord is the protocol metadata kept beside the HTTP-compatible record fields.
type ProtocolRecord struct {
	Protocol     Protocol       `json:"protocol,omitempty"`
	Options      *ReplayOptions `json:"options,omitempty"`
	Operation    string         `json:"operation,omitempty"`
	MessageCount int            `json:"messageCount,omitempty"`
}

// ReplayOptions keeps the settings needed to repeat an exchange from history.
type ReplayOptions struct {
	Variables     string `json:"variables,omitempty"`
	OperationName string `json:"operationName,omitempty"`
	RPCMethod     string `json:"rpcMethod,omitempty"`
	Descriptors   string `json:"descriptors,omitempty"`
}
