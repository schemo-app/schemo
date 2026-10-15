package collection

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// runFixture is a collection with a request, a collection inside it holding two more, and a request
// after that one — the shape that says whether the walk goes depth first and in the tree's order.
type runFixture struct {
	uc           *UseCase
	store        *fakeStore
	sender       *fakeSender
	notifier     *fakeNotifier
	assertions   *fakeAssertions
	collectionID string
	nestedID     string
	requests     map[string]string // name → url
}

func setupRunnable(t *testing.T) *runFixture {
	t.Helper()
	uc, store, sender, notifier, asserted := newTestRun()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	requests := map[string]string{}
	add := func(collectionID, name, url string) string {
		t.Helper()
		_, tree, err := uc.CreateNode(ctx, NodeDraft{
			CollectionID: collectionID, Name: name, Method: "GET",
		})
		if err != nil {
			t.Fatalf("CreateNode %s: %v", name, err)
		}
		node := findInTree(t, tree, name)
		if _, err := uc.SaveNode(ctx, domain.CollectionNode{
			ID: node.ID, Name: name, Method: "GET", URL: url,
			Headers: []domain.Row{
				{ID: node.ID + "-on", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
				{ID: node.ID + "-off", Name: "X-Off", Value: "no", Enabled: false},
			},
		}); err != nil {
			t.Fatalf("SaveNode %s: %v", name, err)
		}
		requests[name] = url
		sender.reply(url, 200, 1000)
		return node.ID
	}

	add(collectionID, "First", "https://api.example.com/first")
	// Dropped in second, so that the level is a request, a collection and then a request: the shape
	// that says whether the walk goes in the drawn order rather than by kind.
	nestedID := nestCollectionAt(t, uc, "Nested", collectionID, 1)
	add(nestedID, "Second", "https://api.example.com/second")
	add(nestedID, "Third", "https://api.example.com/third")
	add(collectionID, "Fourth", "https://api.example.com/fourth")

	return &runFixture{
		uc: uc, store: store, sender: sender, notifier: notifier, assertions: asserted,
		collectionID: collectionID, nestedID: nestedID, requests: requests,
	}
}

func findInTree(t *testing.T, tree []domain.Collection, name string) domain.CollectionNode {
	t.Helper()
	walk := func(nodes []domain.CollectionNode) (domain.CollectionNode, bool) {
		for _, node := range nodes {
			if node.Name == name {
				return node, true
			}
		}
		return domain.CollectionNode{}, false
	}
	var inCollection func([]domain.Collection) (domain.CollectionNode, bool)
	inCollection = func(collections []domain.Collection) (domain.CollectionNode, bool) {
		for _, collection := range collections {
			if found, ok := walk(collection.Items); ok {
				return found, true
			}
			if found, ok := inCollection(collection.Children); ok {
				return found, true
			}
		}
		return domain.CollectionNode{}, false
	}
	if found, ok := inCollection(tree); ok {
		return found
	}
	t.Fatalf("no request named %s in the tree", name)
	return domain.CollectionNode{}
}

func TestRunWalksTheSubtreeDepthFirst(t *testing.T) {
	r := setupRunnable(t)

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	want := []string{
		"https://api.example.com/first",
		"https://api.example.com/second",
		"https://api.example.com/third",
		"https://api.example.com/fourth",
	}
	got := r.sender.urls()
	if len(got) != len(want) {
		t.Fatalf("sent %d requests, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %s, want %s — the run walks the tree in the order it is drawn", i, got[i],
				want[i])
		}
	}

	if run.Passed != 4 || run.Failed != 0 {
		t.Errorf("run = %d passed, %d failed, want all four passed", run.Passed, run.Failed)
	}
	if len(run.Results) != 4 {
		t.Fatalf("run kept %d results, want 4", len(run.Results))
	}
	for i, result := range run.Results {
		if result.Position != int64(i) {
			t.Errorf("result %d is at position %d", i, result.Position)
		}
		if !result.OK || result.Status == nil || *result.Status != 200 || result.DurationUs != 1000 {
			t.Errorf("result %d = %+v, want a 200 with its duration", i, result)
		}
	}
	if run.DurationUs < 0 || run.FinishedAt == 0 {
		t.Errorf("run = %+v, want it closed with a time", run)
	}
}

func TestRunSendsWhatTheRequestIsMadeOf(t *testing.T) {
	r := setupRunnable(t)

	if _, err := r.uc.Run(context.Background(), r.collectionID, r.nestedID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.notifier.runFinished(t)

	sent := r.sender.sent
	if len(sent) != 2 {
		t.Fatalf("sent %d requests, want the nested collection's two", len(sent))
	}
	if sent[0].Method != "GET" || sent[0].URL != r.requests["Second"] {
		t.Errorf("request = %+v, want the saved method and address", sent[0])
	}
	// A row a person switched off travels nowhere, and the row that is on stays a pair.
	if len(sent[0].Headers) != 1 || sent[0].Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, want only the enabled row", sent[0].Headers)
	}
}

func TestRunOfANestedCollectionTouchesOnlyItsSubtree(t *testing.T) {
	r := setupRunnable(t)

	if _, err := r.uc.Run(context.Background(), r.collectionID, r.nestedID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	if len(run.Results) != 2 || run.Passed != 2 {
		t.Errorf("run = %+v, want the nested collection's two requests and nothing else", run)
	}
	if run.NodeID != r.nestedID {
		t.Errorf("run node = %q, want the nested collection it was started from", run.NodeID)
	}
}

func TestRunOfASingleRequest(t *testing.T) {
	r := setupRunnable(t)
	node := findInTree(t, mustTree(t, r), "Third")

	if _, err := r.uc.Run(context.Background(), r.collectionID, node.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	if len(run.Results) != 1 || run.Results[0].NodeID != node.ID {
		t.Errorf("run = %+v, want the one request it names", run)
	}
}

// What a run hands over is a request with the authorization of the level above it already resolved:
// the walk up the tree is the run's own, and by the time a request leaves, where it sat is no
// longer known to whoever sends it.
func TestRunSendsTheInheritedAuth(t *testing.T) {
	r := setupRunnable(t)
	ctx := context.Background()

	if _, err := r.uc.SaveAuth(ctx, r.collectionID, bearer("Collection")); err != nil {
		t.Fatalf("SaveAuth collection: %v", err)
	}
	if _, err := r.uc.SaveAuth(ctx, r.nestedID, bearer("Nested")); err != nil {
		t.Fatalf("SaveAuth nested: %v", err)
	}

	if _, err := r.uc.Run(ctx, r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.notifier.runFinished(t)

	sent := r.sender.sent
	if len(sent) != 4 {
		t.Fatalf("sent %d requests, want the whole collection", len(sent))
	}
	want := []string{"Collection", "Nested", "Nested", "Collection"}
	for i, token := range want {
		auth := sent[i].Auth
		if auth == nil || auth.Answer("token") != token {
			t.Errorf("request %d went out with %+v, want %q", i, auth, token)
		}
	}
}

// A folder two levels down is a level of its own, and the walk that finds it is the walk that
// collects what stands above it: a run started from the second level down goes out with the
// authorization and the variables of every level between it and the root, not the root's alone.
// Both are asserted here because both travel the same path to the sender.
func TestRunFromAFolderTwoLevelsDownCarriesEveryLevelAbove(t *testing.T) {
	r := setupRunnable(t)
	ctx := context.Background()

	deepID := nestCollectionAt(t, r.uc, "Deep", r.nestedID, 1)
	const url = "https://api.example.com/deep"
	if _, _, err := r.uc.CreateNode(ctx, NodeDraft{
		CollectionID: deepID, Name: "Deep", Method: "GET",
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	deep := findInTree(t, mustTree(t, r), "Deep")
	if _, err := r.uc.SaveNode(ctx, domain.CollectionNode{
		ID: deep.ID, Name: "Deep", Method: "GET", URL: url,
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	r.sender.reply(url, 200, 1000)

	if _, err := r.uc.SaveAuth(ctx, r.collectionID, bearer("root")); err != nil {
		t.Fatalf("SaveAuth root: %v", err)
	}
	if _, err := r.uc.SaveAuth(ctx, r.nestedID, bearer("middle")); err != nil {
		t.Fatalf("SaveAuth middle: %v", err)
	}
	saveVariables(t, r.uc, r.nestedID, variable("page", "2"))

	if _, err := r.uc.Run(ctx, r.collectionID, deepID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.notifier.runFinished(t)

	sent := r.sender.requests()
	if len(sent) != 1 || sent[0].URL != url {
		t.Fatalf("sent %v, want the deep folder's one request", r.sender.urls())
	}
	if auth := sent[0].Auth; auth == nil || auth.Answer("token") != "middle" {
		t.Errorf("went out with %+v, want the middle level's authorization", sent[0].Auth)
	}
	if len(sent[0].Variables) != 1 || sent[0].Variables[0].Name != "page" {
		t.Errorf("variables = %+v, want the middle level's own", sent[0].Variables)
	}
}

func TestRunGoesOnAfterAFailure(t *testing.T) {
	r := setupRunnable(t)
	r.sender.fail(r.requests["Second"])

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	// The whole point of a run: one endpoint that is down does not hide the three behind it.
	if len(run.Results) != 4 {
		t.Fatalf("run reached %d requests, want all four", len(run.Results))
	}
	if run.Passed != 3 || run.Failed != 1 {
		t.Errorf("run = %d passed, %d failed, want 3 and 1", run.Passed, run.Failed)
	}
	failed := run.Results[1]
	if failed.OK || failed.Status != nil || failed.Error != "server did not respond" {
		t.Errorf("failed result = %+v, want it without a status and with the reason", failed)
	}
	if run.Results[2].OK != true {
		t.Errorf("the request after a failed one = %+v, want it sent", run.Results[2])
	}
}

// A request a pre-request script kept from going out is neither a pass nor a failure: the row says
// which of the three happened, so a run that skipped half its requests is not a run that failed
// half of them.
func TestRunCountsASkippedRequestAsNeither(t *testing.T) {
	r := setupRunnable(t)
	r.sender.skip(r.requests["Second"])

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	if run.Passed != 3 || run.Failed != 0 {
		t.Errorf("run = %d passed, %d failed, want 3 and none: the skipped one is neither", run.Passed,
			run.Failed)
	}
	skipped := run.Results[1]
	if !skipped.Skipped || skipped.Status != nil || skipped.OK {
		t.Errorf("skipped result = %+v, want a row that says it was not sent", skipped)
	}
	// The flag is the reason; the row is worded where the language is known, so that a run written
	// today reads in whatever language the window is in tomorrow.
	if skipped.Error != "" {
		t.Errorf("error = %q, want no text on a row the flag already explains", skipped.Error)
	}
	// The run asks about every request it walks — whether one of them goes out is decided where the
	// scripts are, not here, and a run that skipped a request still reached it.
	if len(r.sender.sent) != 4 {
		t.Errorf("the run reached %d requests, want all four", len(r.sender.sent))
	}
}

// What each row of a run opens: the record that request produced. The fake sender mints an id per
// answer, and the row has to carry it — otherwise a row can say which endpoint failed and nothing
// about what it answered.
func TestARunRowNamesTheRecordItProduced(t *testing.T) {
	r := setupRunnable(t)

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	for i, result := range run.Results {
		if result.RecordID == "" {
			t.Errorf("result %d = %+v, want the record it produced", i, result)
		}
	}
	// Two requests, two records: a row that named the same record twice would open the wrong answer.
	if run.Results[0].RecordID == run.Results[1].RecordID {
		t.Error("two rows name one record")
	}

	// And the run the window reads back says the same: the link is written, not just published.
	last, err := r.uc.LastRun(context.Background(), r.collectionID, "")
	if err != nil || last == nil {
		t.Fatalf("LastRun = %+v, %v", last, err)
	}
	if len(last.Results) != len(run.Results) || last.Results[0].RecordID != run.Results[0].RecordID {
		t.Errorf("runs read back = %+v, want the same records", last.Results)
	}
}

// What the scripts around each request asserted, on the row of that request: the table draws «3 of
// 4» out of the row itself, and a report per row would be a call per request to draw one. A request
// nothing was written for is 0 of 0 — nothing was asserted, which is not the same as a failure.
func TestRunCarriesTheAssertionsOfEachRow(t *testing.T) {
	r := setupRunnable(t)
	r.assertions.asserts("rec-"+r.requests["Second"], 3, 4)

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	byRecord := map[string]domain.CollectionRunResult{}
	for _, result := range run.Results {
		byRecord[result.RecordID] = result
	}
	written := byRecord["rec-"+r.requests["Second"]]
	if written.AssertionsPassed != 3 || written.AssertionsTotal != 4 {
		t.Errorf("row = %+v, want the three of the four its report holds", written)
	}
	quiet := byRecord["rec-"+r.requests["First"]]
	if quiet.AssertionsPassed != 0 || quiet.AssertionsTotal != 0 {
		t.Errorf("row = %+v, want nothing asserted about it", quiet)
	}
	// The counts are written, not only published: the page reads the last run back when it opens.
	last, err := r.uc.LastRun(context.Background(), r.collectionID, "")
	if err != nil || last == nil {
		t.Fatalf("LastRun = %+v, %v", last, err)
	}
	for _, result := range last.Results {
		if result.RecordID == written.RecordID && result.AssertionsTotal != 4 {
			t.Errorf("run read back = %+v, want the assertions on the row", result)
		}
	}
}

func TestRunStopWaitsForTheRequestInFlight(t *testing.T) {
	r := setupRunnable(t)

	// Stopping from inside a send is the harshest moment: the request is out, and the run must read
	// it to the end rather than throw its answer away.
	stopped := false
	r.sender.hook(func(RunRequest) {
		if !stopped {
			stopped = true
			r.uc.Stop()
		}
	})

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	if len(run.Results) != 1 {
		t.Errorf("run kept %d results, want the one request it was told to finish", len(run.Results))
	}
	if len(r.sender.urls()) != 1 {
		t.Errorf("sent %+v, want nothing after the stop", r.sender.urls())
	}
}

func TestRunRefusesASecondOne(t *testing.T) {
	r := setupRunnable(t)

	// A sender that holds the run inside its first request, so the second call meets a run that is
	// still going.
	held := make(chan struct{}, 1)
	release := make(chan struct{})
	r.sender.hook(func(RunRequest) {
		select {
		case held <- struct{}{}:
		default:
		}
		<-release
	})

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-held

	if _, err := r.uc.Run(context.Background(), r.collectionID, r.nestedID); !errors.Is(err,
		domain.ErrNotAllowed) {
		t.Errorf("a second run = %v, want ErrNotAllowed", err)
	}
	if stopped := r.uc.Stop(); !stopped {
		t.Error("Stop reported nothing to stop while a run was going")
	}

	close(release)
	r.notifier.runFinished(t)

	// With the run over, the next one is welcome again.
	r.sender.hook(nil)
	if stopped := r.uc.Stop(); stopped {
		t.Error("Stop reported a run after the run had finished")
	}
	if _, err := r.uc.Run(context.Background(), r.collectionID, r.nestedID); err != nil {
		t.Errorf("a run after the first one: %v", err)
	}
	r.notifier.runFinished(t)
}

func TestRunRefusesAnEmptySubtree(t *testing.T) {
	uc, store, _, _, _ := newTestRun()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Empty", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	if _, err := uc.Run(ctx, collectionID, ""); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("run of an empty collection = %v, want ErrNotAllowed", err)
	}
	// A refused run leaves nothing behind: no run row, and a next run is not told one is going.
	if len(store.runs) != 0 {
		t.Errorf("stored %d runs, want none", len(store.runs))
	}
	if _, err := uc.Run(ctx, "not-found", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("run of a missing collection = %v, want ErrNotFound", err)
	}

	// A request of another collection is a stale selection, not an empty run: the two are told apart
	// because the window shows them differently.
	_, other, err := uc.CreateCollection(ctx, "Other", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	otherCollection, ok := findCollection(other, other[len(other)-1].ID)
	if !ok {
		t.Fatal("the second collection is not in the tree")
	}
	otherID := otherCollection.ID
	_, tree, err = uc.CreateNode(ctx, NodeDraft{CollectionID: otherID, Name: "Foreign"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	stranger := findInTree(t, tree, "Foreign").ID
	if _, err := uc.Run(ctx, collectionID, stranger); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("run of a node that is not in this collection = %v, want ErrNotFound", err)
	}
}

func TestLastRunReadsWhatTheRunWrote(t *testing.T) {
	r := setupRunnable(t)
	ctx := context.Background()

	last, err := r.uc.LastRun(ctx, r.collectionID, "")
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last != nil {
		t.Fatalf("LastRun = %+v, want nothing before the first run", last)
	}

	if _, err := r.uc.Run(ctx, r.collectionID, r.nestedID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.notifier.runFinished(t)

	last, err = r.uc.LastRun(ctx, r.collectionID, r.nestedID)
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last == nil || len(last.Results) != 2 || last.Passed != 2 {
		t.Fatalf("LastRun = %+v, want the nested collection's run with its rows", last)
	}
	if last.Results[0].NodeID != findInTree(t, mustTree(t, r), "Second").ID {
		t.Errorf("first result = %+v, want the nested collection's first request", last.Results[0])
	}

	// A run of a nested collection is not a run of the one around it: the overview of one must not
	// draw the other's summary.
	collection, err := r.uc.LastRun(ctx, r.collectionID, "")
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if collection != nil {
		t.Errorf("the collection reports a run of the nested collection: %+v", collection)
	}
}

func TestRunPublishesEveryRequestAsItGoes(t *testing.T) {
	r := setupRunnable(t)

	if _, err := r.uc.Run(context.Background(), r.collectionID, r.nestedID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.notifier.runFinished(t)

	progress := []RunProgress{}
	r.notifier.mu.Lock()
	for _, event := range r.notifier.events {
		if event.topic == TopicRunProgress {
			progress = append(progress, event.payload.(RunProgress))
		}
	}
	r.notifier.mu.Unlock()

	// The window draws the status bar from these, so they have to count up and end at the total —
	// a run that reported the total only at the end would show "0 / 2" until it was over.
	if len(progress) != 2 {
		t.Fatalf("published %d progress events, want one per request", len(progress))
	}
	for i, event := range progress {
		if event.Done != i+1 || event.Total != 2 {
			t.Errorf("progress %d = %d of %d, want %d of 2", i, event.Done, event.Total, i+1)
		}
		// The row itself travels with the count: the pane draws each finished request as it comes, and
		// the level the run was started from is what tells a window looking elsewhere that it is not
		// this run's.
		if event.Result.NodeID == "" || event.CollectionID != r.collectionID ||
			event.NodeID != r.nestedID {
			t.Errorf("progress %d = %+v, want the row and the level it came from", i, event)
		}
	}
	if last := r.notifier.topics()[len(r.notifier.topics())-1]; last != TopicRunFinished {
		t.Errorf("the last event is %s, want the finished run", last)
	}
}

func mustTree(t *testing.T, r *runFixture) []domain.Collection {
	t.Helper()
	tree, err := r.uc.Tree(context.Background())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	return tree
}

// switchingScope answers with a different space every time it is asked, and counts the asks, so a
// test can name the space a call was made in without knowing how many calls came before it. A run
// that reads the pointer again mid-flight is what this catches.
type switchingScope struct{ asked int }

func (s *switchingScope) ActiveWorkspace(context.Context) (string, error) {
	s.asked++
	return fmt.Sprintf("space-%d", s.asked), nil
}

// A run resolves the workspace once, when it starts, and hands it to the sender with every request.
// A user who switches spaces while fifty requests are going out must not have the last of them
// filed under the other one — and the space is the run's own decision, so the sender is told it
// rather than left to ask.
func TestEveryRequestOfARunCarriesTheSpaceTheRunStartedIn(t *testing.T) {
	store := newFakeStore()
	sender := newFakeSender()
	notifier := newFakeNotifier()
	scope := &switchingScope{}
	uc := NewUseCase(store, scope, sender, newFakeAssertions(), newFakeEnvironment(), notifier,
		platform.NewIDGen())
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	for _, name := range []string{"First", "Second"} {
		if _, tree, err = uc.CreateNode(ctx, NodeDraft{
			CollectionID: collectionID, Name: name, Method: "GET",
		}); err != nil {
			t.Fatalf("CreateNode %s: %v", name, err)
		}
		url := "https://api.example.com/" + name
		node := findInTree(t, tree, name)
		if _, err := uc.SaveNode(ctx, domain.CollectionNode{
			ID: node.ID, Name: name, Method: "GET", URL: url,
		}); err != nil {
			t.Fatalf("SaveNode %s: %v", name, err)
		}
		sender.reply(url, 200, 1000)
	}

	// The space the run itself will be handed: every call above has taken one of its own.
	runSpace := fmt.Sprintf("space-%d", scope.asked+1)
	if _, err := uc.Run(ctx, collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	notifier.runFinished(t)

	requests := sender.requests()
	if len(requests) != 2 {
		t.Fatalf("sent %d requests, want 2", len(requests))
	}
	for i, req := range requests {
		if req.Workspace != runSpace {
			t.Errorf("request %d went out in %q, want %q — the run resolves the space once",
				i, req.Workspace, runSpace)
		}
	}
}

// A saved request whose body is a form or a file goes out as that form or file: the node's
// BodyKind, its rows and its path travel with it, because the text alone cannot be sent as a
// multipart body.
func TestARunsRequestCarriesWhatItsBodyWasMadeOf(t *testing.T) {
	store := newFakeStore()
	sender := newFakeSender()
	notifier := newFakeNotifier()
	uc := NewUseCase(store, fakeScope{}, sender, newFakeAssertions(), newFakeEnvironment(),
		notifier, platform.NewIDGen())
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	if _, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID, Name: "Upload", Method: "POST",
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	const url = "https://api.example.com/upload"
	node := findInTree(t, tree, "Upload")
	form := []domain.FormRow{{ID: "row-1", Name: "field", Value: "value", Enabled: true}}
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: node.ID, Name: "Upload", Method: "POST", URL: url,
		Body: "field=value", BodyKind: domain.BodyForm, Form: form, BodyFile: "/tmp/report.pdf",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	sender.reply(url, 200, 1000)

	if _, err := uc.Run(ctx, collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	notifier.runFinished(t)

	requests := sender.requests()
	if len(requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(requests))
	}
	got := requests[0]
	if got.BodyKind != domain.BodyForm {
		t.Errorf("body kind = %q, want %q — a form body sent as text goes out as the wrong thing",
			got.BodyKind, domain.BodyForm)
	}
	if len(got.Form) != 1 || got.Form[0].Name != "field" || got.Form[0].Value != "value" {
		t.Errorf("form rows = %+v, want the one the node holds", got.Form)
	}
	if got.BodyFile != "/tmp/report.pdf" {
		t.Errorf("body file = %q, want the node's path", got.BodyFile)
	}
}

// A request saved against an environment of its own is resolved against it wherever it is sent
// from. A run is the case that makes this matter: it reaches requests the window is not looking at,
// so the node's pin has to travel with the request — without it a collection pointed at one
// environment would quietly go out under whatever the window is on.
func TestARunsRequestCarriesTheEnvironmentItWasPinnedTo(t *testing.T) {
	store := newFakeStore()
	sender := newFakeSender()
	notifier := newFakeNotifier()
	uc := NewUseCase(store, fakeScope{}, sender, newFakeAssertions(), newFakeEnvironment(),
		notifier, platform.NewIDGen())
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	if _, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID, Name: "Production", Method: "GET",
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	const url = "https://api.example.com/articles"
	node := findInTree(t, tree, "Production")
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: node.ID, Name: "Production", Method: "GET", URL: url, EnvironmentID: "env-prod",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	sender.reply(url, 200, 1000)

	if _, err := uc.Run(ctx, collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	notifier.runFinished(t)

	requests := sender.requests()
	if len(requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(requests))
	}
	if requests[0].EnvironmentID != "env-prod" {
		t.Errorf("environment = %q, want the one the request was saved against",
			requests[0].EnvironmentID)
	}
}

// What a run went out under is kept with the run. The page that reports on it is read later, under
// whatever environment happens to be on screen then, and drawing that name on an older run would be
// saying it ran under something it never saw.
func TestARunKeepsTheEnvironmentItRanUnder(t *testing.T) {
	store := newFakeStore()
	sender := newFakeSender()
	notifier := newFakeNotifier()
	environment := newFakeEnvironment().named("Local · dev")
	uc := NewUseCase(store, fakeScope{}, sender, newFakeAssertions(), environment, notifier,
		platform.NewIDGen())
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Collection", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	url := "https://api.example.com/articles"
	if _, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID, Name: "Articles", Method: "GET",
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	node := findInTree(t, tree, "Articles")
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: node.ID, Name: "Articles", Method: "GET", URL: url,
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	sender.reply(url, 200, 1000)

	if _, err := uc.Run(ctx, collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	notifier.runFinished(t)

	if len(store.runs) != 1 {
		t.Fatalf("runs = %d, want the one that was started", len(store.runs))
	}
	if got := store.runs[0].Environment; got != "Local · dev" {
		t.Errorf("environment = %q, want the one the run went out under", got)
	}

	// And it survives being read back: the overview reads the run, not the window.
	last, err := uc.LastRun(ctx, collectionID, "")
	if err != nil || last == nil {
		t.Fatalf("LastRun = %v, %v", last, err)
	}
	if last.Environment != "Local · dev" {
		t.Errorf("last run environment = %q, want what was kept", last.Environment)
	}
}

// A request that names a variable nothing answers fails the way a server that never answered does:
// its row carries the refusal — the window is what words a code — and the requests behind it in the
// run still go out. Stopping the run at the first one would hide the twenty behind it.
func TestARunRowCarriesARefusalAndTheRunGoesOn(t *testing.T) {
	r := setupRunnable(t)
	refusal := domain.Refuse(domain.CodeVariableMissing, domain.ErrNotAllowed,
		domain.Args{"n": "1", "names": "var3"})
	r.sender.refuse("https://api.example.com/second", refusal)

	if _, err := r.uc.Run(context.Background(), r.collectionID, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	run := r.notifier.runFinished(t)

	if run.Passed != 3 || run.Failed != 1 {
		t.Errorf("run = %d passed, %d failed, want the one refused and the three others sent",
			run.Passed, run.Failed)
	}
	var refused domain.CollectionRunResult
	for _, result := range run.Results {
		if result.Error != "" {
			refused = result
		}
	}
	if refused.Failure == nil || refused.Failure.Code != domain.CodeVariableMissing {
		t.Fatalf("row = %+v, want the refusal behind its error", refused)
	}
	if refused.Failure.Args["names"] != "var3" || refused.Failure.Args["n"] != "1" {
		t.Errorf("row args = %+v, want the names and their count for the sentence", refused.Failure.Args)
	}
	if refused.Status != nil || refused.RecordID != "" {
		t.Errorf("row = %+v, want a request that never went out", refused)
	}
	if len(r.sender.urls()) != 4 {
		t.Errorf("the sender was handed %v, want all four — the refusal is not a stop",
			r.sender.urls())
	}
}
