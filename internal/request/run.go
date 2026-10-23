package request

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/script"
	"schemo/internal/workspace"
)

// A run reports each finished request, then itself once at the end.
const (
	TopicRunProgress = "run:progress"
	TopicRunFinished = "run:finished"
)

// RunProgress is one request of a run finishing, with the row it produced.
type RunProgress struct {
	RunID        string `json:"runId"`
	CollectionID string `json:"collectionId"`
	NodeID       string `json:"nodeId"`
	// Environment is what the run went out under, so rows are not labelled with a later selection.
	Environment string                     `json:"environment,omitempty"`
	Done        int                        `json:"done"`
	Total       int                        `json:"total"`
	Result      domain.CollectionRunResult `json:"result"`
}

// Runs sends saved requests: a whole collection or folder one request after another, or a single
// request for an agent. Each goes out the way the window sends it, through a draft and the history.
type Runs struct {
	store        *db.Store
	scope        domain.Scope
	collections  *workspace.Collections
	environments *workspace.Environments
	drafts       *Drafts
	history      *History
	scripts      *script.Scripts
	notifier     domain.Notifier
	ids          platform.IDGen

	// send is replaced in tests that decide what a server answered.
	send func(ctx context.Context, req runRequest) (domain.Record, error)

	running atomic.Bool
	stopped atomic.Bool
}

func NewRuns(
	store *db.Store,
	collections *workspace.Collections,
	environments *workspace.Environments,
	drafts *Drafts,
	history *History,
	scripts *script.Scripts,
	notifier domain.Notifier,
	ids platform.IDGen,
) *Runs {
	s := &Runs{
		store: store, scope: store, collections: collections, environments: environments,
		drafts: drafts, history: history, scripts: scripts, notifier: notifier, ids: ids,
	}
	s.send = s.sendOne
	return s
}

// Run starts a run of everything under a node and returns its id at once; progress arrives as
// events. One run at a time.
func (s *Runs) Run(ctx context.Context, collectionID, nodeID string) (string, error) {
	if !s.running.CompareAndSwap(false, true) {
		return "", domain.Refuse(domain.CodeRunInProgress, domain.ErrNotAllowed, nil)
	}
	// The workspace is fixed here: a run outlives this call, and the window may switch workspaces.
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		s.running.Store(false)
		return "", err
	}
	requests, err := s.collections.InWorkspace(workspace).Requests(ctx, collectionID, nodeID)
	if err != nil {
		s.running.Store(false)
		return "", err
	}
	if len(requests) == 0 {
		s.running.Store(false)
		return "", domain.Refuse(domain.CodeNodeHasNoRequests, domain.ErrNotAllowed, nil)
	}
	environment, _ := s.environments.InWorkspace(workspace).ActiveName(ctx)
	run := domain.CollectionRun{
		ID:           s.ids(),
		CollectionID: collectionID,
		NodeID:       nodeID,
		Environment:  environment,
		StartedAt:    time.Now().UnixMilli(),
		Results:      []domain.CollectionRunResult{},
	}
	// Written before anything is sent: results reference it.
	if err := s.store.SaveRun(ctx, run); err != nil {
		s.running.Store(false)
		return "", err
	}
	s.stopped.Store(false)
	go s.execute(workspace, run, requests)
	return run.ID, nil
}

// Stop asks the run to stop after the request in flight.
func (s *Runs) Stop() bool {
	if !s.running.Load() {
		return false
	}
	s.stopped.Store(true)
	return true
}

// Last is the latest run of a node, or nil when it was never run.
func (s *Runs) Last(ctx context.Context, collectionID, nodeID string) (*domain.CollectionRun, error) {
	run, ok, err := s.store.LastRun(ctx, collectionID, nodeID)
	if err != nil || !ok {
		return nil, err
	}
	return &run, nil
}

// Send sends one saved request of a named workspace on behalf of an agent and waits for its record.
//
//wails:ignore
func (s *Runs) Send(ctx context.Context, workspace, nodeID, agent string) (domain.Record, error) {
	collections := s.collections.InWorkspace(workspace)
	node, err := collections.Node(ctx, nodeID)
	if err != nil {
		return domain.Record{}, err
	}
	requests, err := collections.Requests(ctx, node.CollectionID, nodeID)
	if err != nil {
		return domain.Record{}, err
	}
	if len(requests) != 1 {
		return domain.Record{}, fmt.Errorf("node %s: %w", nodeID, domain.ErrNotFound)
	}
	return s.send(ctx, runRequestFrom(workspace, requests[0], "", agent))
}

func (s *Runs) execute(workspace string, run domain.CollectionRun, requests []domain.ResolvedRequest) {
	ctx := context.Background()
	defer s.running.Store(false)

	started := time.Now()
	for position, item := range requests {
		if s.stopped.Load() {
			break
		}
		result := s.runOne(ctx, workspace, item, int64(position), run.ID)
		if err := s.store.AppendRunResult(ctx, run.ID, result); err != nil {
			break
		}
		run.Results = append(run.Results, result)
		switch {
		case result.Skipped:
		case result.OK:
			run.Passed++
		default:
			run.Failed++
		}
		s.notifier.Publish(TopicRunProgress, RunProgress{
			RunID: run.ID, CollectionID: run.CollectionID, NodeID: run.NodeID,
			Environment: run.Environment, Done: len(run.Results), Total: len(requests), Result: result,
		})
	}
	run.DurationUs = time.Since(started).Microseconds()
	run.FinishedAt = time.Now().UnixMilli()
	_ = s.store.SaveRun(ctx, run)
	s.notifier.Publish(TopicRunFinished, run)
}

// runOne sends one request of a run. Its failure is its own row; the run goes on.
func (s *Runs) runOne(
	ctx context.Context,
	workspace string,
	item domain.ResolvedRequest,
	position int64,
	runID string,
) domain.CollectionRunResult {
	result := domain.CollectionRunResult{NodeID: item.Node.ID, Position: position}
	rec, err := s.send(ctx, runRequestFrom(workspace, item, runID, ""))
	if err != nil {
		result.Error = err.Error()
		result.Failure = domain.AsFailure(err)
		return result
	}
	if rec.Skipped {
		result.Skipped = true
		return result
	}
	result.RecordID = rec.ID
	result.DurationUs = rec.DurationUs
	if passed, total, err := s.store.AssertionCounts(ctx, rec.ID); err == nil {
		result.AssertionsPassed, result.AssertionsTotal = passed, total
	}
	result.Error = rec.Error
	if rec.Status == 0 {
		return result
	}
	status := rec.Status
	result.Status = &status
	result.OK = rec.Error == ""
	return result
}

// sendOne prepares the request as a draft (tokens filled, secrets masked in the record) and sends
// it through the history.
func (s *Runs) sendOne(ctx context.Context, req runRequest) (domain.Record, error) {
	prepared, err := s.drafts.Prepare(ctx, Seed{
		Method:        req.Method,
		URL:           req.URL,
		Body:          req.Body,
		BodyKind:      req.BodyKind,
		Form:          req.Form,
		BodyFile:      req.BodyFile,
		Headers:       req.Headers,
		Auth:          req.Auth,
		EnvironmentID: req.EnvironmentID,
	}, append(slices.Clone(req.Variables), s.scripts.RunVariables(req.Run)...))
	if err != nil {
		return domain.Record{}, err
	}
	input := prepared.SendInput()
	input.Node, input.Run, input.Agent = req.NodeID, req.Run, req.Agent
	return s.history.SendAndWait(ctx, req.Workspace, input)
}

// runRequest is one saved request on its way out, with only its enabled headers.
type runRequest struct {
	Run       string
	NodeID    string
	Workspace string
	Agent     string // empty when the person sends

	Method        string
	URL           string
	Body          string
	BodyKind      domain.BodyKind
	Form          []domain.FormRow
	BodyFile      string
	Headers       []domain.HeaderPair
	Auth          *domain.Auth      // from the nearest level that answered
	Variables     []domain.Variable // of the levels above, outermost first
	EnvironmentID string            // the request's own pin; empty means the active one
}

func runRequestFrom(workspace string, item domain.ResolvedRequest, runID, agent string) runRequest {
	node := item.Node
	req := runRequest{
		Run:           runID,
		NodeID:        node.ID,
		Workspace:     workspace,
		Agent:         agent,
		Method:        node.Method,
		URL:           domain.FillPath(node.URL, node.PathParams),
		Body:          node.Body,
		BodyKind:      node.BodyKind,
		Form:          node.Form,
		BodyFile:      node.BodyFile,
		Headers:       []domain.HeaderPair{},
		Auth:          item.Auth,
		Variables:     item.Variables,
		EnvironmentID: node.EnvironmentID,
	}
	for _, row := range node.Headers {
		if row.Enabled && strings.TrimSpace(row.Name) != "" {
			req.Headers = append(req.Headers, domain.HeaderPair{Name: row.Name, Value: row.Value})
		}
	}
	return req
}
