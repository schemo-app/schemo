package command

// Rendering a request back as a command — one tool per file beside this one. Only curl is ever read
// back; all five are ways of carrying a request out of here for a person to paste in a terminal.

import (
	"path/filepath"
	"strings"

	"schemo/internal/domain"
)

// A Format is one of the five notations. FormatURL is the address alone, the one shape of a
// request that is not a command; FormatCurlLines is curl written for a file — one option per line
// — rather than for a terminal, where a nine-header command is one line nobody reads.
type Format string

const (
	FormatCurl       Format = "curl"
	FormatCurlLines  Format = "curl-lines"
	FormatFetch      Format = "fetch"
	FormatWget       Format = "wget"
	FormatHTTPie     Format = "httpie"
	FormatPowerShell Format = "powershell"
	FormatURL        Format = "url"
)

// shellQuote closes and reopens the quotes around an embedded one: to a POSIX shell the pieces are
// one word and the quote survives as itself.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote is PowerShell's rule instead: inside single quotes a quote is written twice.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Losing a header to a name collision would change the command, so the list collapses: a name
// already there keeps its position and takes the new value.
type orderedHeaders struct {
	names []string
	vals  map[string]string
}

func (h *orderedHeaders) set(name, value string) {
	if h.vals == nil {
		h.vals = map[string]string{}
	}
	if _, seen := h.vals[name]; !seen {
		h.names = append(h.names, name)
	}
	h.vals[name] = value
}

func (h orderedHeaders) entries() []domain.HeaderPair {
	out := make([]domain.HeaderPair, 0, len(h.names))
	for _, name := range h.names {
		out = append(out, domain.HeaderPair{Name: name, Value: h.vals[name]})
	}
	return out
}

// commandInput is one request as a renderer sees it. The body is text, a file's path, or a form's
// fields — which of them a tool can write is the tool's business.
type commandInput struct {
	url     string
	method  string
	headers []domain.HeaderPair
	// tool is the notation's name, which a sentence about an upload it could not write needs.
	tool   string
	text   string
	file   string
	fields []domain.FormRow
}

// isUpload is whether the body must be written as a reference to a file rather than as text.
func (in commandInput) isUpload() bool {
	return in.file != "" || len(in.fields) > 0
}

// formFields is the rows of a form a command writes: a row switched off or unnamed is a part no
// server can read, which is the rule the multipart body follows too.
func (in commandInput) formFields() []domain.FormRow {
	out := make([]domain.FormRow, 0, len(in.fields))
	for _, row := range in.fields {
		if row.Enabled && strings.TrimSpace(row.Name) != "" {
			out = append(out, row)
		}
	}
	return out
}

// uploadsOmitted is what a notation that cannot write a file body says: one omission per file, a
// field by its key and a whole body by its file.
func (in commandInput) uploadsOmitted() []Omission {
	out := []Omission{}
	if in.file != "" {
		out = append(out, Omission{Kind: OmissionUpload, Name: filepath.Base(in.file)})
	}
	for _, row := range in.formFields() {
		if row.File && row.Src != "" {
			out = append(out, Omission{Kind: OmissionUpload, Name: strings.TrimSpace(row.Name)})
		}
	}
	return out
}

// Render writes a request out as a command, and says what the notation could not carry. An
// upload is the one thing it names rather than dropping in silence.
func Render(format Format, seed Request) (string, []Omission) {
	var headers orderedHeaders
	for _, h := range seed.Headers {
		headers.set(h.Name, h.Value)
	}

	in := commandInput{
		url:     seed.URL,
		method:  seed.Method,
		headers: headers.entries(),
		tool:    string(format),
	}

	// Told apart by what the copy left in the seed: a form without a file was rendered into the
	// multipart text, and what stays a reference is a file.
	switch {
	case seed.Body == "" && seed.BodyFile != "":
		in.file = seed.BodyFile
	case seed.Body == "" && len(seed.Form) > 0:
		in.fields = seed.Form
	default:
		in.text = seed.Body
	}

	switch format {
	case FormatCurl:
		return renderCurl(in, false)
	case FormatCurlLines:
		return renderCurl(in, true)
	case FormatFetch:
		return renderFetch(in)
	case FormatWget:
		return renderWget(in)
	case FormatHTTPie:
		return renderHTTPie(in)
	case FormatPowerShell:
		return renderPowerShell(in)
	case FormatURL:
		return in.url, nil
	}
	return "", nil
}
