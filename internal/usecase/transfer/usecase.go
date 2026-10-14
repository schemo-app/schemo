// Package transfer owns what comes into this app from other tools and what goes out to them: a
// file, a URL, a paste of commands, another space, and the exports five formats are written in. It
// is the one feature that knows both the tree and the environments, which is why the readers
// themselves stay pure packages beside the formats they know.
package transfer

import (
	"context"
	"strings"
	"sync"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/usecase/publication"
)

// pendingTTL is how long what a preview read waits to be written. A dialog is a person deciding;
// a token older than this belongs to a dialog that was closed without deciding.
const pendingTTL = 10 * time.Minute

type UseCase struct {
	collections  Collections
	environments Environments
	workspaces   Workspaces
	files        Files
	remote       Remote
	commands     Commands
	ids          platform.IDGen

	mu sync.Mutex
	// One read waits at a time: the dialog has one file card open, and a second read replaces the
	// first rather than holding everything ever previewed for ten minutes.
	waiting *waiting
}

// waiting is a document that was read and not yet written, under the token the window holds.
type waiting struct {
	token    string
	document domain.Document
	at       time.Time
}

func NewUseCase(
	collections Collections,
	environments Environments,
	workspaces Workspaces,
	files Files,
	remote Remote,
	commands Commands,
	ids platform.IDGen,
) *UseCase {
	return &UseCase{
		collections:  collections,
		environments: environments,
		workspaces:   workspaces,
		files:        files,
		remote:       remote,
		commands:     commands,
		ids:          ids,
	}
}

// PreviewFile reads a file the person picked and answers what it holds, without writing anything:
// the dialog shows the counts before the person commits.
func (u *UseCase) PreviewFile(ctx context.Context, format Format, path string) (Preview, error) {
	data, err := u.files.Read(path)
	if err != nil {
		return Preview{}, err
	}
	return u.preview(format, data)
}

// PreviewText reads a pasted text — a collection somebody sent, or commands copied from a terminal.
func (u *UseCase) PreviewText(ctx context.Context, format Format, text string) (Preview, error) {
	return u.preview(format, []byte(text))
}

// PreviewUrl reads a document from a URL — a specification or a collection somebody published. A
// published page is asked for its document rather than for the address it was given: what a person
// has is where the page is read, and the bytes behind it are one path further in.
func (u *UseCase) PreviewUrl(ctx context.Context, format Format, url string) (Preview, error) {
	address := url
	if format == FormatPage {
		held, err := publication.DocumentAddress(url)
		if err != nil {
			return Preview{}, err
		}
		address = held
	}
	data, err := u.remote.Fetch(ctx, address)
	if err != nil {
		return Preview{}, err
	}
	return u.preview(format, data)
}

// preview puts what was read where Apply will find it and answers the counters the dialog shows.
func (u *UseCase) preview(format Format, data []byte) (Preview, error) {
	document, err := u.read(format, data)
	if err != nil {
		return Preview{}, err
	}

	token := u.ids()
	u.mu.Lock()
	u.waiting = &waiting{token: token, document: document, at: time.Now()}
	u.mu.Unlock()

	requests, folders := countDocument(document)
	out := Preview{
		Token:        token,
		Name:         firstName(document),
		Size:         len(data),
		Collections:  len(document.Collections),
		Requests:     requests,
		Folders:      folders,
		Environments: len(document.Environments),
		Skipped:      document.Skipped,
	}
	return out, nil
}

// Apply writes what a preview read, at the end of a level or of the top level, and answers with the
// tree and the environment list as they are now. The name fills in where the document gave none —
// a paste of commands has no name of its own — and the token is good once: what was read is
// written and forgotten.
func (u *UseCase) Apply(
	ctx context.Context,
	token string,
	parentID string,
	name string,
) (Summary, error) {
	document, err := u.take(token)
	if err != nil {
		return Summary{}, err
	}

	for index := range document.Collections {
		if strings.TrimSpace(document.Collections[index].Name) == "" {
			document.Collections[index].Name = name
		}
	}

	summary := Summary{Collections: len(document.Collections)}
	if len(document.Collections) > 0 {
		tree, err := u.collections.Import(ctx, document.Collections, parentID)
		if err != nil {
			return Summary{}, err
		}
		summary.Tree = tree
		requests, _ := countDocument(domain.Document{Collections: document.Collections})
		summary.Requests = requests
	}
	for _, draft := range document.Environments {
		state, err := u.environments.Import(ctx, draft)
		if err != nil {
			return Summary{}, err
		}
		summary.EnvState = &state
		summary.Environments++
	}
	if len(document.Collections) > 0 && len(document.Collections[0].Name) > 0 {
		summary.Name = document.Collections[0].Name
	}
	if summary.Collections == 0 && summary.Environments == 0 {
		return Summary{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return summary, nil
}

// take is the document a token names, once: a second Apply with the same token is a window that
// lost track, and answering it twice would write the file twice.
func (u *UseCase) take(token string) (domain.Document, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	read := u.waiting
	if read == nil || read.token != token || time.Since(read.at) > pendingTTL {
		return domain.Document{}, domain.Refuse(domain.CodeImportExpired, domain.ErrNotFound, nil)
	}
	u.waiting = nil
	return read.document, nil
}

// firstName is what the document calls its first collection, or its first environment where it
// holds none: the dialog shows it as the name the new row will take.
func firstName(document domain.Document) string {
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

// Spaces lists every space with the collections a copy could take from its top level: the source
// that copies from another space draws this, and the space that is open is marked rather than
// left out — the window is the one that knows not to offer it.
func (u *UseCase) Spaces(ctx context.Context) ([]Space, error) {
	state, err := u.workspaces.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Space, 0, len(state.Workspaces))
	for _, workspace := range state.Workspaces {
		space := Space{ID: workspace.ID, Name: workspace.Name, Active: workspace.ID == state.ActiveID}
		tree, err := u.collections.TreeOf(ctx, workspace.ID)
		if err != nil {
			return nil, err
		}
		for _, collection := range tree {
			requests, folders := countCollection(collection)
			space.Collections = append(space.Collections, SpaceCollection{
				ID:       collection.ID,
				Name:     collection.Name,
				Requests: requests,
				Folders:  folders,
			})
		}
		out = append(out, space)
	}
	return out, nil
}

// ImportFromSpace copies collections of another space into the active one, read whole there and
// written here: the same walk an export takes, written where the person asked for it.
func (u *UseCase) ImportFromSpace(
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
		collection, err := u.collections.FullOf(ctx, workspace, id)
		if err != nil {
			return Summary{}, err
		}
		collections = append(collections, collection)
	}

	tree, err := u.collections.Import(ctx, collections, parentID)
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

// Curl writes every request of a level out as a curl command, one per command, each under a `#`
// line naming it — which is what the same paste reads back in. Secrets are as the caller asks: a
// copy meant to be handed on names them instead of carrying them.
func (u *UseCase) Curl(ctx context.Context, id string, secrets bool) (string, error) {
	requests, err := u.collections.RequestsOf(ctx, id)
	if err != nil {
		return "", err
	}
	if len(requests) == 0 {
		return "", domain.Refuse(domain.CodeNodeHasNoRequests, domain.ErrNotAllowed, nil)
	}

	pieces := make([]string, 0, len(requests))
	for _, request := range requests {
		text, err := u.commands.Curl(ctx, request, secrets)
		if err != nil {
			return "", err
		}
		pieces = append(pieces, "# "+request.Node.Name+"\n"+text)
	}
	return strings.Join(pieces, "\n\n"), nil
}
