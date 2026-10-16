package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
)

// CollectionsService is the saved requests. Every call that changes the tree answers with the whole
// tree — a rename moves a row the list is already drawing. It holds the draft too, because a saved
// request is edited as one.
type CollectionsService struct {
	collections *collection.UseCase
	drafts      *draft.UseCase
}

func NewCollectionsService(
	uc *collection.UseCase,
	drafts *draft.UseCase,
) *CollectionsService {
	return &CollectionsService{collections: uc, drafts: drafts}
}

func (s *CollectionsService) Tree(ctx context.Context) ([]domain.Collection, error) {
	return s.collections.Tree(ctx)
}

// Node is what opening a saved request needs: a tree row carries its method and nothing else.
func (s *CollectionsService) Node(ctx context.Context, id string) (domain.CollectionNode, error) {
	return s.collections.Node(ctx, id)
}

// Contents is what the collection page's table draws: the requests inside the collection that is
// open, folders and all, with the address each of them goes to and the folder it sits in. The tree
// carries no request payload, so the page asks for its own rows rather than making every tree read
// heavier for one screen.
func (s *CollectionsService) Contents(ctx context.Context, id string) ([]domain.LevelRow, error) {
	return s.collections.Contents(ctx, id)
}

// CreateCollection answers with CreatedCollection, and that is CreatedNode's shape for
// CreatedNode's reason: the id has to come back from Go, because a lookup by name would find the
// older row of the same name — and a folder is created inside another one as often as at the top.
func (s *CollectionsService) CreateCollection(
	ctx context.Context,
	name string,
	description string,
	parentID string,
) (CreatedCollection, error) {
	collection, tree, err := s.collections.CreateCollection(ctx, name, description, parentID)
	if err != nil {
		return CreatedCollection{}, err
	}
	return CreatedCollection{Collection: collection, Tree: tree}, nil
}

// CreatedNode is what a creation answers with: the row that appeared and the tree it appeared in.
// The id has to come back from Go — a lookup by name would find the older row of the same name.
type CreatedNode struct {
	Node domain.CollectionNode `json:"node"`
	Tree []domain.Collection   `json:"tree"`
}

// CreatedCollection is CreatedNode for a collection rather than a request.
type CreatedCollection struct {
	Collection domain.Collection   `json:"collection"`
	Tree       []domain.Collection `json:"tree"`
}

func (s *CollectionsService) CreateNode(
	ctx context.Context,
	in collection.NodeDraft,
) (CreatedNode, error) {
	node, tree, err := s.collections.CreateNode(ctx, in)
	if err != nil {
		return CreatedNode{}, err
	}
	return CreatedNode{Node: node, Tree: tree}, nil
}

// SaveDraft copies a draft into a collection as a new request — the active tab's, unless the caller
// names another. The draft is not touched: saving a copy is not a move. The request arrives whole,
// because the node is written once — a second call filling in an empty one could leave it saved
// with no address.
func (s *CollectionsService) SaveDraft(
	ctx context.Context,
	collectionID string,
	name string,
	draftID domain.DraftID,
) (CreatedNode, error) {
	draft, err := s.drafts.Current(ctx, draftID)
	if err != nil {
		return CreatedNode{}, err
	}

	node, tree, err := s.collections.CreateNode(ctx, collection.NodeDraft{
		CollectionID:  collectionID,
		Name:          name,
		Method:        draft.Method,
		URL:           draft.URL,
		Params:        draft.Params,
		Headers:       draft.Headers,
		PathParams:    draft.PathParams,
		Body:          draft.Body,
		BodyKind:      draft.BodyKind,
		Form:          draft.Form,
		BodyFile:      draft.BodyFile,
		Auth:          &draft.Auth,
		EnvironmentID: draft.EnvironmentID,
	})
	if err != nil {
		return CreatedNode{}, err
	}
	return CreatedNode{Node: node, Tree: tree}, nil
}

// SaveDraftToNew saves a draft as the first request of a collection that does not exist yet, named
// by the caller. The sheet's "Create “X”" row is one intention, and serving it in two calls would
// leave an empty collection behind whenever the second one failed — a collection nobody asked for,
// and one somebody then has to go and delete.
//
// It is here rather than in the collection feature because the draft is: the request being composed
// is this layer's to read, and the two calls that make one collection of it are this layer's to
// join.
func (s *CollectionsService) SaveDraftToNew(
	ctx context.Context,
	collection string,
	name string,
	draftID domain.DraftID,
) (CreatedNode, error) {
	made, _, err := s.collections.CreateCollection(ctx, collection, "", "")
	if err != nil {
		return CreatedNode{}, err
	}
	return s.SaveDraft(ctx, made.ID, name, draftID)
}

// Describe writes what a collection is for — the line the overview draws above its tabs. Empty is
// an answer there: the header then shows the placeholder that invites one.
func (s *CollectionsService) Describe(
	ctx context.Context,
	id string,
	description string,
) ([]domain.Collection, error) {
	return s.collections.Describe(ctx, id, description)
}

// SaveAuth writes what a collection authorizes its requests with. «None» is the same call with an
// empty auth: a level that has none is a level the ones below it inherit past, and the overview
// then draws the tab the way a collection without one looks.
func (s *CollectionsService) SaveAuth(
	ctx context.Context,
	id string,
	auth domain.Auth,
) ([]domain.Collection, error) {
	return s.collections.SaveAuth(ctx, id, auth)
}

// SaveExamples writes the answers kept beside a request: what an import carried in, and what the
// window saves from a live answer. The node comes back whole because the pane reads the set — and
// everything around it — from that one answer.
func (s *CollectionsService) SaveExamples(
	ctx context.Context,
	id string,
	examples []domain.ResponseExample,
) (domain.CollectionNode, error) {
	return s.collections.SaveExamples(ctx, id, examples)
}

// SaveVariables writes the `{{tokens}}` a collection answers for the requests inside it. The set
// arrives whole, as the editor holds it; the tree comes back because the panel draws these levels.
func (s *CollectionsService) SaveVariables(
	ctx context.Context,
	id string,
	variables []domain.Variable,
) ([]domain.Collection, error) {
	return s.collections.SaveVariables(ctx, id, variables)
}

func (s *CollectionsService) Rename(
	ctx context.Context,
	id string,
	name string,
) ([]domain.Collection, error) {
	return s.collections.Rename(ctx, id, name)
}

// Duplicate copies a node under a name the window composes: the suffix that says what the copy is
// is a word, and words belong to the side that knows the language.
func (s *CollectionsService) Duplicate(
	ctx context.Context,
	id string,
	suffix string,
) ([]domain.Collection, error) {
	return s.collections.Duplicate(ctx, id, suffix)
}

// MoveNode and MoveCollection are what a drop in the tree calls. The position is an index in the
// level the row was dropped into, counted the way the window drew it — the requests of a collection
// and the collections inside it are one list on screen and one number line here.
func (s *CollectionsService) MoveNode(
	ctx context.Context,
	id string,
	collectionID string,
	position int64,
) ([]domain.Collection, error) {
	return s.collections.MoveNode(ctx, id, collectionID, position)
}

func (s *CollectionsService) MoveCollection(
	ctx context.Context,
	id string,
	parentID string,
	position int64,
) ([]domain.Collection, error) {
	return s.collections.MoveCollection(ctx, id, parentID, position)
}

func (s *CollectionsService) Delete(ctx context.Context, id string) ([]domain.Collection, error) {
	return s.collections.Delete(ctx, id)
}

// Run starts a collection or a folder and answers with the id of the run. What happens next arrives
// as events: a run of fifty requests outlives the call that started it, and the window draws it as
// it goes.
func (s *CollectionsService) Run(
	ctx context.Context,
	collectionID string,
	nodeID string,
) (string, error) {
	return s.collections.Run(ctx, collectionID, nodeID)
}

// Stop ends the run after the request that is already in flight.
func (s *CollectionsService) Stop(ctx context.Context) (bool, error) {
	return s.collections.Stop(), nil
}

// LastRun is what the overview draws when it opens, and nil when nothing has been run here yet.
func (s *CollectionsService) LastRun(
	ctx context.Context,
	collectionID string,
	nodeID string,
) (*domain.CollectionRun, error) {
	return s.collections.LastRun(ctx, collectionID, nodeID)
}

// NodeEditor is assembled in this layer because this is the only one that knows both features —
// collections own the tree, the draft owns the editing — and neither should know the other.
type NodeEditor struct {
	Tree  []domain.Collection   `json:"tree"`
	Node  domain.CollectionNode `json:"node"`
	State draft.State           `json:"state"`
}

// OpenNode puts a saved request into the draft its card edits, and answers with everything the card
// draws. Opening is what a save starts over from as well: a draft that has just been opened has
// nothing unsaved in it, which is where the window's "Unsaved" comes from and goes.
func (s *CollectionsService) OpenNode(ctx context.Context, id string) (NodeEditor, error) {
	node, err := s.collections.Node(ctx, id)
	if err != nil {
		return NodeEditor{}, err
	}
	return s.editor(ctx, node)
}

// SaveNode writes what the card is editing back into the tree. The draft is where the request is —
// its method, its address, its rows, its body — and the node keeps what the card does not own: what
// it is called, where it sits, when it was made.
func (s *CollectionsService) SaveNode(ctx context.Context, id string) (NodeEditor, error) {
	node, err := s.collections.Node(ctx, id)
	if err != nil {
		return NodeEditor{}, err
	}
	edited, err := s.drafts.Current(ctx, domain.DraftID(id))
	if err != nil {
		return NodeEditor{}, err
	}
	if _, err := s.collections.SaveNode(ctx, nodeFromDraft(node, edited)); err != nil {
		return NodeEditor{}, err
	}

	// The node is read again and the draft opened on it, so the answer is the saved request rather
	// than the one that was being edited — and the next edit counts from there.
	saved, err := s.collections.Node(ctx, id)
	if err != nil {
		return NodeEditor{}, err
	}
	return s.editor(ctx, saved)
}

// editor is the answer both calls give: the node, the draft opened on it, and the tree. Opening
// does not go through the draft service, so the state is completed here as well — a card is drawn
// from this one answer, and would otherwise show a request that inherits nothing until it is
// edited.
func (s *CollectionsService) editor(
	ctx context.Context,
	node domain.CollectionNode,
) (NodeEditor, error) {
	state, err := s.drafts.Open(ctx, draft.FromNode(node))
	if err != nil {
		return NodeEditor{}, err
	}
	tree, err := s.collections.Tree(ctx)
	if err != nil {
		return NodeEditor{}, err
	}
	return NodeEditor{Tree: tree, Node: node,
		State: completed(ctx, s.drafts, s.collections, state)}, nil
}

// nodeFromDraft is the other direction. The two answers that are not credentials are stored as the
// absence of an answer, so a card that never touched the chip cannot turn «Inherit» into «None» —
// the two are different answers, and only one of them was given. What a card was holding
// when it gave one of them stays with it, which is domain.Auth.Stored's job, not this one's.
func nodeFromDraft(node domain.CollectionNode, d domain.Draft) domain.CollectionNode {
	node.Method = d.Method
	node.URL = d.URL
	node.Params = d.Params
	node.Headers = d.Headers
	node.PathParams = d.PathParams
	node.Body = d.Body
	node.BodyKind = domain.KindOf(d.BodyKind)
	node.Form = d.Form
	node.BodyFile = d.BodyFile
	node.Auth = d.Auth.Stored()
	node.EnvironmentID = d.EnvironmentID
	return node
}
