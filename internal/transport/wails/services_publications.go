package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/publication"
)

// PublicationsService is a collection put at an address a stranger may read, as a window sees it.
//
// The page itself belongs to the server, and nothing here fetches it: what this service answers is
// what the page would carry, what it would leave out, and what this device knows about what is
// already there — which is answered without a network, so the popover opens offline.
type PublicationsService struct {
	publications *publication.UseCase
}

func NewPublicationsService(uc *publication.UseCase) *PublicationsService {
	return &PublicationsService{publications: uc}
}

// PublishChoice is what the popover is holding when the button is pressed. It is one value rather
// than seven arguments because the window sends back exactly what Preview answered, and a call of
// seven strings in a row is one transposition away from publishing the wrong environment.
type PublishChoice struct {
	Options       *domain.PublicationOptions `json:"options"`
	Workspace     string                     `json:"workspace"`
	Collection    string                     `json:"collection"`
	EnvironmentID string                     `json:"environmentId"`
	Visibility    string                     `json:"visibility"`
	Locale        string                     `json:"locale"`
	Confirmed     bool                       `json:"confirmed"`
	// Password is held here for as long as a publish takes and is written down nowhere: it is what a
	// locked page answers to, and the row keeps a hash of it rather than the text.
	Password string `json:"password"`
}

// Preview is what publishing this collection would do, and what is already there.
func (s *PublicationsService) Preview(
	ctx context.Context,
	workspace string,
	collection string,
	environmentID string,
	options *domain.PublicationOptions,
) (publication.PublishPreview, error) {
	return s.publications.Preview(ctx, workspace, collection, environmentID, options)
}

// Publish puts the page at an address, or replaces what that address already carries.
func (s *PublicationsService) Publish(
	ctx context.Context,
	choice PublishChoice,
) (domain.Publication, error) {
	return s.publications.Publish(ctx, choice.Workspace, choice.Collection, choice.EnvironmentID,
		choice.Visibility, choice.Locale, choice.Password, choice.Confirmed, choice.Options)
}

// Unpublish takes a page down. A takedown the network did not carry is kept and played back later,
// so this answers successfully rather than making the person try again in better weather.
func (s *PublicationsService) Unpublish(ctx context.Context, collection string) error {
	return s.publications.Unpublish(ctx, collection)
}

// List is the pages of a space as this device knows them, without asking anybody.
func (s *PublicationsService) List(
	ctx context.Context,
	workspace string,
) ([]domain.Publication, error) {
	return s.publications.List(ctx, workspace)
}

// Refresh asks the server and answers what it said, while keeping what this device holds for
// everything the answer did not cover.
func (s *PublicationsService) Refresh(
	ctx context.Context,
	workspace string,
) ([]domain.Publication, error) {
	return s.publications.Refresh(ctx, workspace)
}

// Changes says whether the page is behind what this device holds, said without asking the server.
func (s *PublicationsService) Changes(
	ctx context.Context,
	workspace string,
	collection string,
) (publication.Change, error) {
	return s.publications.Changes(ctx, workspace, collection)
}

func (s *PublicationsService) Details(ctx context.Context, collection string) (domain.PublicationDetails, error) {
	return s.publications.Details(ctx, collection)
}

func (s *PublicationsService) Restore(ctx context.Context, workspace, collection string, revision int) (domain.Publication, error) {
	return s.publications.Restore(ctx, workspace, collection, revision)
}
