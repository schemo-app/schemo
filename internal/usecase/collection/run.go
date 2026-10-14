package collection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"schemo/internal/domain"
)

// Event topics. A run reports itself twice: once per request that finished, which is both the
// progress and the row, and once at the end.
const (
	TopicRunProgress = "collection:run-progress"
	TopicRunFinished = "collection:run-finished"
)

// Run executes everything under a node, one request after another, and answers with the id of the
// run at once: a collection of fifty requests is a minute of waiting, and the window is told how it
// is going rather than holding a promise.
//
// A folder is run as itself — a run of the whole collection is a run of the collection, and the two
// are told apart by what the run was started from.
func (u *UseCase) Run(ctx context.Context, collectionID string, nodeID string) (string, error) {
	// One run at a time: two of them would interleave their rows into the same overview, and the
	// button that started the second one says "Stop" rather than "Run".
	if !u.running.CompareAndSwap(false, true) {
		return "", domain.Refuse(domain.CodeRunInProgress, domain.ErrNotAllowed, nil)
	}

	// The workspace is read here and carried into the goroutine below as a value: a run outlives the
	// call that started it, and asking again down there would file its results under whatever space
	// the user had switched to by the time the last request came back.
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		u.running.Store(false)
		return "", err
	}

	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		u.running.Store(false)
		return "", err
	}

	requests, err := requestsUnder(tree, collectionID, nodeID)
	if err != nil {
		u.running.Store(false)
		return "", err
	}
	if len(requests) == 0 {
		// Nothing to send is a thing the window asks by mistake — an empty folder, a collection not
		// filled yet — and a run that reports "0 / 0" reads as a failure of the app.
		u.running.Store(false)
		return "", domain.Refuse(domain.CodeNodeHasNoRequests, domain.ErrNotAllowed, nil)
	}

	// What the run goes out under is read here, once, and kept with it: the window may be on another
	// environment by the time the page is read again, and the run is not. A failure to answer leaves
	// the name empty, which reads as "nothing known" rather than as the wrong environment.
	environment, _ := u.environment.ActiveEnvironment(ctx)

	run := domain.CollectionRun{
		ID:           u.ids(),
		CollectionID: collectionID,
		NodeID:       nodeID,
		Environment:  environment,
		StartedAt:    time.Now().UnixMilli(),
		Results:      []domain.CollectionRunResult{},
	}
	// The run is written before anything is sent: its results hang off it by a foreign key, and a
	// run the window was told about must exist even if the app is closed mid-flight.
	if err := u.store.SaveRun(ctx, run); err != nil {
		u.running.Store(false)
		return "", err
	}

	u.stopped.Store(false)
	go u.execute(workspace, run, requests)
	return run.ID, nil
}

// Stop asks the run to stop. The request already in flight is read to the end: it is on its way,
// and its answer is a row the run is owed.
func (u *UseCase) Stop() bool {
	if !u.running.Load() {
		return false
	}
	u.stopped.Store(true)
	return true
}

// LastRun is what the overview draws when it opens. Absent is an answer — nothing has been run here
// yet — and it is nil rather than an error, because a collection nobody has run is not a problem.
func (u *UseCase) LastRun(
	ctx context.Context,
	collectionID string,
	nodeID string,
) (*domain.CollectionRun, error) {
	run, ok, err := u.store.LastRun(ctx, collectionID, nodeID)
	if err != nil || !ok {
		return nil, err
	}
	return &run, nil
}

// execute walks the requests and sends them one at a time. It runs on a context of its own: the
// caller's ends when the frontend call returns, and a run outlives the call that started it.
func (u *UseCase) execute(workspace string, run domain.CollectionRun, requests []runnable) {
	ctx := context.Background()
	defer u.running.Store(false)

	started := time.Now()
	for position, item := range requests {
		if u.stopped.Load() {
			break
		}

		result := u.attempt(ctx, workspace, item, int64(position), run.ID)
		// The row goes in before it is counted: a run whose results cannot be written is over, and
		// a summary that counts a row the database does not have is a summary that lies.
		if err := u.store.AppendRunResult(ctx, run.ID, result); err != nil {
			break
		}
		run.Results = append(run.Results, result)
		switch {
		case result.Skipped:
			// Neither a pass nor a failure: a script kept this request from going out, and counting
			// it either way would say something that did not happen.
		case result.OK:
			run.Passed++
		default:
			run.Failed++
		}
		u.notifier.Publish(TopicRunProgress, RunProgress{
			RunID:        run.ID,
			CollectionID: run.CollectionID,
			NodeID:       run.NodeID,
			Environment:  run.Environment,
			Done:         len(run.Results),
			Total:        len(requests),
			Result:       result,
		})
	}

	run.DurationUs = time.Since(started).Microseconds()
	run.FinishedAt = time.Now().UnixMilli()
	// A run that could not be closed is still a run the window is waiting to hear the end of: it is
	// told what happened, and the next time the overview opens it reads what was written.
	_ = u.store.SaveRun(ctx, run)
	u.notifier.Publish(TopicRunFinished, run)
}

// attempt sends one request of a run. Every way it can fail is that request's own row: a node that
// cannot be read, a `{{token}}` that resolves to nothing and a server that never answered are one
// line each, and the run goes on. Stopping at the first broken endpoint would hide the twenty
// behind it, which is the opposite of what a run is for.
func (u *UseCase) attempt(
	ctx context.Context,
	workspace string,
	item runnable,
	position int64,
	runID string,
) domain.CollectionRunResult {
	node := item.node
	result := domain.CollectionRunResult{NodeID: node.ID, Position: position}

	// The row the tree carries has the method and nothing else; what is sent is the node whole.
	full, err := u.store.Node(ctx, node.ID)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// A run is the person's own doing, so the record carries no agent: what the runs send is not an
	// agent's work even when an agent started them.
	rec, err := u.sender.Send(ctx,
		requestFrom(workspace, full, item.auth, item.variables, runID, ""))
	if err != nil {
		result.Error = err.Error()
		result.Failure = domain.AsFailure(err)
		return result
	}
	if rec.Skipped {
		// A pre-request script said this request must not go out. There is no answer and no error:
		// the row says which of the two happened, and the run counts it as neither.
		result.Skipped = true
		return result
	}

	// What the request produced is what the row opens: without it a run can say which endpoint failed
	// and nothing about what it answered.
	result.RecordID = rec.ID
	result.DurationUs = rec.DurationUs
	result = u.withAssertions(ctx, result, rec.ID)
	if rec.Status == 0 {
		// Nothing came back, so the error is all there is to report. A status of zero is not a
		// status: it is the absence of one.
		result.Error = rec.Error
		return result
	}
	status := rec.Status
	result.Status = &status
	result.OK = rec.Error == ""
	result.Error = rec.Error
	return result
}

// withAssertions answers a row with what the scripts around its request asserted. A report that
// cannot be read is a row nobody asserted anything about, which the page draws the same way as a
// request nothing was written for: the answer is the row's subject, and a run is not the place to
// fail over what a script said about it.
func (u *UseCase) withAssertions(
	ctx context.Context,
	result domain.CollectionRunResult,
	recordID string,
) domain.CollectionRunResult {
	if recordID == "" {
		return result
	}
	passed, total, err := u.assertions.AssertionCounts(ctx, recordID)
	if err != nil {
		return result
	}
	result.AssertionsPassed, result.AssertionsTotal = passed, total
	return result
}

// The URL is the request's own — a query string is a URL's rows, not a second copy of them — so
// the parameters travel for the record and do not rewrite the address. A path parameter is written
// into it here: the sender is handed an address and no rows to fill it from.
func requestFrom(
	workspace string,
	node domain.CollectionNode,
	auth *domain.Auth,
	above []domain.Variable,
	runID string,
	agent string,
) RunRequest {
	request := RunRequest{
		Run:           runID,
		NodeID:        node.ID,
		Workspace:     workspace,
		Method:        node.Method,
		URL:           domain.FillPath(node.URL, node.PathParams),
		Body:          node.Body,
		BodyKind:      node.BodyKind,
		Form:          node.Form,
		BodyFile:      node.BodyFile,
		Headers:       []domain.HeaderPair{},
		Auth:          auth,
		Variables:     above,
		EnvironmentID: node.EnvironmentID,
		Agent:         agent,
	}
	for _, row := range node.Headers {
		if row.Enabled && strings.TrimSpace(row.Name) != "" {
			request.Headers = append(request.Headers, domain.HeaderPair{Name: row.Name, Value: row.Value})
		}
	}
	return request
}

// runnable is one request of a run together with the authorization the walk found above it: the
// tree is what knows about inheritance, and the sender is told the answer rather than the tree.
type runnable struct {
	node domain.CollectionNode
	auth *domain.Auth
	// What the levels above answer for `{{tokens}}`, outermost first: the walk down the tree is where
	// it is collected, and the sender is handed it because by then the tree is out of reach.
	variables []domain.Variable
}

// A request is a list of one, so running a saved request and running a collection are the same
// call. A node the collection does not have is a deletion the window has not heard about yet, not
// an empty run.
//
// The tree is walked from the root rather than from the collection the window named, because a
// folder is run as itself and a folder two levels down still inherits from the levels between it
// and the root: handing the walk a detached subtree would lose their authorization and their
// variables silently, and the requests would go out without them.
func requestsUnder(tree []domain.Collection, collectionID, nodeID string) ([]runnable, error) {
	anchor, inherited, above, ok := findCollectionWith(tree, collectionID, nil, nil)
	if !ok {
		return nil, fmt.Errorf("collection %s: %w", collectionID, domain.ErrNotFound)
	}
	if nodeID == "" || nodeID == collectionID {
		return requestsIn(anchor, inherited, above), nil
	}

	// What the run was started from is a level inside the anchor, and the anchor is one of the levels
	// above it — so the pair handed to the walk is the anchor's own, not the one it was given.
	at := answerOf(anchor.Auth, inherited)
	variables := appendLevel(above, anchor.Variables)

	if nested, nAt, nAbove, ok := findCollectionWith(anchor.Children, nodeID, at, variables); ok {
		return requestsIn(nested, nAt, nAbove), nil
	}
	node, auth, nAbove, ok := findNodeWith(anchor, nodeID, at, variables)
	if !ok {
		return nil, fmt.Errorf("node %s: %w", nodeID, domain.ErrNotFound)
	}
	return []runnable{{node: node, auth: auth, variables: nAbove}}, nil
}

// findCollectionWith is a collection and what the levels above it answer — the pair requestsIn
// needs to carry the walk down, and the same walk aboveUnder does for a card.
func findCollectionWith(
	collections []domain.Collection,
	id string,
	inherited *domain.Auth,
	above []domain.Variable,
) (domain.Collection, *domain.Auth, []domain.Variable, bool) {
	for _, collection := range collections {
		if collection.ID == id {
			return collection, inherited, above, true
		}
		at := answerOf(collection.Auth, inherited)
		variables := appendLevel(above, collection.Variables)
		if found, fAt, fAbove, ok := findCollectionWith(collection.Children, id, at, variables); ok {
			return found, fAt, fAbove, true
		}
	}
	return domain.Collection{}, nil, nil, false
}

// findNodeWith is the same walk for a request inside a collection: it answers with the level the
// request itself goes out under, which is what a runnable carries.
func findNodeWith(
	collection domain.Collection,
	id string,
	inherited *domain.Auth,
	above []domain.Variable,
) (domain.CollectionNode, *domain.Auth, []domain.Variable, bool) {
	for _, node := range collection.Items {
		if node.ID == id {
			return node, answerOf(node.Auth, inherited), above, true
		}
	}
	for _, nested := range collection.Children {
		at := answerOf(nested.Auth, inherited)
		variables := appendLevel(above, nested.Variables)
		if node, nAt, nAbove, ok := findNodeWith(nested, id, at, variables); ok {
			return node, nAt, nAbove, true
		}
	}
	return domain.CollectionNode{}, nil, nil, false
}

// appendLevel copies rather than appending to what it was handed: the walk branches, and two
// branches appending to one slice would give the second one the first one's answers.
func appendLevel(above, level []domain.Variable) []domain.Variable {
	out := make([]domain.Variable, 0, len(above)+len(level))
	out = append(out, above...)
	return append(out, level...)
}

// The order is the level's own, not requests-then-collections, and the answer of the levels above
// is carried down.
func requestsIn(
	collection domain.Collection,
	inherited *domain.Auth,
	above []domain.Variable,
) []runnable {
	at := answerOf(collection.Auth, inherited)
	// A level's own variables stand over the ones above it, so they are appended: the reader takes the
	// last answer for a name, which is the nearest level.
	variables := appendLevel(above, collection.Variables)

	out := []runnable{}
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			out = append(out, requestsIn(*entry.Collection, at, variables)...)
			continue
		}
		out = append(out, runnable{
			node:      *entry.Node,
			auth:      answerOf(entry.Node.Auth, at),
			variables: variables,
		})
	}
	return out
}

// findCollection looks a collection up by id, the ones inside collections included: a collection
// answers to an id wherever it sits, because running one of them and dropping something into it are
// the same kind of question.
func findCollection(tree []domain.Collection, id string) (domain.Collection, bool) {
	for _, collection := range tree {
		if collection.ID == id {
			return collection, true
		}
		if found, ok := findCollection(collection.Children, id); ok {
			return found, true
		}
	}
	return domain.Collection{}, false
}
