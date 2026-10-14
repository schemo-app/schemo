package transfer

// The shapes the transfer feature answers with: what a source turned out to hold, what an import
// wrote, and what a space holds for the source that copies from one.

import (
	"schemo/internal/domain"
)

// Format names what a source is read as, and what an export is written as.
type Format string

const (
	FormatSchemo   Format = "schemo"
	FormatPostman  Format = "postman"
	FormatOpenAPI  Format = "openapi"
	FormatInsomnia Format = "insomnia"
	FormatHAR      Format = "har"
	FormatCurl     Format = "curl"
	// FormatPage is a collection somebody published: the document a page is made of, which is this
	// app's own shape and is read from the address the page stands at.
	FormatPage Format = "page"
)

// FormatOf reads a format out of the word the window sends. An unknown word is refused rather than
// guessed at: reading a file as a shape it is not is how a person's collection comes out wrong.
func FormatOf(name string) (Format, error) {
	switch Format(name) {
	case FormatSchemo, FormatPostman, FormatOpenAPI, FormatInsomnia, FormatHAR, FormatCurl,
		FormatPage:
		return Format(name), nil
	}
	return "", domain.Refuse(domain.CodeUnknownFormat, domain.ErrNotAllowed,
		domain.Args{"format": name})
}

// Preview is what a source turned out to hold, before anything is written. The counters are what
// the dialog says before the person commits: a file card reads "48 KB · 9 requests in 3 folders",
// and the button says how many it will write.
type Preview struct {
	// Token names what was read, for the Apply that writes it. It is good once.
	Token string `json:"token"`
	// Name is what the document calls its first collection, empty where it names none — a pasted
	// command has no name of its own, and the window's own wording fills that in.
	Name string `json:"name,omitempty"`
	// File is the name the source is drawn under — the file, the dropped file, the URL — for the
	// card the dialog shows. Empty for a paste.
	File         string        `json:"file,omitempty"`
	Size         int           `json:"size"`
	Collections  int           `json:"collections"`
	Requests     int           `json:"requests"`
	Folders      int           `json:"folders"`
	Environments int           `json:"environments"`
	Skipped      []domain.Skip `json:"skipped,omitempty"`
}

// Summary is what an import wrote, for the tree to draw and the toast to count.
type Summary struct {
	Tree []domain.Collection `json:"tree"`
	// EnvState is the environment list as it is now, present only when the document carried one.
	EnvState     *domain.EnvState `json:"envState,omitempty"`
	Name         string           `json:"name,omitempty"`
	Collections  int              `json:"collections"`
	Requests     int              `json:"requests"`
	Environments int              `json:"environments"`
}

// Space is one space a copy could be taken from, with the collections it holds at the top level.
type Space struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// Collections is empty for a space nothing could be taken from only in the sense of the walk:
	// a space without collections answers with an empty list, which is an answer.
	Collections []SpaceCollection `json:"collections"`
}

// SpaceCollection is one collection of that space with the size of its whole subtree, because the
// picker says how much is being taken.
type SpaceCollection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Requests int    `json:"requests"`
	Folders  int    `json:"folders"`
}

// Export is a collection written as a file the window will save or copy: the bytes, and the name
// the save dialog should open on.
type Export struct {
	Data []byte `json:"data"`
	Name string `json:"name"`
}
