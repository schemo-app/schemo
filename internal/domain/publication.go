package domain

import (
	"regexp"
	"time"
)

// Publication is a collection put at an address anybody may read, as this device remembers it.
//
// It is a copy and nothing more: the server issues the address and keeps the page, and what stands
// here is the last answer it gave, kept so that a window draws a published collection as published
// before anybody is asked anything.
type Publication struct {
	Options      *PublicationOptions `json:"options,omitempty"`
	CollectionID string              `json:"collectionId"`

	// WorkspaceID is the space the collection lives in, which is where the window's list of pages
	// asks for it.
	WorkspaceID string `json:"workspaceId,omitempty"`

	// ID is the server's row. Empty until a publish has been answered.
	ID string `json:"id,omitempty"`

	// Slug is the last segment of the address, and URL the whole of it to hand out.
	Slug string `json:"slug,omitempty"`
	URL  string `json:"url,omitempty"`

	Visibility string `json:"visibility,omitempty"`
	Status     string `json:"status,omitempty"`
	Revision   int    `json:"revision,omitempty"`

	// ContentHash is the digest of what was published, so that a device can say whether the page is
	// behind what it holds without publishing to find out.
	ContentHash     string `json:"contentHash,omitempty"`
	EnvironmentName string `json:"environmentName,omitempty"`
	UpdatedAt       int64  `json:"updatedAt,omitempty"`

	// CanManage is the server's answer about the caller who asked, and never about the row.
	CanManage bool `json:"canManage"`

	// PendingUnpublish marks a takedown that has not reached the server yet — the one thing here that
	// is this device's own state rather than the server's.
	PendingUnpublish bool `json:"pendingUnpublish,omitempty"`

	// Outline and Changes are that same device's own state said at length: the shape the last publish
	// put up, and what has moved at the page since. No answer of the server's carries either, so they
	// are written by a save of their own and every other write of the row leaves them alone.
	Outline []OutlineItem `json:"outline,omitempty"`
	Changes []PageRelease `json:"changes,omitempty"`
}

type PublicationOptions struct {
	Docs     bool `json:"docs"`
	Examples bool `json:"examples"`
}

func (p Publication) PageOptions() PublicationOptions {
	if p.Options != nil {
		return *p.Options
	}
	return PublicationOptions{Docs: true, Examples: true}
}

// OutlineItem is one request as the last publish put it up: enough to know it again, and enough to
// tell a request that changed from one that is new. The fingerprint is of the request the page
// drew, so a changelog entry says nothing a reader of the page was not already shown.
type OutlineItem struct {
	ID          string            `json:"id"`
	Method      string            `json:"method"`
	Name        string            `json:"name"`
	Fingerprint string            `json:"fingerprint"`
	Parts       map[string]string `json:"parts"`
}

// PageRelease is one entry of what a page says has moved at it, and PageItem one request the entry
// touched. Sign is this device's own word — "+" for one that arrived, "~" for one that changed, "-"
// for one that went — and a page draws the colour it stands for without translating it.
type PageRelease struct {
	Revision int        `json:"revision"`
	At       time.Time  `json:"at"`
	Items    []PageItem `json:"items"`
}

// PageItem is one request a release touched: what happened to it, and which request it was.
type PageItem struct {
	Fields []string `json:"fields"`
	Sign   string   `json:"sign"`
	Method string   `json:"method"`
	Name   string   `json:"name"`
}

type PublicationVersion struct {
	Revision        int    `json:"revision"`
	PublishedAt     int64  `json:"publishedAt"`
	EnvironmentName string `json:"environmentName"`
	Requests        int    `json:"requests"`
	RestoredFrom    int    `json:"restoredFrom"`
}

type PublicationDetails struct {
	Views    int64                `json:"views"`
	Imports  int64                `json:"imports"`
	Versions []PublicationVersion `json:"versions"`
}

// PublicationDraft carries the public document, its digest and access settings to the server.
type PublicationDraft struct {
	Collection  string
	Workspace   string
	Visibility  string
	Document    []byte
	ContentHash string
	// Confirmed is the author saying they have been told the page answers to whoever holds the link.
	Confirmed bool
	// Password is sent separately from the public document; local copies belong in the OS keyring.
	Password string
}

// Who a page is for. The vocabulary is the server's, because it is the server that refuses one.
const (
	// VisibilityUnlisted answers at the address and nowhere else, and is what a publish that says
	// nothing gets.
	VisibilityUnlisted = "unlisted"

	// VisibilityPublic names the collection to anyone who finds the page.
	VisibilityPublic = "public"

	// VisibilityPassword answers only to a reader who sends the password. The page keeps a hash and
	// never the text, and this device hands the text over once rather than writing it down.
	VisibilityPassword = "password"
)

// What a page's password may be, in bytes, as the server holds it: below the first it is a guess
// away, and a password past the second is one the server's own hash would silently cut short. These
// stand here rather than beside the check because the window draws the same two, from `layout.ts`.
const (
	PasswordLeast = 8
	PasswordMost  = 72
)

// Whether a page answers at all.
const (
	PublicationActive  = "active"
	PublicationRevoked = "revoked"
)

// Runnable reports whether a page answers at the address. A revoked page is not gone: its row keeps
// both the bytes and the address, so putting the collection back up answers with the page it had.
func (p Publication) Runnable() bool {
	return p.Status == PublicationActive && p.ID != "" && !p.PendingUnpublish
}

// HiddenReason says why a variable of a collection is not on the page. A page carries no reason of
// its own for having left something out, and this is only ever said in the window that publishes.
type HiddenReason string

const (
	// HiddenSecret is a variable this app already marks as a secret.
	HiddenSecret HiddenReason = "secret"

	// HiddenLinked is a variable an environment reaches only through one that is hidden, and so is
	// one whose value is on the page by another name.
	HiddenLinked HiddenReason = "linked"

	// HiddenLooksSecret is a variable whose name reads like a credential. Nothing about it is known —
	// the name alone is why it stays behind, which is the point: a secret nobody marked is the one
	// that would otherwise be published by accident.
	HiddenLooksSecret HiddenReason = "looksSecret"
)

// slugForm is the shape of an address the server issues: a readable part, a dash, and ten random
// characters. The random tail is what makes the address unguessable, and its fixed length is what
// lets this side tell a page's address from any other path without asking the server.
var slugForm = regexp.MustCompile(`^[a-z0-9-]{1,40}-[a-z0-9]{10}$`)

// ValidSlug reports whether the text has the form of an address the server issues.
func ValidSlug(slug string) bool { return slugForm.MatchString(slug) }
