package cloud

import (
	"context"
	"errors"
	"schemo/internal/db"
	"schemo/internal/domain"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	publicationv1 "github.com/schemo-app/proto/go/publication/v1"
	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"google.golang.org/grpc"
)

// Publications is a collection put at an address a stranger may read, as a window sees it. The page
// belongs to the server: this service answers what the page would carry, what it would leave out,
// and what this device knows about what is already there, without a network, so the popover opens
// offline.
type Publications struct {
	store        *db.Store
	collections  Collections
	environments Environments
	server       PublicationServer
	caller       publicationSession
	passwords    Passwords
	now          func() time.Time
}

func NewPublications(
	store *db.Store,
	collections Collections,
	environments Environments,
	server PublicationServer,
	caller publicationSession,
	passwords Passwords,
) *Publications {
	return &Publications{
		store:        store,
		collections:  collections,
		environments: environments,
		server:       server,
		caller:       caller,
		passwords:    passwords,
		now:          time.Now,
	}
}

// owningPage is the page a collection belongs to. A page is a whole tree, so a folder inside a
// published one answers at that page rather than at its own: Collection is the folder the page is
// of, Name is what it is called, Inside says whether it sits above the collection, and Held says if
// one answers.
type owningPage struct {
	Collection string
	Name       string
	Inside     bool
	Page       domain.Publication
	Held       bool
}

// inheritedFrom names the folder whose page contains the collection asked about, and is empty for a
// collection that holds a page of its own.
func (h owningPage) inheritedFrom() string {
	if h.Inside {
		return h.Name
	}
	return ""
}

// keepsPassword is whether the page already holds a password of its own, so that publishing it again
// without a new one leaves that one in place.
func (h owningPage) keepsPassword() bool {
	return h.Held && h.Page.Visibility == domain.VisibilityPassword
}

// owningPageOf asks which page answers for a collection: its own when the collection holds a
// runnable one, and otherwise the nearest folder above it that does. A folder inside a published
// one is published by it and cannot hold a second page of its own, which the two sides have to
// agree on.
func (s *Publications) owningPageOf(ctx context.Context, workspace, collectionID string) (owningPage, error) {
	chain, err := s.collections.PathOf(ctx, workspace, collectionID)
	if err != nil {
		return owningPage{}, err
	}
	for at, level := range chain {
		held, known, err := s.store.Publication(ctx, level.ID)
		if err != nil {
			return owningPage{}, err
		}
		if known && held.Runnable() {
			return owningPage{
				Collection: level.ID,
				Name:       level.Name,
				Inside:     at > 0,
				Page:       held,
				Held:       true,
			}, nil
		}
	}
	return owningPage{Collection: collectionID}, nil
}

func (s *Publications) remoteWorkspace(ctx context.Context, workspace string) (domain.Workspace, error) {
	held, err := s.store.Workspace(ctx, workspace)
	if err != nil {
		return domain.Workspace{}, err
	}
	if held.RemoteID == "" {
		return domain.Workspace{}, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	return held, nil
}

// Collections is the tree a page is read from.
type Collections interface {
	FullOf(ctx context.Context, workspace, id string) (domain.Collection, error)
	// PathOf is the collections from the asked one up to the root of its workspace, the asked one first:
	// a page is a whole tree, so the page a folder sits inside of is found by walking up from it.
	PathOf(ctx context.Context, workspace, id string) ([]domain.Collection, error)
}

// Environments is the environment list as the sheet answers it, already masked.
type Environments interface {
	State(ctx context.Context) (domain.EnvState, error)
}

// PublicationServer is the publication API of the account.
type PublicationServer interface {
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

// publicationSession is who this device is signed in as.
type publicationSession interface {
	Caller(ctx context.Context) (domain.Caller, error)
	Me(ctx context.Context) (domain.Account, error)
}

// Passwords stores local publication passwords in the operating system credential store.
type Passwords interface {
	PublicationPassword(account domain.Account, id string) (string, error)
	SetPublicationPassword(account domain.Account, id, password string) error
	DeletePublicationPassword(account domain.Account, id string) error
}

// PublishRequest is what the popover is holding when the button is pressed. It is one value rather
// than nine arguments because the window sends back exactly what Preview returned, and a call of
// strings in a row is one transposition away from publishing the wrong environment.
type PublishRequest struct {
	Options       *domain.PublicationOptions `json:"options"`
	Workspace     string                     `json:"workspace"`
	Collection    string                     `json:"collection"`
	EnvironmentID string                     `json:"environmentId"`
	Visibility    string                     `json:"visibility"`
	Locale        string                     `json:"locale"`
	Confirmed     bool                       `json:"confirmed"`
	// Password goes to the server. Keeping it on this device is the caller's business.
	Password string `json:"password"`
}

// PublishResult is a page put up, and whether its password was kept on this device.
type PublishResult struct {
	Page          domain.Publication `json:"page"`
	PasswordSaved bool               `json:"passwordSaved"`
}

// Publish puts the page at an address, or replaces what that address already carries. The password
// of a protected page is sent to the server and saved in the OS credential store; a keyring that
// refuses does not turn a page that is up into a failed publish.
func (s *Publications) Publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	account, err := s.caller.Me(ctx)
	if err != nil {
		return PublishResult{}, err
	}
	page, err := s.publish(ctx, request)
	if err != nil {
		return PublishResult{}, err
	}
	saved := true
	if page.Visibility == domain.VisibilityPassword && request.Password != "" {
		saved = s.rememberPassword(ctx, page.CollectionID, request.Password, account.SessionID) == nil
	} else if page.Visibility != domain.VisibilityPassword {
		_ = s.rememberPassword(ctx, page.CollectionID, "", account.SessionID)
	}
	return PublishResult{Page: page, PasswordSaved: saved}, nil
}

func (s *Publications) publish(ctx context.Context, request PublishRequest) (domain.Publication, error) {
	if err := checkVisibility(request.Visibility, request.Password); err != nil {
		return domain.Publication{}, err
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return domain.Publication{}, err
	}
	workspace, err := s.remoteWorkspace(ctx, request.Workspace)
	if err != nil {
		return domain.Publication{}, err
	}
	// A folder inside a published one has no page of its own to put up: what a publish from it
	// carries is the page that holds it, which is the only way the folder's own requests can reach
	// a reader without a second document claiming them as well.
	owner, err := s.owningPageOf(ctx, request.Workspace, request.Collection)
	if err != nil {
		return domain.Publication{}, err
	}
	if request.Visibility == domain.VisibilityPassword && request.Password == "" &&
		!owner.keepsPassword() {
		return domain.Publication{}, passwordFits(request.Password)
	}
	snapshot, _, err := s.build(ctx, request.Workspace, owner.Collection,
		request.EnvironmentID, request.Locale, request.Options)
	if err != nil {
		return domain.Publication{}, err
	}
	if !owner.Held {
		snapshot.Changes = s.firstRelease(snapshot)
	}
	packed, digest, err := Encode(snapshot)
	if err != nil {
		return domain.Publication{}, err
	}

	held, err := s.server.Publish(ctx, caller, domain.PublicationDraft{
		Collection:  owner.Collection,
		Workspace:   workspace.RemoteID,
		Visibility:  request.Visibility,
		Document:    packed,
		ContentHash: digest,
		Confirmed:   request.Confirmed,
		Password:    request.Password,
	})
	if err != nil {
		return domain.Publication{}, err
	}

	if held.UpdatedAt == 0 {
		held.UpdatedAt = s.now().UnixMilli()
	}
	held.WorkspaceID = request.Workspace
	if err := s.store.SavePublication(ctx, held); err != nil {
		return domain.Publication{}, err
	}
	if err := s.notePublished(ctx, owner.Collection, snapshot); err != nil {
		return domain.Publication{}, err
	}
	return held, nil
}

// checkVisibility refuses a visibility this device does not offer, and a password the server would
// refuse. A password-protected page may be published without one when it already holds its own,
// which Publish decides once it knows the page.
func checkVisibility(visibility, password string) error {
	switch visibility {
	case domain.VisibilityPublic, domain.VisibilityUnlisted:
		return nil
	case domain.VisibilityPassword:
		if password == "" {
			return nil
		}
		return passwordFits(password)
	default:
		return domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed,
			domain.Args{"visibility": visibility})
	}
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

// firstRelease is the history a page starts with: one entry saying everything arrived at once.
func (s *Publications) firstRelease(snapshot snapshotv1.Snapshot) []snapshotv1.Release {
	requests := 0
	for _, group := range snapshot.Collection.Groups {
		requests += len(group.Endpoints)
	}
	initial := domain.PageItem{Sign: signAdded, Fields: []string{"initial:" + strconv.Itoa(requests)}}
	return snapshotChanges(appended(nil, []domain.PageItem{initial}, s.now(), 1))
}

// notePublished writes down what this publish put up and what it says has changed: this device's
// own note, written after the row exists so that a first publish keeps it as well as a later one.
func (s *Publications) notePublished(
	ctx context.Context,
	collectionID string,
	snapshot snapshotv1.Snapshot,
) error {
	outline, err := outlineOf(snapshot)
	if err != nil {
		return err
	}
	return s.store.SavePublished(ctx, collectionID, outline,
		storedChanges(snapshot.Changes), optionsOf(snapshot))
}

func (s *Publications) Unpublish(ctx context.Context, collectionID string) error {
	held, known, err := s.store.Publication(ctx, collectionID)
	if err != nil {
		return err
	}
	if !known || held.ID == "" {
		return domain.Refuse(domain.CodeNotFound, domain.ErrNotFound, nil)
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return err
	}
	held.Status = domain.PublicationRevoked
	if err := s.server.Unpublish(ctx, caller, held.ID); err != nil {
		if domain.CodeOf(err) != domain.CodeServerUnreachable {
			return err
		}
		held.PendingUnpublish = true
		return s.store.SavePublication(ctx, held)
	}
	held.PendingUnpublish = false
	return s.store.SavePublication(ctx, held)
}

// Replay plays back the takedowns that never reached the server. It is the one piece of this
// feature that runs by itself, and it runs when the window asks rather than on a timer: a page
// taken down is taken down the next time this device has a session and the network.
//
//wails:ignore
func (s *Publications) Replay(ctx context.Context) error {
	waiting, err := s.store.AwaitingUnpublish(ctx)
	if err != nil || len(waiting) == 0 {
		return err
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		// Not signed in: the takedown is waiting for a session and not for a person to come back.
		return nil
	}
	for _, held := range waiting {
		if err := s.server.Unpublish(ctx, caller, held.ID); err != nil {
			if domain.CodeOf(err) == domain.CodeServerUnreachable {
				return nil
			}
			return err
		}
		held.PendingUnpublish = false
		if err := s.store.SavePublication(ctx, held); err != nil {
			return err
		}
	}
	return nil
}

func (s *Publications) Details(ctx context.Context, collectionID string) (domain.PublicationDetails, error) {
	page, known, err := s.store.Publication(ctx, collectionID)
	if err != nil {
		return domain.PublicationDetails{}, err
	}
	if !known {
		return domain.PublicationDetails{}, domain.ErrNotFound
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return domain.PublicationDetails{}, err
	}
	return s.server.Details(ctx, caller, page.ID)
}

func (s *Publications) Restore(ctx context.Context, workspace, collectionID string, revision int) (domain.Publication, error) {
	owner, err := s.owningPageOf(ctx, workspace, collectionID)
	if err != nil {
		return domain.Publication{}, err
	}
	if !owner.Held || !owner.Page.Runnable() {
		return domain.Publication{}, domain.ErrNotFound
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return domain.Publication{}, err
	}
	page, document, err := s.server.Restore(ctx, caller, owner.Page.ID, revision)
	if err != nil {
		return domain.Publication{}, err
	}
	raw, err := expand(document)
	if err != nil {
		return domain.Publication{}, err
	}
	digest, err := Digest(raw)
	if err != nil {
		return domain.Publication{}, err
	}
	if digest != page.ContentHash {
		return domain.Publication{}, notAPage()
	}
	snapshot, err := snapshotv1.UnmarshalSnapshot(raw)
	if err != nil {
		return domain.Publication{}, notAPage()
	}
	outline, err := outlineOf(snapshot)
	if err != nil {
		return domain.Publication{}, err
	}
	page.WorkspaceID = workspace
	if err := s.store.SavePublication(ctx, page); err != nil {
		return domain.Publication{}, err
	}
	options := domain.PublicationOptions{Docs: snapshot.Options.Docs, Examples: snapshot.Options.Examples}
	if err := s.store.SavePublished(ctx, page.CollectionID, outline, storedChanges(snapshot.Changes), options); err != nil {
		return domain.Publication{}, err
	}
	return page, nil
}

func applyOptions(snapshot *snapshotv1.Snapshot, options domain.PublicationOptions) {
	snapshot.Options = snapshotv1.Options{Docs: options.Docs, Examples: options.Examples}
	if !options.Docs {
		snapshot.Collection.Description = ""
		snapshot.Prose = snapshotv1.Prose{}
	}
	for groupIndex := range snapshot.Collection.Groups {
		if !options.Docs {
			snapshot.Collection.Groups[groupIndex].Description = ""
		}
		for index := range snapshot.Collection.Groups[groupIndex].Endpoints {
			endpoint := &snapshot.Collection.Groups[groupIndex].Endpoints[index]
			if !options.Docs {
				endpoint.Description = ""
				for parameter := range endpoint.Parameters {
					endpoint.Parameters[parameter].Description = ""
				}
				if endpoint.Body != nil {
					for field := range endpoint.Body.Fields {
						endpoint.Body.Fields[field].Description = ""
					}
				}
			}
			if !options.Examples {
				endpoint.Responses = nil
			}
		}
	}
	snapshot.Collection.Groups = slices.DeleteFunc(snapshot.Collection.Groups, func(group snapshotv1.Group) bool {
		return len(group.Endpoints) == 0 && group.Description == ""
	})
}

const (
	maxDepth        = 16
	maxFolders      = 1000
	maxRequests     = 5000
	maxVariables    = 500
	maxParams       = 100
	maxFields       = 200
	maxResponses    = 50
	maxTitleRunes   = 200
	maxNameRunes    = 300
	maxTextRunes    = 8000
	maxExampleRunes = 32 << 10
	// What a page may be called from and what it may say has moved at it. The server holds a document
	// to the same three, and a page refused here is one that would have been refused there.
	maxEnvironments = 20
	maxReleases     = 200
	maxReleaseItems = 200
)

func checkName(name string, most int) error {
	if utf8.RuneCountInString(name) > most {
		return domain.Refuse(domain.CodeNameTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(most)})
	}
	return nil
}

func tooMany(what string, held, most int) error {
	if held <= most {
		return nil
	}
	return domain.Refuse(domain.CodeTooManyItems, domain.ErrNotAllowed,
		domain.Args{"what": what, "max": strconv.Itoa(most)})
}

func clip(text string, most int) (string, bool) {
	if utf8.RuneCountInString(text) <= most {
		return text, false
	}
	return string([]rune(text)[:most]), true
}

func methodOf(method string) (string, error) {
	held := strings.ToUpper(strings.TrimSpace(method))
	if held == "" {
		return "GET", nil
	}
	for _, char := range held {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '-' {
			return "", domain.Refuse(domain.CodeMethodUnsupported, domain.ErrNotAllowed,
				domain.Args{"method": method})
		}
	}
	if len(held) > 16 {
		return "", domain.Refuse(domain.CodeMethodUnsupported, domain.ErrNotAllowed,
			domain.Args{"method": method})
	}
	return held, nil
}

func (s *Publications) passwordOwner(ctx context.Context, collection string) (domain.Account, domain.Publication, error) {
	account, err := s.caller.Me(ctx)
	if err != nil {
		return domain.Account{}, domain.Publication{}, err
	}
	page, known, err := s.store.Publication(ctx, collection)
	if err != nil {
		return domain.Account{}, domain.Publication{}, err
	}
	if !account.SignedIn() || !known || !page.CanManage || !page.Runnable() {
		return domain.Account{}, domain.Publication{}, domain.ErrNotAllowed
	}
	return account, page, nil
}

// Password explicitly reads a password saved on this device, outside normal publication DTOs.
func (s *Publications) Password(ctx context.Context, collection string) (string, error) {
	account, page, err := s.passwordOwner(ctx, collection)
	if err != nil {
		return "", err
	}
	if page.Visibility != domain.VisibilityPassword {
		return "", nil
	}
	password, err := s.passwords.PublicationPassword(account, page.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	return password, err
}

// rememberPassword saves a successfully published password without a database fallback.
func (s *Publications) rememberPassword(ctx context.Context, collection, password, sessionID string) error {
	account, page, err := s.passwordOwner(ctx, collection)
	if err != nil {
		return err
	}
	if account.SessionID != sessionID {
		return domain.ErrNotAllowed
	}
	if page.Visibility != domain.VisibilityPassword {
		return s.passwords.DeletePublicationPassword(account, page.ID)
	}
	if password == "" {
		return nil
	}
	if err := passwordFits(password); err != nil {
		return err
	}
	return s.passwords.SetPublicationPassword(account, page.ID, password)
}

// The four things the server answers about published pages.
//
// Publishing is an ordinary write into the workspace the collection lives in, over the same channel and
// token as every other write. Only the page is anonymous, and it is drawn on the server, so nothing
// here fetches it.

// Publish puts a collection's document at an address, or replaces what that address carries. The
// whole document travels, not the collection: the server keeps the bytes, and the digest beside
// them lets a device tell whether a page is behind what it holds.
func (c *Client) Publish(
	ctx context.Context,
	caller domain.Caller,
	draft domain.PublicationDraft,
) (domain.Publication, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.PublishResponse, error) {
			return publicationv1.NewPublicationServiceClient(conn).Publish(ctx,
				publicationv1.PublishRequest_builder{
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

func (c *Client) Details(ctx context.Context, caller domain.Caller, id string) (domain.PublicationDetails, error) {
	answer, err := callServer(c, ctx, caller, func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.GetPublicationDetailsResponse, error) {
		return publicationv1.NewPublicationServiceClient(conn).GetPublicationDetails(ctx,
			publicationv1.GetPublicationDetailsRequest_builder{PublicationId: id}.Build())
	})
	if err != nil {
		return domain.PublicationDetails{}, err
	}
	details := domain.PublicationDetails{Views: answer.GetViews(), Imports: answer.GetImports()}
	for _, version := range answer.GetVersions() {
		item := domain.PublicationVersion{Revision: int(version.GetRevision()), EnvironmentName: version.GetEnvironmentName(),
			Requests: int(version.GetRequestCount()), RestoredFrom: int(version.GetRestoredFrom())}
		if at := version.GetPublishedAt(); at != nil {
			item.PublishedAt = at.AsTime().UnixMilli()
		}
		details.Versions = append(details.Versions, item)
	}
	return details, nil
}

func (c *Client) Restore(ctx context.Context, caller domain.Caller, id string, revision int) (domain.Publication, []byte, error) {
	answer, err := callServer(c, ctx, caller, func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.RestorePublicationResponse, error) {
		return publicationv1.NewPublicationServiceClient(conn).RestorePublication(ctx,
			publicationv1.RestorePublicationRequest_builder{PublicationId: id, Revision: int64(revision)}.Build())
	})
	if err != nil {
		return domain.Publication{}, nil, err
	}
	return publicationOf(answer.GetPublication()), answer.GetDocument(), nil
}

// Unpublish takes a page down. The address stays taken, which is the server's rule and a good one:
// a link that was handed out once must never come to mean something else.
func (c *Client) Unpublish(
	ctx context.Context,
	caller domain.Caller,
	publicationID string,
) error {
	_, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.UnpublishResponse, error) {
			return publicationv1.NewPublicationServiceClient(conn).Unpublish(ctx,
				publicationv1.UnpublishRequest_builder{PublicationId: publicationID}.Build())
		})
	return err
}

// Pages is what the server holds for the named collections. A collection with no page is absent
// from the answer rather than refused.
func (c *Client) Pages(
	ctx context.Context,
	caller domain.Caller,
	collectionIDs []string,
) ([]domain.Publication, error) {
	if len(collectionIDs) == 0 {
		return nil, nil
	}
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.GetPublicationsResponse, error) {
			return publicationv1.NewPublicationServiceClient(conn).GetPublications(ctx,
				publicationv1.GetPublicationsRequest_builder{CollectionIds: collectionIDs}.Build())
		})
	if err != nil {
		return nil, err
	}
	return publicationsOf(answer.GetPublications()), nil
}

// Published is the pages of one workspace, newest first.
func (c *Client) Published(
	ctx context.Context,
	caller domain.Caller,
	workspace string,
) ([]domain.Publication, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*publicationv1.ListPublicationsResponse, error) {
			client := publicationv1.NewPublicationServiceClient(conn)
			result := publicationv1.ListPublicationsResponse_builder{}.Build()
			token := ""
			for {
				page, err := client.ListPublications(ctx, publicationv1.ListPublicationsRequest_builder{WorkspaceId: workspace, PageToken: token}.Build())
				if err != nil {
					return nil, err
				}
				result.SetPublications(append(result.GetPublications(), page.GetPublications()...))

				token = page.GetNextPageToken()
				if token == "" {
					return result, nil
				}
			}
		})
	if err != nil {
		return nil, err
	}
	return publicationsOf(answer.GetPublications()), nil
}

// publicationOf is one page as the wire carries it, in the shape this app keeps.
func publicationOf(held *publicationv1.Publication) domain.Publication {
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
func publicationsOf(held []*publicationv1.Publication) []domain.Publication {
	out := make([]domain.Publication, 0, len(held))
	for _, one := range held {
		out = append(out, publicationOf(one))
	}
	return out
}

// visibilityName is who a page is for, in this app's words. A visibility this build has never heard
// of is answered as none rather than the nearest one: a window that guessed would offer to publish
// a page it does not understand.
func visibilityName(held publicationv1.Visibility) string {
	switch held {
	case publicationv1.Visibility_VISIBILITY_PUBLIC:
		return domain.VisibilityPublic
	case publicationv1.Visibility_VISIBILITY_UNLISTED:
		return domain.VisibilityUnlisted
	case publicationv1.Visibility_VISIBILITY_PASSWORD:
		return domain.VisibilityPassword
	}
	return ""
}

// visibilityOf is that word on the way back to the server.
func visibilityOf(name string) publicationv1.Visibility {
	switch name {
	case domain.VisibilityPublic:
		return publicationv1.Visibility_VISIBILITY_PUBLIC
	case domain.VisibilityUnlisted:
		return publicationv1.Visibility_VISIBILITY_UNLISTED
	case domain.VisibilityPassword:
		return publicationv1.Visibility_VISIBILITY_PASSWORD
	}
	return publicationv1.Visibility_VISIBILITY_UNSPECIFIED
}

// statusName is whether a page answers at all.
func statusName(held publicationv1.PublicationStatus) string {
	switch held {
	case publicationv1.PublicationStatus_PUBLICATION_STATUS_ACTIVE:
		return domain.PublicationActive
	case publicationv1.PublicationStatus_PUBLICATION_STATUS_REVOKED:
		return domain.PublicationRevoked
	}
	return ""
}
