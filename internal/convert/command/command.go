// Package command reads a request from a pasted curl and writes one in each supported notation.
// Request is the package's own shape; callers map it onto what they store.
package command

import (
	"schemo/internal/domain"
)

// Kind is what reading a text came to.
type Kind string

const (
	KindNone  Kind = "none"
	KindOK    Kind = "ok"
	KindError Kind = "error"
)

// Reason is why a command was recognised but could not be read. The values cross to the window,
// which has a sentence for each, so they are a contract with the locale files.
type Reason string

const (
	ReasonNoURL                Reason = "no-url"
	ReasonBadQuotes            Reason = "bad-quotes"
	ReasonLeftover             Reason = "leftover"
	ReasonUnsupportedMultipart Reason = "unsupported-multipart"
)

// Request is a request as the format sees it: a draft's seed without the environment.
type Request struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Body    string              `json:"body"`
	Headers []domain.HeaderPair `json:"headers,omitempty"`
	// A command carries the format it was read with, as a seed does: the Content-Type that follows
	// from the kind is part of what the request goes out with.
	BodyKind domain.BodyKind  `json:"bodyKind,omitempty"`
	Form     []domain.FormRow `json:"form,omitempty"`
	BodyFile string           `json:"bodyFile,omitempty"`
	Auth     *domain.Auth     `json:"auth,omitempty"`
}

// Reading is what a pasted text came to. KindNone is the ordinary case and not a failure: most of
// what a person pastes is not a command at all, and the field keeps it untouched.
type Reading struct {
	Kind    Kind    `json:"kind"`
	Request Request `json:"request"`
	Reason  Reason  `json:"reason"`
}

// OmissionKind is the sort of thing a command had to leave out. The values are a contract with the
// locale files, which hold the sentence for each.
type OmissionKind string

const (
	// OmissionVariable is a `{{name}}` nothing answers; the command keeps the braces.
	OmissionVariable OmissionKind = "variable"
	// OmissionToken is a scheme that fetches its credential, with none held.
	OmissionToken OmissionKind = "token"
	// OmissionDigest is a Digest credential, a hash over a nonce only the server can send.
	OmissionDigest OmissionKind = "digest"
	// OmissionUpload is a body a notation cannot write as a reference to a file.
	OmissionUpload OmissionKind = "upload"
)

// Omission is one part left out and what the window's sentence names with it: a variable, a form
// field, or the file a body is read from. Name is empty where the kind is the whole of it.
type Omission struct {
	Kind OmissionKind `json:"kind"`
	Name string       `json:"name,omitempty"`
}

// Parse reads pasted text as a curl command. Most pastes are not commands at all, and coming back
// with KindNone is what lets the window put them in the field untouched.
func Parse(text string) Reading {
	normalized := normalizeInput(text)
	if trimSpace(normalized) == "" {
		return Reading{Kind: KindNone}
	}

	rest, ok := detect(normalized)
	if !ok {
		return Reading{Kind: KindNone}
	}

	tokens, tokOK := tokenize(rest)
	if !tokOK {
		return Reading{Kind: KindError, Reason: ReasonBadQuotes}
	}

	parsed, reason := fromCurl(tokens)
	if reason != "" {
		return Reading{Kind: KindError, Reason: reason}
	}

	req := parsed.request()
	// An ok result must be usable: no empty URL or method. Pinned by TestEveryOKResultIsUsable.
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.URL == "" {
		return Reading{Kind: KindError, Reason: ReasonNoURL}
	}
	return Reading{Kind: KindOK, Request: req}
}
