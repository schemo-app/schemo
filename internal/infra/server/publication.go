package server

// The four things the server answers about published pages.
//
// Publishing is an ordinary write into the space the collection lives in: it leaves over the same
// channel and the same token as every other write, and the doors it passes are the server's. What
// is anonymous is the page — and the page is drawn on the server, so nothing here fetches it.

import (
	"context"

	"google.golang.org/grpc"

	"github.com/schemo-app/proto/go/publication/v1"

	"schemo/internal/domain"
)

// Publish puts a collection's document at an address, or replaces what that address carries. What
// travels is the whole document rather than the collection: the server keeps the bytes, and the
// digest beside them is what lets a device tell whether a page is behind what it holds.
func (c *Client) Publish(
	ctx context.Context,
	caller domain.Caller,
	draft domain.PublicationDraft,
) (domain.Publication, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publication.PublishResponse, error) {
			return publication.NewPublicationServiceClient(conn).Publish(ctx,
				publication.PublishRequest_builder{
					CollectionId: draft.Collection,
					WorkspaceId:  draft.Workspace,
					Visibility:   visibilityOf(draft.Visibility),
					Document:     draft.Document,
					ContentHash:  draft.ContentHash,
					Confirmed:    draft.Confirmed,
					Password:     draft.Password,
				}.Build())
		})
	if err != nil {
		return domain.Publication{}, err
	}
	return publicationOf(answer.GetPublication()), nil
}

// Unpublish takes a page down. The address stays taken, which is the server's rule and a good one:
// a link that was handed out once must never come to mean something else.
func (c *Client) Unpublish(
	ctx context.Context,
	caller domain.Caller,
	publicationID string,
) error {
	_, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publication.UnpublishResponse, error) {
			return publication.NewPublicationServiceClient(conn).Unpublish(ctx,
				publication.UnpublishRequest_builder{PublicationId: publicationID}.Build())
		})
	return err
}

// Pages is what the server holds for the named collections. One it has nothing for is absent from
// the answer rather than refused: a collection nobody published has no page, and that is the whole
// of what there is to say about it.
func (c *Client) Pages(
	ctx context.Context,
	caller domain.Caller,
	collectionIDs []string,
) ([]domain.Publication, error) {
	if len(collectionIDs) == 0 {
		return nil, nil
	}
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publication.GetPublicationsResponse, error) {
			return publication.NewPublicationServiceClient(conn).GetPublications(ctx,
				publication.GetPublicationsRequest_builder{CollectionIds: collectionIDs}.Build())
		})
	if err != nil {
		return nil, err
	}
	return publicationsOf(answer.GetPublications()), nil
}

// Published is the pages of one space, newest first.
func (c *Client) Published(
	ctx context.Context,
	caller domain.Caller,
	workspace string,
) ([]domain.Publication, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publication.ListPublicationsResponse, error) {
			return publication.NewPublicationServiceClient(conn).ListPublications(ctx,
				publication.ListPublicationsRequest_builder{WorkspaceId: workspace}.Build())
		})
	if err != nil {
		return nil, err
	}
	return publicationsOf(answer.GetPublications()), nil
}

// publicationOf is one page as the wire carries it, in the shape this app keeps.
func publicationOf(held *publication.Publication) domain.Publication {
	if held == nil {
		return domain.Publication{}
	}
	out := domain.Publication{
		CollectionID:    held.GetCollectionId(),
		WorkspaceID:     held.GetWorkspaceId(),
		ID:              held.GetId(),
		Slug:            held.GetSlug(),
		URL:             held.GetPublicUrl(),
		Visibility:      visibilityName(held.GetVisibility()),
		Status:          statusName(held.GetStatus()),
		Revision:        int(held.GetRevision()),
		ContentHash:     held.GetContentHash(),
		EnvironmentName: held.GetEnvironmentName(),
		CanManage:       held.GetCanManage(),
	}
	if at := held.GetUpdatedAt(); at != nil {
		out.UpdatedAt = at.AsTime().UnixMilli()
	}
	return out
}

// publicationsOf is a list of pages, in the same shape.
func publicationsOf(held []*publication.Publication) []domain.Publication {
	out := make([]domain.Publication, 0, len(held))
	for _, one := range held {
		out = append(out, publicationOf(one))
	}
	return out
}

// visibilityName is who a page is for, in this app's words. A visibility this build has never heard
// of is answered as none rather than as the nearest one: the server is free to add one, and a
// window that guessed would offer to publish a page it does not understand.
func visibilityName(held publication.Visibility) string {
	switch held {
	case publication.Visibility_VISIBILITY_PUBLIC:
		return domain.VisibilityPublic
	case publication.Visibility_VISIBILITY_UNLISTED:
		return domain.VisibilityUnlisted
	case publication.Visibility_VISIBILITY_PASSWORD:
		return domain.VisibilityPassword
	}
	return ""
}

// visibilityOf is that word on the way back to the server.
func visibilityOf(name string) publication.Visibility {
	switch name {
	case domain.VisibilityPublic:
		return publication.Visibility_VISIBILITY_PUBLIC
	case domain.VisibilityUnlisted:
		return publication.Visibility_VISIBILITY_UNLISTED
	case domain.VisibilityPassword:
		return publication.Visibility_VISIBILITY_PASSWORD
	}
	return publication.Visibility_VISIBILITY_UNSPECIFIED
}

// statusName is whether a page answers at all.
func statusName(held publication.PublicationStatus) string {
	switch held {
	case publication.PublicationStatus_PUBLICATION_STATUS_ACTIVE:
		return domain.PublicationActive
	case publication.PublicationStatus_PUBLICATION_STATUS_REVOKED:
		return domain.PublicationRevoked
	}
	return ""
}
