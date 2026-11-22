// Package convert imports and exports collections and environments.
package convert

import (
	"context"
	"strings"
	"sync"
	"time"

	"schemo/internal/cloud"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// pendingTTL is how long what a preview read waits to be written. A dialog is a person deciding;
// a token older than this belongs to a dialog that was closed without deciding.
const pendingTTL = 10 * time.Minute

// Transfer is what comes in from other tools and goes out to them, as the window asks for it.
type Transfer struct {
	collections  Collections
	environments Environments
	workspaces   Workspaces
	files        Files
	remote       Remote
	commands     Commands
	ids          platform.IDGen
	host         *desktop.Host
	mu           sync.Mutex
	staged       *stagedRead
}

// stagedRead is a document that was read and not yet written, under the token the window holds.
type stagedRead struct {
	token    string
	document domain.Document
	at       time.Time
}

func NewTransfer(
	collections Collections,
	environments Environments,
	workspaces Workspaces,
	files Files,
	remote Remote,
	commands Commands,
	ids platform.IDGen,
	host *desktop.Host,
) *Transfer {
	return &Transfer{
		collections:  collections,
		environments: environments,
		workspaces:   workspaces,
		files:        files,
		remote:       remote,
		commands:     commands,
		ids:          ids,
		host:         host,
	}
}

// previewText reads a pasted text — a collection somebody sent, or commands copied from a terminal.
func (s *Transfer) previewText(ctx context.Context, format Format, text string) (Preview, error) {
	return s.stagePreview(format, []byte(text))
}

// previewUrl reads a document from a URL; a published page is read at its document path.
func (s *Transfer) previewUrl(ctx context.Context, format Format, url string) (Preview, error) {
	address := url
	if format == FormatPage {
		held, err := cloud.DocumentAddress(url)
		if err != nil {
			return Preview{}, err
		}
		address = held
	}
	data, err := s.remote.Fetch(ctx, address)
	if err != nil {
		return Preview{}, err
	}
	return s.stagePreview(format, data)
}

// stagePreview puts what was read where Apply will find it and answers the counters the dialog
// shows.
func (s *Transfer) stagePreview(format Format, data []byte) (Preview, error) {
	document, err := s.read(format, data)
	if err != nil {
		return Preview{}, err
	}
	return s.stage(document, len(data)), nil
}

func (s *Transfer) stage(document domain.Document, size int) Preview {
	token := s.ids()
	s.mu.Lock()
	s.staged = &stagedRead{token: token, document: document, at: time.Now()}
	s.mu.Unlock()

	requests, folders := countDocument(document)
	return Preview{
		Token:        token,
		Name:         nameOfFirst(document),
		Size:         size,
		Collections:  len(document.Collections),
		Requests:     requests,
		Folders:      folders,
		Environments: len(document.Environments),
		Globals:      len(document.Globals),
		Skipped:      document.Skipped,
	}
}

// Apply writes what a preview read under a level and returns the updated tree and environments. The
// name fills in when the document has none; the token is single-use.
func (s *Transfer) Apply(
	ctx context.Context,
	token string,
	parentID string,
	name string,
	readonlySuffix string,
) (Summary, error) {
	document, err := s.claimStaged(token)
	if err != nil {
		return Summary{}, err
	}

	for index := range document.Collections {
		if strings.TrimSpace(document.Collections[index].Name) == "" {
			document.Collections[index].Name = name
		}
	}

	summary := Summary{Collections: len(document.Collections), CreatedEnvironments: []string{}}
	targets := []string{}
	if len(document.Collections) > 0 {
		imported, err := s.collections.ImportCollections(ctx, document.Collections, parentID)
		if err != nil {
			return Summary{}, err
		}
		summary.Tree, targets = imported.Tree, imported.Targets
		requests, _ := countDocument(domain.Document{Collections: document.Collections})
		summary.Requests = requests
	}
	if len(document.Environments) > 0 {
		report, err := s.environments.ImportDrafts(ctx, document.Environments, targets, readonlySuffix)
		if err != nil {
			return Summary{}, err
		}
		summary.EnvState = &report.State
		summary.Environments = len(document.Environments)
		summary.CreatedEnvironments = report.Created
	}
	if len(document.Globals) > 0 {
		if _, err := s.environments.ImportVariables(ctx, domain.EnvScope{}, document.Globals); err != nil {
			return Summary{}, err
		}
		state, err := s.environments.State(ctx)
		if err != nil {
			return Summary{}, err
		}
		summary.EnvState = &state
		summary.Globals = len(document.Globals)
	}
	if len(document.Collections) > 0 && len(document.Collections[0].Name) > 0 {
		summary.Name = document.Collections[0].Name
	}
	if summary.Collections == 0 && summary.Environments == 0 && summary.Globals == 0 {
		return Summary{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return summary, nil
}

// claimStaged is the document a token names, once: a second Apply with the same token is a window
// that lost track, and answering it twice would write the file twice.
func (s *Transfer) claimStaged(token string) (domain.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	read := s.staged
	if read == nil || read.token != token || time.Since(read.at) > pendingTTL {
		return domain.Document{}, domain.Refuse(domain.CodeImportExpired, domain.ErrNotFound, nil)
	}
	s.staged = nil
	return read.document, nil
}

// nameOfFirst is what the document calls its first collection, or its first environment where it
// holds none: the dialog shows it as the name the new row will take.
func nameOfFirst(document domain.Document) string {
	for _, collection := range document.Collections {
		if name := strings.TrimSpace(collection.Name); name != "" {
			return name
		}
	}
	for _, environment := range document.Environments {
		if name := strings.TrimSpace(environment.Name); name != "" {
			return name
		}
	}
	return ""
}

// Workspaces lists every workspace with its top-level collections, the open one marked for the window
// to skip.
func (s *Transfer) Workspaces(ctx context.Context) ([]Workspace, error) {
	state, err := s.workspaces.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(state.Workspaces))
	for _, workspace := range state.Workspaces {
		workspace := Workspace{ID: workspace.ID, Name: workspace.Name, Active: workspace.ID == state.ActiveID}
		tree, err := s.collections.TreeOf(ctx, workspace.ID)
		if err != nil {
			return nil, err
		}
		for _, collection := range tree {
			requests, folders := countCollection(collection)
			workspace.Collections = append(workspace.Collections, WorkspaceCollection{
				ID:       collection.ID,
				Name:     collection.Name,
				Requests: requests,
				Folders:  folders,
			})
		}
		out = append(out, workspace)
	}
	return out, nil
}

// ImportFromWorkspace copies collections of another workspace into the active one, read whole there and
// written here: the same walk an export takes, written where the person asked for it.
func (s *Transfer) ImportFromWorkspace(
	ctx context.Context,
	workspace string,
	ids []string,
	parentID string,
) (Summary, error) {
	if len(ids) == 0 {
		return Summary{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	collections := make([]domain.Collection, 0, len(ids))
	for _, id := range ids {
		collection, err := s.collections.FullOf(ctx, workspace, id)
		if err != nil {
			return Summary{}, err
		}
		collections = append(collections, collection)
	}

	tree, err := s.collections.Import(ctx, collections, parentID)
	if err != nil {
		return Summary{}, err
	}
	requests, _ := countDocument(domain.Document{Collections: collections})
	return Summary{
		Tree:        tree,
		Name:        collections[0].Name,
		Collections: len(collections),
		Requests:    requests,
	}, nil
}

// Curl writes a level's requests as curl commands, each under a `#` name line that the paste reads
// back. Secrets are values or references as asked.
func (s *Transfer) Curl(ctx context.Context, id string, secrets bool) (string, error) {
	requests, err := s.collections.Requests(ctx, id, "")
	if err != nil {
		return "", err
	}
	if len(requests) == 0 {
		return "", domain.Refuse(domain.CodeNodeHasNoRequests, domain.ErrNotAllowed, nil)
	}

	pieces := make([]string, 0, len(requests))
	for _, request := range requests {
		text, err := s.commands.Curl(ctx, request, secrets)
		if err != nil {
			return "", err
		}
		pieces = append(pieces, "# "+request.Node.Name+"\n"+text)
	}
	return strings.Join(pieces, "\n\n"), nil
}

// The shapes Transfer answers with: what a source turned out to hold, what an import
// wrote, and what a workspace holds for the source that copies from one.

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

// Preview is what a source holds before anything is written, with the counts the dialog shows.
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
	Globals      int           `json:"globals"`
	Skipped      []domain.Skip `json:"skipped,omitempty"`
}

// Summary is what an import wrote, for the tree to draw and the toast to count.
type Summary struct {
	Tree                []domain.Collection `json:"tree"`
	EnvState            *domain.EnvState    `json:"envState,omitempty"`
	Name                string              `json:"name,omitempty"`
	Collections         int                 `json:"collections"`
	Requests            int                 `json:"requests"`
	Environments        int                 `json:"environments"`
	Globals             int                 `json:"globals"`
	CreatedEnvironments []string            `json:"createdEnvironments"`
}

// Workspace is one workspace a copy could be taken from, with the collections it holds at the top level.
type Workspace struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Active      bool                  `json:"active"`
	Collections []WorkspaceCollection `json:"collections"`
}

// WorkspaceCollection is one collection of that workspace with the size of its whole subtree, because the
// picker says how much is being taken.
type WorkspaceCollection struct {
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

// The I/O this feature needs, declared here on the side that needs it: a service names what it
// needs, and only the composition root wires the implementations.

// Collections is the tree as bringing collections in and taking copies out reads it.
type Collections interface {
	// Import writes the collections of a document at the end of a level, or of the top level when
	// the id is empty, and answers with the tree to draw.
	Import(
		ctx context.Context,
		collections []domain.Collection,
		parentID string,
	) ([]domain.Collection, error)
	// ImportCollections is Import that also says which collection each of the file's collections
	// answers to in the environments.
	ImportCollections(
		ctx context.Context,
		collections []domain.Collection,
		parentID string,
	) (domain.Imported, error)
	// Full is a collection — or one request, when the id names one — read whole: what an export
	// writes down.
	Full(ctx context.Context, id string) (domain.Collection, error)
	// FullOf is Full for a named workspace, which the window may not be looking at.
	FullOf(ctx context.Context, workspace, id string) (domain.Collection, error)
	// TreeOf is the tree of a named workspace, as the source list draws it.
	TreeOf(ctx context.Context, workspace string) ([]domain.Collection, error)
	// Requests is every request under a collection, with the authorization and the variables its
	// levels answer.
	Requests(ctx context.Context, collectionID, nodeID string) ([]domain.ResolvedRequest, error)
}

// Environments is the environment list as importing one and exporting the values reads it.
type Environments interface {
	// ImportDrafts writes the environments a document described into the list, by name and without
	// replacing a value already there; a block goes to the collection its index names in targets.
	ImportDrafts(ctx context.Context, drafts []domain.EnvironmentDraft, targets []string,
		readonlySuffix string) (domain.EnvImport, error)
	// ImportVariables adds the names a scope does not hold yet; values already there stay.
	ImportVariables(ctx context.Context, scope domain.EnvScope, vars []domain.Variable) (domain.VarImport, error)
	// State is the whole list, with secret values withheld.
	State(ctx context.Context) (domain.EnvState, error)
	// Reveal is the one way a secret's value leaves the database — here, into a file the person
	// asked to carry it.
	Reveal(ctx context.Context, variableID string) (string, error)
}

// Workspaces is the workspaces of the app, as the source that copies from another one lists them.
type Workspaces interface {
	Snapshot(ctx context.Context) (domain.WorkspaceState, error)
}

// Files reads a file the person picked: the dialog is the window's, and what it answered is read
// here.
type Files interface {
	Read(path string) ([]byte, error)
}

// Remote reads a document from a URL — a specification or a collection somebody published.
type Remote interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Commands is Drafts' reading and writing of commands: a command is a request in
// another notation, and that notation lives there.
type Commands interface {
	// Curl writes one saved request as a curl command line, with its secrets as the caller asks.
	Curl(ctx context.Context, request domain.ResolvedRequest, secrets bool) (string, error)
	// Read reads pasted text as the requests it holds, one per curl command.
	Read(text string) ([]domain.CollectionNode, error)
}
