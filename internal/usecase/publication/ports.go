package publication

import (
	"context"

	"schemo/internal/domain"
)

type Store interface {
	Workspace(ctx context.Context, id string) (domain.Workspace, error)
	Publication(ctx context.Context, collectionID string) (domain.Publication, bool, error)
	OfWorkspace(ctx context.Context, workspace string) ([]domain.Publication, error)
	SavePublication(ctx context.Context, publication domain.Publication) error
	// SavePublished is what this device alone keeps about a page: the shape the last publish put up
	// and what has moved at it since. It is not part of SavePublication, which writes the server's
	// answers over the row and is told about neither.
	SavePublished(ctx context.Context, collectionID string, outline []domain.OutlineItem,
		changes []domain.PageRelease, options domain.PublicationOptions) error
	ForgetPublication(ctx context.Context, collectionID string) error
	AwaitingUnpublish(ctx context.Context) ([]domain.Publication, error)
}

type Collections interface {
	FullOf(ctx context.Context, workspace, id string) (domain.Collection, error)
	// PathOf is the collections from the asked one up to the root of its space, the asked one first:
	// a page is a whole tree, so the page a folder sits inside of is found by walking up from it.
	PathOf(ctx context.Context, workspace, id string) ([]domain.Collection, error)
}

type Environments interface {
	Snapshot(ctx context.Context) (domain.EnvState, error)
}

type Server interface {
	Details(ctx context.Context, caller domain.Caller, publicationID string) (domain.PublicationDetails, error)
	Restore(ctx context.Context, caller domain.Caller, publicationID string, revision int) (domain.Publication, []byte, error)
	Publish(
		ctx context.Context,
		caller domain.Caller,
		draft domain.PublicationDraft,
	) (domain.Publication, error)
	Unpublish(ctx context.Context, caller domain.Caller, publicationID string) error
	Pages(
		ctx context.Context,
		caller domain.Caller,
		collectionIDs []string,
	) ([]domain.Publication, error)
	Published(
		ctx context.Context,
		caller domain.Caller,
		workspace string,
	) ([]domain.Publication, error)
}

type Session interface {
	Caller(ctx context.Context) (domain.Caller, error)
}
