package publication

import (
	"context"
	"strconv"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

type UseCase struct {
	store        Store
	collections  Collections
	environments Environments
	server       Server
	caller       Session
	passwords    Passwords
	now          func() time.Time
}

func NewUseCase(
	store Store,
	collections Collections,
	environments Environments,
	server Server,
	caller Session,
	passwords Passwords,
) *UseCase {
	return &UseCase{
		store:        store,
		collections:  collections,
		environments: environments,
		server:       server,
		caller:       caller,
		passwords:    passwords,
		now:          time.Now,
	}
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

// host is the page a collection belongs to. A page is a whole tree, so a folder inside a published
// one answers at that page rather than at its own: Collection is the folder the page is of, Name is
// what it is called, Inside says whether it sits above the collection, Held says if one answers.
type host struct {
	Collection string
	Name       string
	Inside     bool
	Page       domain.Publication
	Held       bool
}

// pageOf asks which page answers for a collection: its own when the collection holds a runnable
// one, and otherwise the nearest folder above it that does. A folder inside a published one is
// published by it and cannot hold a second page of its own, which the two sides have to agree on.
func (u *UseCase) pageOf(ctx context.Context, workspace, collectionID string) (host, error) {
	chain, err := u.collections.PathOf(ctx, workspace, collectionID)
	if err != nil {
		return host{}, err
	}
	for at, level := range chain {
		held, known, err := u.store.Publication(ctx, level.ID)
		if err != nil {
			return host{}, err
		}
		if known && held.Runnable() {
			return host{
				Collection: level.ID,
				Name:       level.Name,
				Inside:     at > 0,
				Page:       held,
				Held:       true,
			}, nil
		}
	}
	return host{Collection: collectionID}, nil
}

// movedIn is what moved at a page inside one folder of it: the outline the last publish put up,
// read against the document built now, both held to the folder's own requests. A folder the page
// itself is of has no filter, and its list is the page's whole one.
func (u *UseCase) movedIn(
	ctx context.Context,
	workspace, collectionID string,
	owner host,
	snapshot snapshotv1.Snapshot,
) ([]domain.PageItem, error) {
	inside, err := u.inside(ctx, workspace, collectionID, owner.Collection)
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

// inside is the page's own names for the requests the asked folder holds, every folder under it
// included: what a folder's popover draws, and nothing about the folders beside it. The folder the
// page itself is of answers with no set — every request of the document is already its own.
func (u *UseCase) inside(
	ctx context.Context,
	workspace, collectionID, pageID string,
) (map[string]bool, error) {
	if collectionID == pageID {
		return nil, nil
	}
	tree, err := u.collections.FullOf(ctx, workspace, collectionID)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{}
	eachItem(tree, func(id string) { held[PageID(pageID, id)] = true })
	return held, nil
}

func (u *UseCase) Preview(
	ctx context.Context,
	workspace, collectionID, environmentID string,
	options *domain.PublicationOptions,
) (PublishPreview, error) {
	owner, err := u.pageOf(ctx, workspace, collectionID)
	if err != nil {
		return PublishPreview{}, err
	}
	state, err := u.environments.Snapshot(ctx)
	if err != nil {
		return PublishPreview{}, err
	}
	// Saying nothing about the environment means the one the page went out with: the popover opens
	// on the page it is about, and a person who has not touched the field is asking for no change.
	if environmentID == "" {
		environmentID = environmentNamed(state, owner.Page.EnvironmentName)
	}
	snapshot, report, err := u.build(ctx, workspace, owner.Collection, environmentID, "", options)
	if err != nil {
		return PublishPreview{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return PublishPreview{}, err
	}

	out := PublishPreview{
		Options:      domain.PublicationOptions{Docs: snapshot.Options.Docs, Examples: snapshot.Options.Examples},
		Collection:   owner.Collection,
		Environments: choices(state),
		Report:       report,
	}
	if owner.Held {
		out.Publication = &owner.Page
		out.Behind = owner.Page.ContentHash != "" && owner.Page.ContentHash != digest
		items, err := u.movedIn(ctx, workspace, collectionID, owner, snapshot)
		if err != nil {
			return PublishPreview{}, err
		}
		out.Changes = items
		if owner.Inside {
			out.InheritedFrom = owner.Name
		}
	}
	return out, nil
}

func (u *UseCase) Publish(
	ctx context.Context,
	workspace, collectionID, environmentID, visibility, locale string,
	password string,
	confirmed bool,
	options *domain.PublicationOptions,
) (domain.Publication, error) {
	switch visibility {
	case domain.VisibilityPublic, domain.VisibilityUnlisted:
	case domain.VisibilityPassword:
		if password != "" {
			if err := passwordFits(password); err != nil {
				return domain.Publication{}, err
			}
		}
	default:
		return domain.Publication{}, domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed,
			domain.Args{"visibility": visibility})
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return domain.Publication{}, err
	}
	space, err := u.space(ctx, workspace)
	if err != nil {
		return domain.Publication{}, err
	}
	// A folder inside a published one has no page of its own to put up: what a publish from it carries
	// is the page that holds it, which is the only way the folder's own requests can reach a reader
	// without a second document claiming them as well.
	owner, err := u.pageOf(ctx, workspace, collectionID)
	if err != nil {
		return domain.Publication{}, err
	}
	if visibility == domain.VisibilityPassword && password == "" && (!owner.Held || owner.Page.Visibility != domain.VisibilityPassword) {
		return domain.Publication{}, passwordFits(password)
	}
	snapshot, _, err := u.build(ctx, workspace, owner.Collection, environmentID, locale, options)
	if err != nil {
		return domain.Publication{}, err
	}
	if !owner.Held {
		requests := 0
		for _, group := range snapshot.Collection.Groups {
			requests += len(group.Endpoints)
		}
		snapshot.Changes = snapshotChanges(appended(nil, []domain.PageItem{{
			Sign: signAdded, Fields: []string{"initial:" + strconv.Itoa(requests)},
		}}, u.now(), 1))
	}
	packed, digest, err := Encode(snapshot)
	if err != nil {
		return domain.Publication{}, err
	}

	held, err := u.server.Publish(ctx, caller, domain.PublicationDraft{
		Collection:  owner.Collection,
		Workspace:   space.RemoteID,
		Visibility:  visibility,
		Document:    packed,
		ContentHash: digest,
		Confirmed:   confirmed,
		Password:    password,
	})
	if err != nil {
		return domain.Publication{}, err
	}

	if held.UpdatedAt == 0 {
		held.UpdatedAt = u.now().UnixMilli()
	}
	held.WorkspaceID = workspace
	if err := u.store.SavePublication(ctx, held); err != nil {
		return domain.Publication{}, err
	}

	// What this publish put up and what it says has changed are this device's own note, written after
	// the row exists so that a first publish keeps them as well as a later one.
	outline, err := outlineOf(snapshot)
	if err != nil {
		return domain.Publication{}, err
	}
	changes := storedChanges(snapshot.Changes)
	chosen := domain.PublicationOptions{Docs: snapshot.Options.Docs, Examples: snapshot.Options.Examples}
	if err := u.store.SavePublished(ctx, owner.Collection, outline, changes, chosen); err != nil {
		return domain.Publication{}, err
	}
	return held, nil
}

// passwordFits refuses a password the server would refuse, said here so a window answers before a
// publish goes out rather than after. It is a length and nothing else: what makes a password good
// is not a question this device answers, and the page is served over the web's own channel.
func passwordFits(password string) error {
	if len(password) >= domain.PasswordLeast && len(password) <= domain.PasswordMost {
		return nil
	}
	return domain.Refuse(domain.CodePasswordInvalid, domain.ErrNotAllowed, domain.Args{
		"min": strconv.Itoa(domain.PasswordLeast),
		"max": strconv.Itoa(domain.PasswordMost),
	})
}

func (u *UseCase) space(ctx context.Context, workspace string) (domain.Workspace, error) {
	held, err := u.store.Workspace(ctx, workspace)
	if err != nil {
		return domain.Workspace{}, err
	}
	if held.RemoteID == "" {
		return domain.Workspace{}, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	return held, nil
}

func (u *UseCase) Unpublish(ctx context.Context, collectionID string) error {
	held, known, err := u.store.Publication(ctx, collectionID)
	if err != nil {
		return err
	}
	if !known || held.ID == "" {
		return domain.Refuse(domain.CodeNotFound, domain.ErrNotFound, nil)
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return err
	}
	held.Status = domain.PublicationRevoked
	if err := u.server.Unpublish(ctx, caller, held.ID); err != nil {
		if domain.CodeOf(err) != domain.CodeServerUnreachable {
			return err
		}
		held.PendingUnpublish = true
		return u.store.SavePublication(ctx, held)
	}
	held.PendingUnpublish = false
	return u.store.SavePublication(ctx, held)
}

// Replay plays back the takedowns that never reached the server. It is the one piece of this
// feature that runs by itself, and it runs when the window asks rather than on a timer: a page
// taken down is taken down the next time this device has a session and the network, which is what
// the person asked for.
func (u *UseCase) Replay(ctx context.Context) error {
	waiting, err := u.store.AwaitingUnpublish(ctx)
	if err != nil || len(waiting) == 0 {
		return err
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		// Not signed in: the takedown is waiting for a session and not for a person to come back.
		return nil
	}
	for _, held := range waiting {
		if err := u.server.Unpublish(ctx, caller, held.ID); err != nil {
			if domain.CodeOf(err) == domain.CodeServerUnreachable {
				return nil
			}
			return err
		}
		held.PendingUnpublish = false
		if err := u.store.SavePublication(ctx, held); err != nil {
			return err
		}
	}
	return nil
}

// List is the pages of a space as this device knows them, without asking anybody: a device that is
// offline still draws the list it last saw.
func (u *UseCase) List(ctx context.Context, workspace string) ([]domain.Publication, error) {
	return u.store.OfWorkspace(ctx, workspace)
}

// Refresh asks the server what it holds and keeps the answer. There is nobody to ask on a device
// with no session, and what such a device holds is still the list a window draws.
func (u *UseCase) Refresh(ctx context.Context, workspace string) ([]domain.Publication, error) {
	known, err := u.store.OfWorkspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return known, nil
	}

	answered, err := u.ask(ctx, caller, workspace, known)
	if err != nil {
		return nil, err
	}
	for _, held := range answered {
		// The answer names the space by the account's id, and the row is this device's: kept as it
		// came, it would be a page under a space the window never asks about.
		held.WorkspaceID = workspace
		if err := u.store.SavePublication(ctx, held); err != nil {
			return nil, err
		}
	}
	return u.store.OfWorkspace(ctx, workspace)
}

// ask is the question this device may put about a space. The collections it holds here are asked
// about by id, because a collection nobody here published is not one to invent a question about; a
// device that holds none asks the space itself, whose pages a second device has as well.
func (u *UseCase) ask(
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
		return u.server.Pages(ctx, caller, ids)
	}
	space, err := u.store.Workspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if space.RemoteID == "" {
		return nil, nil
	}
	return u.server.Published(ctx, caller, space.RemoteID)
}

// Change is what this device can say about a collection's page without asking the server: the row
// that answers for it (its own, or one of a folder above it — a page is a whole tree), whether that
// page is behind, and which of this collection's requests are. InheritedFrom names that folder.
type Change struct {
	Behind        bool                `json:"behind"`
	Changes       []domain.PageItem   `json:"changes,omitempty"`
	Page          *domain.Publication `json:"page,omitempty"`
	InheritedFrom string              `json:"inheritedFrom,omitempty"`
}

// Changes is the local half of the question, and the reason the popover can open without a network.
// It builds against the environment the page went out with rather than the one this device works in
// now: a page published from Staging is not behind because the app has since moved to Local.
func (u *UseCase) Changes(
	ctx context.Context,
	workspace, collectionID string,
) (Change, error) {
	owner, err := u.pageOf(ctx, workspace, collectionID)
	if err != nil {
		return Change{}, err
	}
	if !owner.Held {
		return Change{}, nil
	}
	state, err := u.environments.Snapshot(ctx)
	if err != nil {
		return Change{}, err
	}
	snapshot, _, err := u.Build(ctx, workspace, owner.Collection,
		environmentNamed(state, owner.Page.EnvironmentName), "")
	if err != nil {
		return Change{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return Change{}, err
	}
	items, err := u.movedIn(ctx, workspace, collectionID, owner, snapshot)
	if err != nil {
		return Change{}, err
	}
	out := Change{
		Behind:  owner.Page.ContentHash != "" && owner.Page.ContentHash != digest,
		Changes: items,
		Page:    &owner.Page,
	}
	if owner.Inside {
		out.InheritedFrom = owner.Name
	}
	return out, nil
}

// choices is the environments a page's addresses may be read against.
func choices(state domain.EnvState) []Choice {
	out := make([]Choice, 0, len(state.Environments))
	for _, environment := range state.Environments {
		out = append(out, Choice{
			ID:      environment.ID,
			Name:    environment.Name,
			BaseURL: baseOf(environment),
		})
	}
	return out
}
