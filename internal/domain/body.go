package domain

// The media types a body is sent as and read back from, named once for every reader and writer.
const (
	MediaJSON      = "application/json"
	MediaXML       = "application/xml"
	MediaForm      = "application/x-www-form-urlencoded"
	MediaMultipart = "multipart/form-data"
	MediaText      = "text/plain"
	MediaHTML      = "text/html"
)

type BodyKind string

const (
	BodyRaw    BodyKind = "raw"
	BodyJSON   BodyKind = "json"
	BodyXML    BodyKind = "xml"
	BodyForm   BodyKind = "form"
	BodyBinary BodyKind = "binary"
)

func KindOf(kind BodyKind) BodyKind {
	if kind == "" {
		return BodyRaw
	}
	return kind
}

type FormRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Src     string `json:"src,omitempty"`
	File    bool   `json:"file,omitempty"`
	Enabled bool   `json:"enabled"`
}
