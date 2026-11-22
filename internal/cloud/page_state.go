package cloud

import (
	"context"
	"schemo/internal/domain"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// List is the workspace's pages as last stored, so it works offline.
func (s *Publications) List(ctx context.Context, workspace string) ([]domain.Publication, error) {
	return s.store.OfWorkspace(ctx, workspace)
}

// Refresh asks the server what it holds and keeps the answer. There is nobody to ask on a device
// with no session, and what such a device holds is still the list a window draws.
func (s *Publications) Refresh(ctx context.Context, workspace string) ([]domain.Publication, error) {
	known, err := s.store.OfWorkspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return known, nil
	}

	answered, err := s.askServerForPages(ctx, caller, workspace, known)
	if err != nil {
		return nil, err
	}
	for _, held := range answered {
		// The answer names the workspace by the account's id, and the row is this device's: kept as it
		// came, it would be a page under a workspace the window never asks about.
		held.WorkspaceID = workspace
		if err := s.store.SavePublication(ctx, held); err != nil {
			return nil, err
		}
	}
	return s.store.OfWorkspace(ctx, workspace)
}

// askServerForPages asks about the collections held here by id, or about the whole workspace when
// none are held.
func (s *Publications) askServerForPages(
	ctx context.Context,
	caller domain.Caller,
	workspace string,
	known []domain.Publication,
) ([]domain.Publication, error) {
	if len(known) > 0 {
		ids := make([]string, 0, len(known))
		for _, held := range known {
			ids = append(ids, held.CollectionID)
		}
		return s.server.Pages(ctx, caller, ids)
	}
	row, err := s.store.Workspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if row.RemoteID == "" {
		return nil, nil
	}
	return s.server.Published(ctx, caller, row.RemoteID)
}

type Choice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
}

type PublishPreview struct {
	Options      domain.PublicationOptions `json:"options"`
	Collection   string                    `json:"collectionId"`
	Publication  *domain.Publication       `json:"publication,omitempty"`
	Behind       bool                      `json:"behind"`
	Changes      []domain.PageItem         `json:"changes,omitempty"`
	Environments []Choice                  `json:"environments"`
	Report       Report                    `json:"report"`
	// InheritedFrom names the folder whose page contains this collection, and is empty for a
	// collection that holds a page of its own.
	InheritedFrom string `json:"inheritedFrom,omitempty"`
}

// Change is what this device knows about a collection's page offline: the row that answers for it
// (its own or a folder's above, named by InheritedFrom), and whether it and its requests are behind.
type Change struct {
	Behind        bool                `json:"behind"`
	Changes       []domain.PageItem   `json:"changes,omitempty"`
	Page          *domain.Publication `json:"page,omitempty"`
	InheritedFrom string              `json:"inheritedFrom,omitempty"`
}

func (s *Publications) Preview(
	ctx context.Context,
	workspace, collectionID, environmentID string,
	options *domain.PublicationOptions,
) (PublishPreview, error) {
	owner, err := s.owningPageOf(ctx, workspace, collectionID)
	if err != nil {
		return PublishPreview{}, err
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return PublishPreview{}, err
	}
	// Saying nothing about the environment means the one the page went out with: the popover opens
	// on the page it is about, and a person who has not touched the field is asking for no change.
	if environmentID == "" {
		environmentID = environmentNamed(state, owner.Page.EnvironmentName)
	}
	snapshot, report, err := s.build(ctx, workspace, owner.Collection, environmentID, "", options)
	if err != nil {
		return PublishPreview{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return PublishPreview{}, err
	}

	out := PublishPreview{
		Options:      optionsOf(snapshot),
		Collection:   owner.Collection,
		Environments: environmentChoices(state, owner.Collection),
		Report:       report,
	}
	if owner.Held {
		out.Publication = &owner.Page
		out.Behind = behind(owner.Page, digest)
		items, err := s.changesInsideFolder(ctx, workspace, collectionID, owner, snapshot)
		if err != nil {
			return PublishPreview{}, err
		}
		out.Changes = items
		out.InheritedFrom = owner.inheritedFrom()
	}
	return out, nil
}

// Changes compares offline, against the environment the page was published with rather than the
// current one.
func (s *Publications) Changes(
	ctx context.Context,
	workspace, collectionID string,
) (Change, error) {
	owner, err := s.owningPageOf(ctx, workspace, collectionID)
	if err != nil {
		return Change{}, err
	}
	if !owner.Held {
		return Change{}, nil
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return Change{}, err
	}
	snapshot, _, err := s.Build(ctx, workspace, owner.Collection,
		environmentNamed(state, owner.Page.EnvironmentName), "")
	if err != nil {
		return Change{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return Change{}, err
	}
	items, err := s.changesInsideFolder(ctx, workspace, collectionID, owner, snapshot)
	if err != nil {
		return Change{}, err
	}
	return Change{
		Behind:        behind(owner.Page, digest),
		Changes:       items,
		Page:          &owner.Page,
		InheritedFrom: owner.inheritedFrom(),
	}, nil
}

// behind is whether a page differs from the document built now. A page that never recorded its hash
// is not behind: there is nothing to differ from.
func behind(page domain.Publication, digest string) bool {
	return page.ContentHash != "" && page.ContentHash != digest
}

// optionsOf is what a document was built with, as the popover draws it.
func optionsOf(snapshot snapshotv1.Snapshot) domain.PublicationOptions {
	return domain.PublicationOptions{
		Docs:     snapshot.Options.Docs,
		Examples: snapshot.Options.Examples,
	}
}

// changesInsideFolder compares the published outline with the current document, limited to a folder's
// requests; the page's own folder is not limited.
func (s *Publications) changesInsideFolder(
	ctx context.Context,
	workspace, collectionID string,
	owner owningPage,
	snapshot snapshotv1.Snapshot,
) ([]domain.PageItem, error) {
	inside, err := s.pageIDsInside(ctx, workspace, collectionID, owner.Collection)
	if err != nil {
		return nil, err
	}
	now, err := outlineOf(snapshot)
	if err != nil {
		return nil, err
	}
	items, _ := moved(within(owner.Page.Outline, inside), within(now, inside))
	return items, nil
}

// pageIDsInside is the page ids of a folder's requests, nested ones included; nil for the page's own folder.
func (s *Publications) pageIDsInside(
	ctx context.Context,
	workspace, collectionID, pageID string,
) (map[string]bool, error) {
	if collectionID == pageID {
		return nil, nil
	}
	tree, err := s.collections.FullOf(ctx, workspace, collectionID)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{}
	eachItem(tree, func(id string) { held[PageID(pageID, id)] = true })
	return held, nil
}

// environmentChoices is the environments a page's addresses may be read against.
func environmentChoices(state domain.EnvState, collection string) []Choice {
	out := make([]Choice, 0, len(state.Environments))
	for _, environment := range state.Environments {
		out = append(out, Choice{
			ID:      environment.ID,
			Name:    environment.Name,
			BaseURL: baseOf(environment, collection),
		})
	}
	return out
}
