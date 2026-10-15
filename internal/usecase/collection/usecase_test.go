package collection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// A run writes to it from its own goroutine while a test reads, so the mutex is the fake standing
// in for the database's own serialisation.
type fakeStore struct {
	mu          sync.Mutex
	collections []domain.Collection
	nodes       []domain.CollectionNode
	runs        []domain.CollectionRun
	results     map[string][]domain.CollectionRunResult
	gone        map[string]bool
	// A request carries its own scripts, a collection does not — which is the shape the table has, and
	// the reason this is a map rather than a field.
	scripts map[string]*domain.Scripts
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		gone:    map[string]bool{},
		results: map[string][]domain.CollectionRunResult{},
		scripts: map[string]*domain.Scripts{},
	}
}

func (f *fakeStore) hidden(id string) bool {
	return f.gone[id]
}

// bury marks a request gone, and buryCollection a collection with everything inside it — the way
// the schema's two cascades would: the nesting one, and the collection a request belongs to.
func (f *fakeStore) bury(id string) {
	f.gone[id] = true
}

func (f *fakeStore) buryCollection(id string) {
	if f.gone[id] {
		return
	}
	f.gone[id] = true
	for _, collection := range f.collections {
		if collection.ParentID == id {
			f.buryCollection(collection.ID)
		}
	}
	for _, node := range f.nodes {
		if node.CollectionID == id {
			f.bury(node.ID)
		}
	}
}

// fakeScope answers with the workspace the test is working in. The store below keeps one tree and
// ignores the id: what is being tested here is the use case, and the split between workspaces is
// the SQL's own test.
type fakeScope struct{ id string }

// ws is the workspace the tests below call the store in directly — the one the app is born with,
// since none of them is about a second space.
const ws = domain.WorkspacePersonalID

func (f fakeScope) ActiveWorkspace(context.Context) (string, error) {
	if f.id == "" {
		return domain.WorkspacePersonalID, nil
	}
	return f.id, nil
}

func (f *fakeStore) Collections(context.Context, string) ([]domain.Collection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tree(), nil
}

// tree is the shape the use case reads: collections nested by parent, and inside each one its own
// requests, shallow and in position order — which is the order the store's own query returns them
// in, and the order both the level merge and the run walk depend on.
func (f *fakeStore) tree() []domain.Collection {
	byParent := map[string][]domain.Collection{}
	for _, collection := range f.collections {
		if f.hidden(collection.ID) {
			continue
		}
		byParent[collection.ParentID] = append(byParent[collection.ParentID], collection)
	}

	var build func(parent string) []domain.Collection
	build = func(parent string) []domain.Collection {
		children := byParent[parent]
		sort.SliceStable(children,
			func(i, j int) bool { return children[i].Position < children[j].Position })

		out := []domain.Collection{}
		for _, collection := range children {
			row := collection
			row.Items = f.itemsOf(row.ID)
			row.Children = build(row.ID)
			out = append(out, row)
		}
		return out
	}
	return build("")
}

// itemsOf is one collection's requests as the list draws them: a row carries its method, and two
// hundred bodies is not what a list loads.
func (f *fakeStore) itemsOf(collectionID string) []domain.CollectionNode {
	out := []domain.CollectionNode{}
	for _, node := range f.nodes {
		if node.CollectionID != collectionID || f.hidden(node.ID) {
			continue
		}
		row := node
		row.Params, row.Headers, row.Body = nil, nil, ""
		row.URL, row.Scripts = "", nil
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (f *fakeStore) Node(_ context.Context, id string) (domain.CollectionNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, node := range f.nodes {
		if node.ID == id && !f.hidden(id) {
			return node, nil
		}
	}
	return domain.CollectionNode{}, domain.ErrNotFound
}

// SpaceOf answers with the one space these tests work in: none of them is about a second one, and
// the split between spaces is the SQL's own test.
func (f *fakeStore) SpaceOf(_ context.Context, collectionID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, collection := range f.collections {
		if collection.ID == collectionID && !f.hidden(collectionID) {
			return domain.WorkspacePersonalID, nil
		}
	}
	return "", domain.ErrNotFound
}

// ContentRows is the page's read: the requests inside a collection, folders and all, with the
// address each is drawn with. It walks the way the SQL does — a level's own requests and its
// folders are one numbered sequence — because a fake that answered only the level would let a
// page that draws one level pass.
func (f *fakeStore) ContentRows(_ context.Context, collectionID string) ([]domain.LevelRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var walk func(level string) []domain.LevelRow
	walk = func(level string) []domain.LevelRow {
		type entry struct {
			position int64
			node     *domain.CollectionNode
			child    *domain.Collection
		}
		entries := []entry{}
		for i := range f.nodes {
			if f.nodes[i].CollectionID == level {
				entries = append(entries, entry{position: f.nodes[i].Position, node: &f.nodes[i]})
			}
		}
		for i := range f.collections {
			if f.collections[i].ParentID == level {
				entries = append(entries, entry{position: f.collections[i].Position,
					child: &f.collections[i]})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].position < entries[j].position
		})

		out := []domain.LevelRow{}
		for _, e := range entries {
			if e.child != nil {
				if f.hidden(e.child.ID) {
					continue
				}
				out = append(out, walk(e.child.ID)...)
				continue
			}
			if f.hidden(e.node.ID) {
				continue
			}
			out = append(out, domain.LevelRow{
				ID:     e.node.ID,
				Name:   e.node.Name,
				Method: e.node.Method,
				URL:    e.node.URL,
			})
		}
		return out
	}

	// A collection the store has never heard of reads as nothing, the way a query for it would.
	found := false
	for _, collection := range f.collections {
		if collection.ID == collectionID {
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	return walk(collectionID), nil
}

func (f *fakeStore) SaveCollection(
	_ context.Context,
	_ string,
	collection domain.Collection,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	collection.Items, collection.Children = nil, nil
	for i := range f.collections {
		if f.collections[i].ID == collection.ID {
			// The row's place is written by the insert and left alone by the update, the way the SQL
			// does it: a rename saves the row it read, and moving is a gesture of its own.
			collection.ParentID = f.collections[i].ParentID
			f.collections[i] = collection
			return nil
		}
	}
	f.collections = append(f.collections, collection)
	return nil
}

func (f *fakeStore) SaveDocs(_ context.Context, id string, docs domain.Docs) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.collections {
		if f.collections[i].ID == id {
			f.collections[i].Docs = docs
			return nil
		}
	}
	return nil
}

func (f *fakeStore) SaveNode(_ context.Context, node domain.CollectionNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.nodes {
		if f.nodes[i].ID == node.ID {
			f.nodes[i] = node
			return nil
		}
	}
	f.nodes = append(f.nodes, node)
	return nil
}

func (f *fakeStore) DeleteCollection(_ context.Context, _ string, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.buryCollection(id)
	return nil
}

func (f *fakeStore) DeleteNode(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.bury(id)
	return nil
}

// Scripts answers the way the table does: an id names a collection or a node, and a collection's
// scripts live beside the tree because the struct the list draws has no field for them. A level
// with nothing of its own answers nothing, which is not the same as an empty script.
func (f *fakeStore) Scripts(_ context.Context, _ string, id string) (*domain.Scripts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.hidden(id) {
		return nil, domain.ErrNotFound
	}
	for i := range f.nodes {
		if f.nodes[i].ID == id {
			return f.nodes[i].Scripts, nil
		}
	}
	for _, collection := range f.collections {
		if collection.ID == id {
			return f.scripts[id], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeStore) SaveScripts(
	_ context.Context,
	_ string,
	id string,
	scripts *domain.Scripts,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.hidden(id) {
		return domain.ErrNotFound
	}
	for i := range f.nodes {
		if f.nodes[i].ID == id {
			f.nodes[i].Scripts = scripts
			return nil
		}
	}
	for _, collection := range f.collections {
		if collection.ID == id {
			f.scripts[id] = scripts
			return nil
		}
	}
	return domain.ErrNotFound
}

// NextPosition is one past the last child of a level, requests and nested collections counted
// together: they share one number line, which is what makes them one list on screen.
func (f *fakeStore) NextPosition(_ context.Context, _ string, collectionID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var next int64
	consider := func(position int64) {
		if position >= next {
			next = position + 1
		}
	}
	for _, node := range f.nodes {
		if node.CollectionID == collectionID && !f.hidden(node.ID) {
			consider(node.Position)
		}
	}
	for _, collection := range f.collections {
		if collection.ParentID == collectionID && !f.hidden(collection.ID) {
			consider(collection.Position)
		}
	}
	return next, nil
}

// MoveNode and MoveCollection are the store's own two moves: the row changes where it lives, and
// both the level it left and the level it joined are numbered again with it in place.
func (f *fakeStore) MoveNode(
	_ context.Context,
	_ string,
	id string,
	collectionID string,
	position int64,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.nodes {
		if f.nodes[i].ID != id || f.hidden(id) {
			continue
		}
		from := f.nodes[i].CollectionID
		f.nodes[i].CollectionID = collectionID
		f.renumber(placeAt(f.levelOf(from), "", 0))
		f.renumber(placeAt(f.levelOf(collectionID), id, position))
		return nil
	}
	return domain.ErrNotFound
}

func (f *fakeStore) MoveCollection(
	_ context.Context,
	_ string,
	id string,
	parentID string,
	position int64,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.collections {
		if f.collections[i].ID != id || f.hidden(id) {
			continue
		}
		from := f.collections[i].ParentID
		f.collections[i].ParentID = parentID
		f.renumber(placeAt(f.levelOf(from), "", 0))
		f.renumber(placeAt(f.levelOf(parentID), id, position))
		return nil
	}
	return domain.ErrNotFound
}

// levelOf is one level in the order it is drawn: a collection's requests and the collections inside
// it, merged by position — the same one list the window draws and the store numbers.
func (f *fakeStore) levelOf(collectionID string) []string {
	type entry struct {
		id       string
		position int64
	}
	entries := []entry{}
	for _, node := range f.nodes {
		if node.CollectionID == collectionID && !f.hidden(node.ID) {
			entries = append(entries, entry{id: node.ID, position: node.Position})
		}
	}
	for _, collection := range f.collections {
		if collection.ParentID == collectionID && !f.hidden(collection.ID) {
			entries = append(entries, entry{id: collection.ID, position: collection.Position})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].position < entries[j].position })

	out := make([]string, 0, len(entries))
	for _, one := range entries {
		out = append(out, one.id)
	}
	return out
}

// placeAt lifts a row out of a level and puts it back at the dropped index, which counts the level
// as it looks now — the row being moved included — the way the store's own does.
func placeAt(level []string, moved string, at int64) []string {
	others := make([]string, 0, len(level))
	insert := int64(0)
	for i, id := range level {
		if id == moved {
			continue
		}
		if int64(i) < at {
			insert++
		}
		others = append(others, id)
	}
	if insert > int64(len(others)) {
		insert = int64(len(others))
	}
	if insert < 0 {
		insert = 0
	}

	out := make([]string, 0, len(others)+1)
	out = append(out, others[:insert]...)
	out = append(out, moved)
	out = append(out, others[insert:]...)
	return out
}

func (f *fakeStore) renumber(level []string) {
	for i, id := range level {
		for j := range f.nodes {
			if f.nodes[j].ID == id {
				f.nodes[j].Position = int64(i)
			}
		}
		for j := range f.collections {
			if f.collections[j].ID == id {
				f.collections[j].Position = int64(i)
			}
		}
	}
}

func (f *fakeStore) SaveRun(_ context.Context, run domain.CollectionRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	run.Results = nil
	for i := range f.runs {
		if f.runs[i].ID == run.ID {
			f.runs[i] = run
			return nil
		}
	}
	f.runs = append(f.runs, run)
	return nil
}

func (f *fakeStore) AppendRunResult(
	_ context.Context,
	runID string,
	result domain.CollectionRunResult,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.results[runID] = append(f.results[runID], result)
	return nil
}

func (f *fakeStore) LastRun(
	_ context.Context,
	collectionID string,
	nodeID string,
) (domain.CollectionRun, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := len(f.runs) - 1; i >= 0; i-- {
		run := f.runs[i]
		if run.CollectionID == collectionID && run.NodeID == nodeID {
			run.Results = append([]domain.CollectionRunResult{}, f.results[run.ID]...)
			return run, true, nil
		}
	}
	return domain.CollectionRun{}, false, nil
}

// fakeSender answers with what the test set up for a URL. It is the only thing the run knows about
// the network, which is what lets a test decide what a server did.
type fakeSender struct {
	mu      sync.Mutex
	sent    []RunRequest
	answers map[string]answer
	// onSend runs inside the send, which is where a test can look at a run while it is going.
	onSend func(RunRequest)
}

type answer struct {
	status      int
	durationUs  int64
	transportEr string
	skipped     bool
	// refusal is the app saying no rather than the network failing — a request whose `{{tokens}}`
	// answered to nothing. It is an error, not a record: nothing went out.
	refusal error
}

func newFakeSender() *fakeSender {
	return &fakeSender{answers: map[string]answer{}}
}

func (f *fakeSender) reply(url string, status int, durationUs int64) *fakeSender {
	f.answers[url] = answer{status: status, durationUs: durationUs}
	return f
}

func (f *fakeSender) fail(url string) *fakeSender {
	f.answers[url] = answer{transportEr: "server did not respond"}
	return f
}

// skip is a request a pre-request script kept from going out: nothing is sent, and the answer says
// so rather than pretending the server said something.
func (f *fakeSender) skip(url string) *fakeSender {
	f.answers[url] = answer{skipped: true}
	return f
}

// refuse is a request the app would not send at all: the sender answers with the refusal, the way
// the real one does when a `{{token}}` in the request has nothing to answer it.
func (f *fakeSender) refuse(url string, refusal error) *fakeSender {
	f.answers[url] = answer{refusal: refusal}
	return f
}

// hook sets what happens inside a send, which is the only moment a test can look at a run while it
// is going.
func (f *fakeSender) hook(onSend func(RunRequest)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.onSend = onSend
}

func (f *fakeSender) Send(_ context.Context, req RunRequest) (domain.Record, error) {
	f.mu.Lock()
	onSend := f.onSend
	f.sent = append(f.sent, req)
	answer, ok := f.answers[req.URL]
	f.mu.Unlock()

	// Outside the lock: a hook that stops the run is a caller of this feature, not of this fake.
	if onSend != nil {
		onSend(req)
	}

	if !ok {
		return domain.Record{}, fmt.Errorf("no response for %s", req.URL)
	}
	if answer.skipped {
		return domain.Record{Skipped: true}, nil
	}
	if answer.refusal != nil {
		return domain.Record{}, answer.refusal
	}
	return domain.Record{RecordSummary: domain.RecordSummary{
		ID:         "rec-" + req.URL,
		Status:     answer.status,
		DurationUs: answer.durationUs,
		Error:      answer.transportEr,
	}}, nil
}

func (f *fakeSender) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := []string{}
	for _, req := range f.sent {
		out = append(out, req.URL)
	}
	return out
}

// requests is everything that was sent, whole: what a run hands the sender is what this feature is
// responsible for, and a test that only looked at the URLs could not see the rest of it.
func (f *fakeSender) requests() []RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]RunRequest{}, f.sent...)
}

// fakeAssertions is the reports of a run's rows: what the scripts around each record asserted. It
// answers by record id, which is what the run hands it — a row whose id nobody wrote about is a row
// with nothing asserted, not an error.
type fakeAssertions struct {
	mu      sync.Mutex
	answers map[string][2]int
}

func newFakeAssertions() *fakeAssertions {
	return &fakeAssertions{answers: map[string][2]int{}}
}

// asserts sets what the report of one record says: how many of its assertions held, and out of how
// many.
func (f *fakeAssertions) asserts(recordID string, passed, total int) *fakeAssertions {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.answers[recordID] = [2]int{passed, total}
	return f
}

func (f *fakeAssertions) AssertionCounts(_ context.Context, recordID string) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	counts, ok := f.answers[recordID]
	if !ok {
		return 0, 0, nil
	}
	return counts[0], counts[1], nil
}

// fakeNotifier keeps what was published and lets a test wait for the end of a run instead of
// sleeping: the run writes from its own goroutine.
type fakeNotifier struct {
	mu     sync.Mutex
	events []struct {
		topic   string
		payload any
	}
	finished chan struct{}
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{finished: make(chan struct{}, 1)}
}

func (n *fakeNotifier) Publish(topic string, payload any) {
	n.mu.Lock()
	n.events = append(n.events, struct {
		topic   string
		payload any
	}{topic, payload})
	n.mu.Unlock()

	if topic == TopicRunFinished {
		select {
		case n.finished <- struct{}{}:
		default:
		}
	}
}

func (n *fakeNotifier) topics() []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := []string{}
	for _, event := range n.events {
		out = append(out, event.topic)
	}
	return out
}

func (n *fakeNotifier) runFinished(t *testing.T) domain.CollectionRun {
	t.Helper()
	select {
	case <-n.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never finished")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	for _, event := range n.events {
		if event.topic == TopicRunFinished {
			return event.payload.(domain.CollectionRun)
		}
	}
	t.Fatal("no finished run was published")
	return domain.CollectionRun{}
}

func newTestUseCase() (*UseCase, *fakeStore) {
	uc, store, _, _, _ := newTestRun()
	return uc, store
}

// newTestRun is the use case with all of its dependencies, for the tests that also look at what was
// sent, what was published and what the scripts asserted.
func newTestRun() (*UseCase, *fakeStore, *fakeSender, *fakeNotifier, *fakeAssertions) {
	store := newFakeStore()
	sender := newFakeSender()
	notifier := newFakeNotifier()
	asserted := newFakeAssertions()
	return NewUseCase(store, fakeScope{}, sender, asserted, newFakeEnvironment(), notifier,
		platform.NewIDGen()), store, sender, notifier, asserted
}

func only(t *testing.T, tree []domain.Collection) domain.Collection {
	t.Helper()
	if len(tree) != 1 {
		t.Fatalf("tree = %d collections, want 1", len(tree))
	}
	return tree[0]
}

func TestCreateCollectionAppends(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, first, err := uc.CreateCollection(ctx, "  Users  ", " test ", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	_, tree, err := uc.CreateCollection(ctx, "Orders", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want 2", len(tree))
	}
	if tree[0].Name != "Users" || tree[0].Description != "test" {
		t.Errorf("first = %+v, want the name trimmed", tree[0])
	}
	if tree[1].Position != 1 {
		t.Errorf("second position = %d, want the end of the list", tree[1].Position)
	}
	if first[0].ID == tree[1].ID {
		t.Error("two collections share an id")
	}
}

func TestCreateCollectionRejectsAnEmptyName(t *testing.T) {
	uc, _ := newTestUseCase()

	if _, _, err := uc.CreateCollection(context.Background(), "   ", "", ""); !errors.Is(err,
		domain.ErrNotAllowed) {
		t.Fatalf("empty name = %v, want ErrNotAllowed", err)
	}
}

func TestCreateNodeLandsAtTheEndOfItsLevel(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	id := only(t, tree).ID

	if _, _, err = uc.CreateNode(ctx, NodeDraft{CollectionID: id, Name: "First"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Dropped in at the top of the level, so the request that was there is now second.
	nestedID := nestCollection(t, uc, "Nested", id)

	_, tree, err = uc.CreateNode(ctx, NodeDraft{CollectionID: id, Name: "Second", Method: "post"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	collection := only(t, tree)
	level := collection.Level()
	if len(level) != 3 {
		t.Fatalf("level = %+v, want two requests and the collection between them", level)
	}
	// The requests of a collection and the collections inside it are one number line: the new request
	// lands after the collection that was dropped in, not at the end of a group of its own.
	if level[0].Collection == nil || level[0].Collection.ID != nestedID {
		t.Errorf("level[0] = %+v, want the nested collection at 0", level[0])
	}
	if level[0].Collection.Position != 0 {
		t.Errorf("level[0] = %+v, want it numbered 0", level[0])
	}
	if level[1].Node == nil || level[1].Node.Name != "First" || level[1].Node.Position != 1 {
		t.Errorf("level[1] = %+v, want the first request pushed to 1", level[1])
	}
	if level[1].Node.Method != "GET" {
		t.Errorf("method = %q, want the default one", level[1].Node.Method)
	}
	if level[2].Node == nil || level[2].Node.Position != 2 || level[2].Node.Method != "POST" {
		t.Errorf("level[2] = %+v, want the new request at 2 with the method as typed", level[2])
	}
}

// nestCollection creates a collection and drops it at the top of the one named, which is how a
// collection comes to sit inside another: the tree has no gesture that makes one there.
func nestCollection(t *testing.T, uc *UseCase, name string, parentID string) string {
	t.Helper()
	return nestCollectionAt(t, uc, name, parentID, 0)
}

// nestCollectionAt is the same drop at a place of its own, which is what a fixture needs when it is
// the order of the level that a test is about.
func nestCollectionAt(t *testing.T, uc *UseCase, name string, parentID string, at int64) string {
	t.Helper()
	ctx := context.Background()

	// The row that came back is the one that appeared: the answer carries it now, so nothing here has
	// to guess which of the last two it is.
	made, _, err := uc.CreateCollection(ctx, name, "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := uc.MoveCollection(ctx, made.ID, parentID, at); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	return made.ID
}

// A request saved from a composer is written whole: the row that comes back is the one that
// appeared, with everything the request already had, so nothing depends on finding it again by
// name.
func TestCreateNodeTakesAWholeRequest(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	created, tree, err := uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID,
		Name:         "Saved",
		Method:       "patch",
		URL:          "https://api.example.com/users/1?page=2",
		Headers: []domain.Row{
			{ID: "h1", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
		},
		Body: `{"data": 1}`,
		Auth: bearerRef("{{token}}"),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if created.ID == "" || created.Name != "Saved" {
		t.Fatalf("created = %+v, want the row that appeared", created)
	}
	if len(only(t, tree).Items) != 1 || only(t, tree).Items[0].ID != created.ID {
		t.Errorf("tree = %+v, want the created row in it", only(t, tree).Items)
	}

	stored, err := store.Node(ctx, created.ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.Method != "PATCH" || stored.URL != "https://api.example.com/users/1?page=2" {
		t.Errorf("stored = %+v, want the request it was given", stored)
	}
	if len(stored.Headers) != 1 || stored.Body != `{"data": 1}` {
		t.Errorf("stored = %+v, want its headers and body", stored)
	}
	if stored.Auth == nil || stored.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want the choice the composer made", stored.Auth)
	}
}

func TestRenameReachesBothKinds(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "Request"})
	requestID := only(t, tree).Items[0].ID

	tree, err := uc.Rename(ctx, collectionID, "Renamed")
	if err != nil {
		t.Fatalf("Rename collection: %v", err)
	}
	if only(t, tree).Name != "Renamed" {
		t.Errorf("collection = %+v, want the new name", only(t, tree))
	}

	tree, err = uc.Rename(ctx, requestID, "  Also  ")
	if err != nil {
		t.Fatalf("Rename request: %v", err)
	}
	if only(t, tree).Items[0].Name != "Also" {
		t.Errorf("request = %+v, want the name trimmed", only(t, tree).Items[0])
	}
}

// Renaming a collection inside another one is the same gesture, and it leaves the row where it is:
// a rename saves the row it read, and where a row sits is not part of what a rename edits.
func TestRenameKeepsANestedCollectionNested(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)

	tree, err := uc.Rename(ctx, nestedID, "Renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	renamed, ok := findCollection(tree, nestedID)
	if !ok {
		t.Fatalf("the nested collection left the tree: %+v", tree)
	}
	if renamed.Name != "Renamed" || renamed.ParentID != collectionID {
		t.Errorf("nested = %+v, want it renamed and still inside the collection", renamed)
	}
}

// What a level is for is written the same way every level answers it, and an empty answer is a real
// one: the header draws a placeholder for it rather than a gap.
func TestDescribeReachesBothKinds(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "old description", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)

	tree, err := uc.Describe(ctx, collectionID, "  Catalog endpoints  ")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got := only(t, tree).Description; got != "Catalog endpoints" {
		t.Errorf("description = %q, want it trimmed", got)
	}

	tree, err = uc.Describe(ctx, nestedID, "Admin only")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if nested, ok := findCollection(tree, nestedID); !ok || nested.Description != "Admin only" {
		t.Errorf("nested = %+v, want the description it was given", nested)
	}

	// Empty is allowed and means what it says — unlike a name, which cannot be empty.
	tree, err = uc.Describe(ctx, collectionID, "   ")
	if err != nil {
		t.Fatalf("Describe(empty): %v", err)
	}
	if got := only(t, tree).Description; got != "" {
		t.Errorf("description = %q, want it cleared", got)
	}

	// What is too long to be a description is refused, and the row keeps the one it had.
	refused := strings.Repeat("é", maxDescriptionLength+1)
	if _, err := uc.Describe(ctx, collectionID, refused); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a description of %d runes = %v, want ErrNotAllowed", len(refused), err)
	}
	if _, err := uc.Describe(ctx, "not-found", "something"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("describing nothing = %v, want ErrNotFound", err)
	}
}

// copySuffix is what the window sends: a word, in whatever language it is in.
const copySuffix = " (copy)"

func TestDuplicateCopiesTheSubtree(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	// The tree is read again: the row looked up on the next line is the one this call adds.
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: nestedID, Name: "Inside"})
	requestID := findIn(t, tree, nestedID).Items[0].ID

	// A request inside a collection is more than the tree row the copy starts from, and a copy that
	// forgot it would be an empty collection.
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Inside", Method: "PATCH", URL: "https://api.example.com/users/1",
		Headers: []domain.Row{{ID: "h1", Name: "X-Test", Value: "1", Enabled: true}},
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	tree, err := uc.Duplicate(ctx, nestedID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	// The copy lands in the level the original is in: a copy of a nested collection is not a reason to
	// hoist it to the top.
	level := only(t, tree).Children
	if len(level) != 2 {
		t.Fatalf("children = %d, want the copy beside the original", len(level))
	}
	if level[1].Name != "Nested"+copySuffix || level[1].Position != 1 {
		t.Errorf("copy = %+v, want it named and placed next to the original", level[1])
	}
	if level[1].ParentID != collectionID {
		t.Errorf("copy = %+v, want it inside the collection the original is in", level[1])
	}
	if len(level[1].Items) != 1 {
		t.Fatalf("copy = %+v, want its own request", level[1])
	}
	copiedID := level[1].Items[0].ID
	if copiedID == requestID {
		t.Error("the copy kept the original's id")
	}
	copied, err := store.Node(ctx, copiedID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if copied.CollectionID != level[1].ID {
		t.Errorf("copied request = %+v, want it inside the copy", copied)
	}
	if copied.URL != "https://api.example.com/users/1" || len(copied.Headers) != 1 {
		t.Errorf("copied request = %+v, want the request's own fields", copied)
	}
	if copied.Method != "PATCH" {
		t.Errorf("copied method = %q, want the original's", copied.Method)
	}
}

// findIn is a collection of the tree by id, which is how a test reaches one that is not at the top.
func findIn(t *testing.T, tree []domain.Collection, id string) domain.Collection {
	t.Helper()
	collection, ok := findCollection(tree, id)
	if !ok {
		t.Fatalf("collection %s is not in the tree", id)
	}
	return collection
}

// A copy of a request is the request, not its name: the tree row a duplicate starts from carries
// the method and nothing else, and taking that for the content is how a copy comes out empty.
func TestDuplicateCopiesTheRequestItself(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "Request"})
	requestID := only(t, tree).Items[0].ID

	bearer := bearerRef("{{token}}")
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Request", Method: "PATCH", URL: "https://api.example.com/users/1?page=2",
		Description: "about the user",
		Params:      []domain.Row{{ID: "p1", Name: "page", Value: "2", Enabled: true}},
		Headers: []domain.Row{
			{ID: "h1", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
		},
		Body: `{"data": {"type": "users"}}`,
		Auth: bearer,
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	// A copy runs the same code the original did: its scripts come with it, the way its rows do.
	if err := store.SaveScripts(ctx, ws, requestID,
		&domain.Scripts{Post: "console.log('own');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}

	tree, err := uc.Duplicate(ctx, requestID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	copiedID := only(t, tree).Items[1].ID
	copied, err := store.Node(ctx, copiedID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}

	if copied.Name != "Request"+copySuffix || copied.Position != 1 {
		t.Errorf("copy = %+v, want it named and placed beside the original", copied)
	}
	if copied.URL != "https://api.example.com/users/1?page=2" || copied.Method != "PATCH" ||
		copied.Body != `{"data": {"type": "users"}}` {
		t.Errorf("copy = %+v, want the request it was copied from", copied)
	}
	if len(copied.Params) != 1 || len(copied.Headers) != 1 {
		t.Errorf("copy = %+v, want its rows", copied)
	}
	if copied.Description != "about the user" {
		t.Errorf("description = %q, want it carried over", copied.Description)
	}
	if copied.Auth == nil || copied.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want it carried over", copied.Auth)
	}
	// The copy is a second thing: its own rows, so editing one does not edit the other.
	if copied.Params[0].ID == "p1" || copied.Headers[0].ID == "h1" {
		t.Errorf("copy = %+v, want rows of its own", copied)
	}
	if copied.Scripts == nil || copied.Scripts.Post != "console.log('own');" {
		t.Errorf("copy scripts = %+v, want the original's code with it", copied.Scripts)
	}
}

// The card edits a request — its address, its rows, its body — and knows nothing about its scripts:
// a save that dropped them would take the code off a request every time it was touched.
func TestSavingARequestKeepsItsScripts(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "Request"})
	requestID := only(t, tree).Items[0].ID

	if err := store.SaveScripts(ctx, ws, requestID,
		&domain.Scripts{Post: "console.log('own');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Renamed", Method: "GET", URL: "https://api.example.com/users",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	stored, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.Scripts == nil || stored.Scripts.Post != "console.log('own');" {
		t.Errorf("node scripts = %+v, want them where the card left them", stored.Scripts)
	}
}

// A copy behaves the way the original did, so it takes what the collection runs around its requests
// with it. The tree carries no scripts — they belong to the level, not to the row — so a duplicate
// that only copied the row would lose them without saying so.
func TestDuplicateCopiesTheCollectionScripts(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	if err := store.SaveScripts(ctx, ws, collectionID,
		&domain.Scripts{Pre: "console.log('running');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}

	tree, err := uc.Duplicate(ctx, collectionID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	copied, err := store.Scripts(ctx, ws, tree[1].ID)
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	if copied == nil || copied.Pre != "console.log('running');" {
		t.Errorf("copied scripts = %+v, want the original's", copied)
	}
	if original, err := store.Scripts(ctx, ws, collectionID); err != nil || original == nil {
		t.Errorf("the original's scripts = %+v, %v, want them where they were", original, err)
	}
}

func TestDuplicateCopiesACollection(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "description", "")
	collectionID := only(t, tree).ID
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "First"})
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	// A request is a tree row too, and a copy that took the row for the content would lose the
	// request behind it — the same trap the request duplicate had.
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: only(t,
			tree).Items[0].ID, Name: "First", Method: "GET", URL: "https://api.example.com/first",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	tree, err := uc.Duplicate(ctx, collectionID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want 2", len(tree))
	}
	copied := tree[1]
	// The copy lands at the end of the level the original is in: the requests of a collection and the
	// collections inside it are one list, and that list is three long here.
	if copied.Name != "Collection"+copySuffix || copied.Description != "description" ||
		copied.Position != 2 {
		t.Errorf("copy = %+v, want it named and placed after the original", copied)
	}
	if len(copied.Items) != 1 || len(copied.Children) != 1 {
		t.Fatalf("copy = %+v, want its own level: a request and the collection inside it", copied)
	}
	// Everything has to move into the new collection: a row left behind would be visible in both.
	for _, node := range copied.Items {
		if node.CollectionID != copied.ID {
			t.Errorf("request %s still lives in %s", node.ID, node.CollectionID)
		}
	}
	if copied.Children[0].ParentID != copied.ID || copied.Children[0].ID == nestedID {
		t.Errorf("nested copy = %+v, want a collection of its own inside the copy", copied.Children[0])
	}
	if len(copied.Children[0].Items) != 0 {
		t.Errorf("nested copy = %+v, want the original's own level copied with it", copied.Children[0])
	}
	if copied.Items[0].ID == tree[0].Items[0].ID {
		t.Error("the copy kept the original's request id")
	}
	copiedFirst, err := store.Node(ctx, copied.Items[0].ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if copiedFirst.Name != "First" || copiedFirst.URL != "https://api.example.com/first" {
		t.Errorf("copied request = %+v, want the request it was copied from", copiedFirst)
	}
}

// A copy answers for the names the original answered for, and with ids of its own: a variable is
// addressed by its id, and one id naming a variable in two collections names two things.
func TestDuplicateCopiesTheVariablesOfEveryLevel(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	saveVariables(t, uc, collectionID, variable("baseUrl", "https://api.example.com"))
	saveVariables(t, uc, nestedID, variable("version", "v2"))

	tree, err := uc.Duplicate(ctx, collectionID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want the copy beside the original", len(tree))
	}
	copied := tree[1]

	if len(copied.Variables) != 1 || copied.Variables[0].Name != "baseUrl" ||
		copied.Variables[0].Value != "https://api.example.com" {
		t.Fatalf("copy = %+v, want the original's own answer", copied.Variables)
	}
	if copied.Variables[0].ID == tree[0].Variables[0].ID {
		t.Error("the copy kept the original's variable id")
	}

	if len(copied.Children) != 1 || len(copied.Children[0].Variables) != 1 {
		t.Fatalf("nested copy = %+v, want the level inside it copied whole", copied.Children)
	}
	if v := copied.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" {
		t.Errorf("nested variable = %+v, want the one the folder answered", v)
	}
}

func TestDuplicateClipsTheNameAtTheCeiling(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	long := ""
	for len([]rune(long)) < maxNameLength {
		long += "é"
	}
	_, tree, err := uc.CreateCollection(ctx, long, "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	tree, err = uc.Duplicate(ctx, only(t, tree).ID, copySuffix)
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if got := len([]rune(tree[1].Name)); got > maxNameLength {
		t.Errorf("name is %d runes, want at most %d", got, maxNameLength)
	}
}

func TestDeleteTakesTheWholeSubtree(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	// The node has to exist for the cascade below to have something to take: what the tree looks like
	// after this call is what Delete reads.
	if _, _, err := uc.CreateNode(ctx, NodeDraft{CollectionID: nestedID, Name: "Inside"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// A collection inside another is a row of its own, so removing it is what takes its requests with
	// it — through both cascades: the nesting one, and the collection the request belongs to.
	tree, err := uc.Delete(ctx, nestedID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(only(t, tree).Children) != 0 {
		t.Errorf("children = %+v, want it and its request gone", only(t, tree).Children)
	}

	tree, err = uc.Delete(ctx, collectionID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("tree = %+v, want nothing left", tree)
	}
}

func TestSaveNodeKeepsWhereItLives(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: nestedID, Name: "Request"})
	requestID := findIn(t, tree, nestedID).Items[0].ID

	before, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}

	// The card knows nothing about where the request lives, so it sends what it has and the rest must
	// survive: a save that moved a request out of its collection would be a lost tree.
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "  Renamed  ", Method: "delete", URL: "https://api.example.com/users",
		Description: "  about users  ", CollectionID: "",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	after, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if after.Name != "Renamed" || after.Description != "about users" {
		t.Errorf("request = %+v, want the edited name trimmed", after)
	}
	if after.Method != "DELETE" {
		t.Errorf("method = %q, want it upper-cased", after.Method)
	}
	if after.CollectionID != before.CollectionID || after.Position != before.Position ||
		after.CreatedAt != before.CreatedAt {
		t.Errorf("request = %+v, want the place it had (%+v)", after, before)
	}
	if after.CollectionID != nestedID {
		t.Errorf("collection = %q, want it still in the one inside the collection", after.CollectionID)
	}
}

func TestSaveNodeRejectsAnEmptyName(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "Request"})
	requestID := only(t, tree).Items[0].ID

	_, err := uc.SaveNode(ctx, domain.CollectionNode{ID: requestID, Name: "   "})
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("empty name = %v, want ErrNotAllowed", err)
	}
}

func TestMoveNodeTakesTheDropIndex(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	for _, name := range []string{"First", "Second", "Third"} {
		if _, tree, err = uc.CreateNode(ctx,
			NodeDraft{CollectionID: collectionID, Name: name}); err != nil {
			t.Fatalf("CreateNode %s: %v", name, err)
		}
	}
	second := only(t, tree).Items[1].ID

	// Dropped after the row that was behind it: the index counts the level as it looks, the moving row
	// included, so "after the second" is 2 — and the row must not be counted twice.
	if tree, err = uc.MoveNode(ctx, second, collectionID, 2); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}
	order := []string{}
	for _, node := range only(t, tree).Items {
		order = append(order, node.Name)
	}
	if !slices.Equal(order, []string{"First", "Second", "Third"}) {
		t.Errorf("level = %v, want the request where it already stood", order)
	}

	// And to the front, which is the other end of the same index.
	if tree, err = uc.MoveNode(ctx, second, collectionID, 0); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}
	order = []string{}
	for _, node := range only(t, tree).Items {
		order = append(order, node.Name)
	}
	if !slices.Equal(order, []string{"Second", "First", "Third"}) {
		t.Errorf("level = %v, want the dropped request first", order)
	}
	// The rows around it close up rather than keeping the numbers they had.
	for i, node := range only(t, tree).Items {
		if node.Position != int64(i) {
			t.Errorf("%s is at %d, want %d", node.Name, node.Position, i)
		}
	}
}

func TestMoveNodeBetweenCollections(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	_, tree, _ = uc.CreateNode(ctx, NodeDraft{CollectionID: collectionID, Name: "Outside"})
	requestID := only(t, tree).Items[0].ID

	if _, err := uc.MoveNode(ctx, requestID, nestedID, 0); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	moved, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if moved.CollectionID != nestedID {
		t.Errorf("collection = %q, want the one it was dropped into", moved.CollectionID)
	}
	tree, err = uc.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(only(t, tree).Items) != 0 || len(findIn(t, tree, nestedID).Items) != 1 {
		t.Errorf("tree = %+v, want the request inside the nested collection only", only(t, tree))
	}
}

// A collection dropped into itself or into something it holds would be a ring, and a ring is a tree
// nothing can be drawn from: the refusal is what keeps the tree a tree.
func TestMoveCollectionRefusesARing(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)

	for _, parent := range []string{collectionID, nestedID} {
		_, err := uc.MoveCollection(ctx, collectionID, parent, 0)
		if !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("moving %s into %s = %v, want ErrNotAllowed", collectionID, parent, err)
		}
		// The window words the refusal from the code: "it was refused" says nothing about a ring.
		if code := domain.CodeOf(err); code != domain.CodeIntoItself {
			t.Errorf("code = %q, want %q", code, domain.CodeIntoItself)
		}
	}

	// And nothing moved: a refused drop leaves the tree as it was.
	tree, err := uc.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 1 || len(only(t, tree).Children) != 1 {
		t.Errorf("tree = %+v, want it untouched", tree)
	}
	if any := findIn(t, tree, nestedID); any.ParentID != collectionID {
		t.Errorf("nested = %+v, want it still inside the collection", any)
	}
}

func TestMoveCollectionReturnsToTheTopLevel(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)

	tree, err := uc.MoveCollection(ctx, nestedID, "", 0)
	if err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	if len(tree) != 2 || tree[0].ID != nestedID || tree[0].ParentID != "" {
		t.Errorf("tree = %+v, want it back out at the top", tree)
	}
	if len(findIn(t, tree, collectionID).Children) != 0 {
		t.Errorf("tree = %+v, want nothing left inside", tree)
	}
}

// A collection made elsewhere becomes a collection here: its name, its order, and everything inside
// it — with ids minted for everything a file does not write.
func TestImportTakesAWholeCollection(t *testing.T) {
	uc, store := newTestUseCase()
	ctx := context.Background()

	tree, err := uc.Import(ctx, []domain.Collection{{
		Name: "  Imported  ",
		Items: []domain.CollectionNode{
			{Name: "Outside", Position: 1, Method: "GET", URL: "https://api.example.com/out?page=2",
				Params: []domain.Row{{Name: "page", Value: "2", Enabled: true}}},
		},
		Children: []domain.Collection{
			{
				Name: "Nested", Position: 0,
				Items: []domain.CollectionNode{
					{Name: "Inside", Method: "post", URL: "https://api.example.com/inside",
						Headers: []domain.Row{{Name: "Accept", Value: "application/json", Enabled: true}}},
				},
			},
		},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	collection := only(t, tree)
	if collection.Name != "Imported" || collection.Position != 0 {
		t.Errorf("collection = %+v, want the file's name, trimmed", collection)
	}
	if len(collection.Items) != 1 || len(collection.Children) != 1 {
		t.Fatalf("imported = %+v, want the request and the collection inside it", collection)
	}
	nested := collection.Children[0]
	if nested.Name != "Nested" || len(nested.Items) != 1 || nested.Items[0].Name != "Inside" {
		t.Fatalf("nested = %+v, want the file's tree", nested)
	}
	if nested.ParentID != collection.ID {
		t.Errorf("nested = %+v, want the collection it was imported into", nested)
	}
	for _, node := range []domain.CollectionNode{collection.Items[0], nested.Items[0]} {
		if node.ID == "" {
			t.Errorf("request = %+v, want an id minted for it", node)
		}
	}
	// A request belongs to the collection that holds it: the one from inside the file's folder belongs
	// to the collection that folder became.
	if nested.Items[0].CollectionID != nested.ID {
		t.Errorf("the request inside = %+v, want it in the nested collection", nested.Items[0])
	}
	if collection.Items[0].CollectionID != collection.ID {
		t.Errorf("the request at the top = %+v, want it in the imported collection", collection.Items[0])
	}

	// What the tree carries is a row; what was stored is the request.
	stored, err := store.Node(ctx, collection.Items[0].ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.URL != "https://api.example.com/out?page=2" || stored.Method != "GET" {
		t.Errorf("stored = %+v, want the request the file had", stored)
	}
	if len(stored.Params) != 1 || stored.Params[0].ID == "" {
		t.Errorf("params = %+v, want the row with an id the window can address", stored.Params)
	}
	if stored.Position != 1 {
		t.Errorf("stored = %+v, want its place in the imported order", stored)
	}
}

// The tree's menu imports into a collection, not only beside one: a file named a level lands at the
// end of that level, and a name that is no collection at all is refused rather than quietly put at
// the top.
func TestImportLandsInTheNamedCollection(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	parent := only(t, tree).ID

	if _, err = uc.Import(ctx, []domain.Collection{{Name: "From file"}}, parent); err != nil {
		t.Fatalf("Import: %v", err)
	}

	tree, err = uc.store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("tree = %d collections at the top, want the one that was already there", len(tree))
	}
	inside := findIn(t, tree, parent)
	if len(inside.Children) != 1 || inside.Children[0].Name != "From file" {
		t.Errorf("children = %+v, want the import inside %s", inside.Children, parent)
	}

	// An import aimed at a collection that is not there is refused; no level is made up for it.
	nowhere := []domain.Collection{{Name: "From file"}}
	if _, err = uc.Import(ctx, nowhere, "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("import into nothing = %v, want ErrNotFound", err)
	}
}

// A file carries what a level answers for `{{tokens}}`, and an import keeps it: a collection that
// lost them would answer `{{baseUrl}}` with nothing on the other side of the move.
func TestImportKeepsTheVariablesOfEveryLevel(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	tree, err := uc.Import(ctx, []domain.Collection{{
		Name: "Imported",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com", Kind: domain.VariableText, Enabled: true},
		},
		Children: []domain.Collection{{
			Name: "Nested",
			Variables: []domain.Variable{
				{Name: "version", Value: "v2", Kind: domain.VariableText, Enabled: true},
			},
		}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	collection := only(t, tree)
	if len(collection.Variables) != 1 || collection.Variables[0].Value != "https://api.example.com" {
		t.Fatalf("level = %+v, want the file's own answer", collection.Variables)
	}
	// A file writes no ids with them, and a variable the window cannot address is one it cannot edit.
	if collection.Variables[0].ID == "" {
		t.Error("the imported variable has no id")
	}
	if len(collection.Children) != 1 || len(collection.Children[0].Variables) != 1 {
		t.Fatalf("nested = %+v, want the folder's own answer", collection.Children)
	}
	if v := collection.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" || v.ID == "" {
		t.Errorf("nested variable = %+v, want the one the file carried", v)
	}
}

// An export writes down what a tree row does not carry, and what a level answers for `{{tokens}}`
// is one of those: a collection handed on has to mean the same thing on the other side.
func TestFullReadsTheVariablesWhole(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	saveVariables(t, uc, collectionID, variable("baseUrl", "https://api.example.com"))
	saveVariables(t, uc, nestedID, variable("version", "v2"))

	full, err := uc.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if len(full.Variables) != 1 || full.Variables[0].Name != "baseUrl" {
		t.Errorf("level = %+v, want the collection's own answer", full.Variables)
	}
	if len(full.Children) != 1 || len(full.Children[0].Variables) != 1 {
		t.Fatalf("nested = %+v, want the folder read whole too", full.Children)
	}
	if v := full.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" {
		t.Errorf("nested variable = %+v, want the one the folder answers", v)
	}
}

// An export writes down what a tree row does not carry: Full reads the nodes whole, and a request
// on its own is the collection an export of one request makes.
func TestFullReadsTheRequestsWhole(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "description", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	_, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID, Name: "Request", Method: "GET",
		URL: "https://api.example.com/users", Body: `{"a": 1}`,
		Headers: []domain.Row{{Name: "Accept", Value: "application/vnd.api+json", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	requestID := only(t, tree).Items[0].ID

	full, err := uc.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if full.Name != "Collection" || full.Description != "description" || len(full.Items) != 1 {
		t.Fatalf("full = %+v, want the collection and its request", full)
	}
	if full.Items[0].Body != `{"a": 1}` || len(full.Items[0].Headers) != 1 {
		t.Errorf("request = %+v, want it read whole", full.Items[0])
	}

	single, err := uc.Full(ctx, requestID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if single.Name != "Request" || len(single.Items) != 1 ||
		single.Items[0].URL != "https://api.example.com/users" {
		t.Errorf("single = %+v, want a collection of the one request", single)
	}
}

// An export of a collection carries the collections inside it: what a file writes as folders is the
// levels of the tree, and one left out would be a file that is not the collection.
func TestFullReadsNestedCollectionsWhole(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, uc, "Nested", collectionID)
	if _, _, err := uc.CreateNode(ctx, NodeDraft{
		CollectionID: nestedID, Name: "Inside", Method: "GET", URL: "https://api.example.com/inside",
		Body: `{"b": 2}`,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	full, err := uc.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if len(full.Children) != 1 || full.Children[0].Name != "Nested" {
		t.Fatalf("full = %+v, want the collection and the one inside it", full)
	}
	if len(full.Children[0].Items) != 1 || full.Children[0].Items[0].Body != `{"b": 2}` {
		t.Errorf("nested = %+v, want its request read whole", full.Children[0])
	}
}

// A collection is read with the levels it sits inside of, itself first: what asks for it is a
// reader of one folder of a page, and the answer is the chain from that folder to the root
// of its space.
func TestPathOfReadsTheLevelsACollectionSitsInsideOf(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	root := only(t, tree).ID
	middle := nestCollection(t, uc, "middle", root)
	leaf := nestCollection(t, uc, "Leaf", middle)

	path, err := uc.PathOf(ctx, domain.WorkspacePersonalID, leaf)
	if err != nil {
		t.Fatalf("PathOf: %v", err)
	}

	want := []domain.Collection{
		{ID: leaf, Name: "Leaf"},
		{ID: middle, Name: "middle"},
		{ID: root, Name: "Collection"},
	}
	if !slices.EqualFunc(path, want, func(a, b domain.Collection) bool {
		return a.ID == b.ID && a.Name == b.Name
	}) {
		t.Errorf("path = %+v, want the collection and the levels above it", path)
	}
}

// A request and a collection are named in the same id space, and only one of them has a path: an id
// the tree never meets is answered with nothing rather than with a refusal nobody asked for.
func TestPathOfAnswersNothingForAnIDTheTreeDoesNotHold(t *testing.T) {
	uc, _ := newTestUseCase()
	ctx := context.Background()

	_, tree, _ := uc.CreateCollection(ctx, "Collection", "", "")
	node, _, err := uc.CreateNode(ctx, NodeDraft{
		CollectionID: only(t, tree).ID, Name: "Request", Method: "GET",
		URL: "https://api.example.com/v1",
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	path, err := uc.PathOf(ctx, domain.WorkspacePersonalID, node.ID)
	if err != nil {
		t.Fatalf("PathOf: %v", err)
	}
	if len(path) != 0 {
		t.Errorf("path = %+v, want nothing for a request", path)
	}
}

func TestNodeInAMissingTree(t *testing.T) {
	uc, _ := newTestUseCase()

	if _, err := uc.Node(context.Background(), "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing node = %v, want ErrNotFound", err)
	}
}

// fakeEnvironment is the environment a run is said to have gone out under. A run keeps that name,
// so the tests that read a run back look at what this answered when it started.
type fakeEnvironment struct {
	mu   sync.Mutex
	name string
	err  error
}

func newFakeEnvironment() *fakeEnvironment {
	return &fakeEnvironment{}
}

func (f *fakeEnvironment) named(name string) *fakeEnvironment {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.name = name
	return f
}

func (f *fakeEnvironment) ActiveEnvironment(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.name, f.err
}
