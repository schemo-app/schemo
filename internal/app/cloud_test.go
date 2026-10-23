package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	accountv1 "github.com/schemo-app/proto/go/account/v1"
	historyv1 "github.com/schemo-app/proto/go/history/v1"
	planv1 "github.com/schemo-app/proto/go/plan/v1"
	platformv1 "github.com/schemo-app/proto/go/platform/v1"
	syncv1 "github.com/schemo-app/proto/go/sync/v1"
	workspacev1 "github.com/schemo-app/proto/go/workspace/v1"
)

// fakeCloud is an in-memory Schemo cloud: OAuth device sign-in over HTTP and the gRPC services the
// desktop calls, on one loopback listener. It keeps only the server rules the client depends on:
// versions, conflicts, duplicate operations and sequence numbers.
type fakeCloud struct {
	address string

	mu         sync.Mutex
	signingIn  string // the user the next device sign-in completes as
	workspaces map[string]*cloudWorkspace
	moved      chan struct{} // closed and replaced whenever any workspace log grows
}

type cloudWorkspace struct {
	ws       *workspacev1.Workspace
	members  map[string]bool
	seq      int64
	log      []*syncv1.SyncChange
	current  map[string]*syncv1.SyncEntity
	ops      map[string]bool
	hseq     int64
	historyL []*historyv1.RecordChange
}

// newFakeCloud starts a cloud for the test and stops it when the test ends.
func newFakeCloud(t testing.TB) *fakeCloud {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeCloud{
		address:    "http://" + listener.Addr().String(),
		signingIn:  "user-1",
		workspaces: map[string]*cloudWorkspace{},
		moved:      make(chan struct{}),
	}

	rpc := grpc.NewServer()
	accountv1.RegisterAccountServiceServer(rpc, accountServer{c: c})
	platformv1.RegisterPlatformServiceServer(rpc, platformServer{})
	planv1.RegisterPlanServiceServer(rpc, planServer{})
	workspacev1.RegisterWorkspaceServiceServer(rpc, workspaceServer{c: c})
	syncv1.RegisterSyncServiceServer(rpc, syncServer{c: c})
	historyv1.RegisterHistoryServiceServer(rpc, historyServer{c: c})

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", c.discovery)
	mux.HandleFunc("/oauth/device", c.deviceAuthorization)
	mux.HandleFunc("/oauth/token", c.token)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				rpc.ServeHTTP(w, r)
				return
			}
			mux.ServeHTTP(w, r)
		}),
		Protocols: new(http.Protocols),
	}
	server.Protocols.SetHTTP1(true)
	server.Protocols.SetUnencryptedHTTP2(true)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		rpc.Stop()
		_ = server.Close()
	})
	return c
}

// signInAs makes the next device sign-in complete as this user.
func (c *fakeCloud) signInAs(user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signingIn = user
}

func (c *fakeCloud) notify() {
	close(c.moved)
	c.moved = make(chan struct{})
}

// --- OAuth device flow ---

func (c *fakeCloud) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{
		"issuer":                        c.address,
		"device_authorization_endpoint": c.address + "/oauth/device",
		"token_endpoint":                c.address + "/oauth/token",
	})
}

func (c *fakeCloud) deviceAuthorization(w http.ResponseWriter, _ *http.Request) {
	c.mu.Lock()
	user := c.signingIn
	c.mu.Unlock()
	writeJSON(w, map[string]any{
		"device_code":               "device-" + user,
		"user_code":                 "CODE-" + user,
		"verification_uri":          c.address + "/activate",
		"verification_uri_complete": c.address + "/activate?code=" + user,
		"expires_in":                600,
		"interval":                  1, // oauth2 waits five seconds on zero
	})
}

func (c *fakeCloud) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var user string
	switch r.Form.Get("grant_type") {
	case "urn:ietf:params:oauth:grant-type:device_code":
		user = strings.TrimPrefix(r.Form.Get("device_code"), "device-")
	case "refresh_token":
		user = strings.TrimPrefix(r.Form.Get("refresh_token"), "refresh-")
	}
	if user == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	writeJSON(w, map[string]any{
		"access_token":  "access-" + user,
		"refresh_token": "refresh-" + user,
		"token_type":    "Bearer",
		"expires_in":    3600,
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// userOf is the user the call's bearer token names.
func userOf(ctx context.Context) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, value := range md.Get("authorization") {
		if user, ok := strings.CutPrefix(value, "Bearer access-"); ok && user != "" {
			return user, nil
		}
	}
	return "", status.Error(codes.Unauthenticated, "not signed in")
}

// member is the workspace a user may reach, or a refusal.
func (c *fakeCloud) member(ctx context.Context, workspaceID string) (*cloudWorkspace, error) {
	user, err := userOf(ctx)
	if err != nil {
		return nil, err
	}
	s := c.workspaces[workspaceID]
	if s == nil || !s.members[user] {
		return nil, status.Error(codes.NotFound, "no such workspace")
	}
	return s, nil
}

// --- account, platform, plan ---

type accountServer struct {
	accountv1.UnimplementedAccountServiceServer
	c *fakeCloud
}

func (a accountServer) Me(ctx context.Context, _ *accountv1.MeRequest) (*accountv1.MeResponse, error) {
	user, err := userOf(ctx)
	if err != nil {
		return nil, err
	}
	return accountv1.MeResponse_builder{
		Account: accountv1.Account_builder{
			Id: user, Email: user + "@example.com", Name: user,
		}.Build(),
		SessionId: "session-" + user,
	}.Build(), nil
}

func (a accountServer) SignOut(ctx context.Context, _ *accountv1.SignOutRequest) (*accountv1.SignOutResponse, error) {
	if _, err := userOf(ctx); err != nil {
		return nil, err
	}
	return &accountv1.SignOutResponse{}, nil
}

type platformServer struct {
	platformv1.UnimplementedPlatformServiceServer
}

func (platformServer) GetInfo(context.Context, *platformv1.GetInfoRequest) (*platformv1.GetInfoResponse, error) {
	return platformv1.GetInfoResponse_builder{
		Server: platformv1.ServerInfo_builder{
			Name: "test",
			Limits: platformv1.Limits_builder{
				MaxBodyBytes: 8 << 20,
				MaxPushBytes: 4 << 20,
			}.Build(),
		}.Build(),
	}.Build(), nil
}

type planServer struct {
	planv1.UnimplementedPlanServiceServer
}

func (planServer) GetPlan(ctx context.Context, _ *planv1.GetPlanRequest) (*planv1.GetPlanResponse, error) {
	if _, err := userOf(ctx); err != nil {
		return nil, err
	}
	return planv1.GetPlanResponse_builder{
		Plan:  planv1.Plan_builder{Tier: "free"}.Build(),
		Usage: &planv1.Usage{},
	}.Build(), nil
}

// --- workspaces ---

type workspaceServer struct {
	workspacev1.UnimplementedWorkspaceServiceServer
	c *fakeCloud
}

func (s workspaceServer) List(ctx context.Context, _ *workspacev1.ListRequest) (*workspacev1.ListResponse, error) {
	user, err := userOf(ctx)
	if err != nil {
		return nil, err
	}
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	var out []*workspacev1.Workspace
	for _, sp := range s.c.workspaces {
		if sp.members[user] && !sp.ws.HasDeletedAt() {
			out = append(out, view(sp))
		}
	}
	return workspacev1.ListResponse_builder{Workspaces: out, OwnOrgId: "org-" + user}.Build(), nil
}

func (s workspaceServer) Create(ctx context.Context, req *workspacev1.CreateRequest) (*workspacev1.CreateResponse, error) {
	user, err := userOf(ctx)
	if err != nil {
		return nil, err
	}
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if sp := s.c.workspaces[req.GetId()]; sp != nil {
		return workspacev1.CreateResponse_builder{Workspace: view(sp)}.Build(), nil
	}
	kind := req.GetKind()
	if kind == workspacev1.WorkspaceKind_WORKSPACE_KIND_UNSPECIFIED {
		kind = workspacev1.WorkspaceKind_WORKSPACE_KIND_PERSONAL
	}
	org := req.GetOrgId()
	if org == "" {
		org = "org-" + user
	}
	sp := &cloudWorkspace{
		ws: workspacev1.Workspace_builder{
			Id: req.GetId(), OrgId: org, Name: req.GetName(), Color: req.GetColor(), Kind: kind,
			Version: 1, CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(),
		}.Build(),
		members: map[string]bool{user: true},
		current: map[string]*syncv1.SyncEntity{},
		ops:     map[string]bool{},
	}
	s.c.workspaces[req.GetId()] = sp
	return workspacev1.CreateResponse_builder{Workspace: view(sp)}.Build(), nil
}

func (s workspaceServer) Update(ctx context.Context, req *workspacev1.UpdateRequest) (*workspacev1.UpdateResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	sp, err := s.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if req.HasName() {
		sp.ws.SetName(req.GetName())
	}
	if req.HasColor() {
		sp.ws.SetColor(req.GetColor())
	}
	if req.HasKind() {
		sp.ws.SetKind(req.GetKind())
	}
	sp.ws.SetVersion(sp.ws.GetVersion() + 1)
	return workspacev1.UpdateResponse_builder{Workspace: view(sp)}.Build(), nil
}

func (s workspaceServer) Delete(ctx context.Context, req *workspacev1.DeleteRequest) (*workspacev1.DeleteResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	sp, err := s.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	sp.ws.SetDeletedAt(timestamppb.Now())
	return &workspacev1.DeleteResponse{}, nil
}

func view(sp *cloudWorkspace) *workspacev1.Workspace {
	out := proto.Clone(sp.ws).(*workspacev1.Workspace)
	out.SetPermissions([]string{"workspace:read", "workspace:write"})
	out.SetCan(workspacev1.WorkspaceCan_builder{Update: true, Delete: true, ManageMembers: true}.Build())
	out.SetIsMember(true)
	out.SetLastSyncSeq(sp.seq)
	return out
}

// --- sync ---

type syncServer struct {
	syncv1.UnimplementedSyncServiceServer
	c *fakeCloud
}

func (s syncServer) Push(ctx context.Context, req *syncv1.PushRequest) (*syncv1.PushResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	sp, err := s.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	var results []*syncv1.PushResult
	moved := false
	for _, e := range req.GetEntities() {
		result := syncv1.PushResult_builder{EntityId: e.GetId()}
		held := sp.current[e.GetId()]
		switch {
		case e.GetOperationId() != "" && sp.ops[e.GetOperationId()]:
			result.Status = syncv1.PushStatus_PUSH_STATUS_DUPLICATE
			if held != nil {
				result.NewVersion = held.GetVersion()
			}
		case held != nil && held.GetVersion() != e.GetVersion():
			result.Status = syncv1.PushStatus_PUSH_STATUS_CONFLICT_RESOLVED
			result.NewVersion = held.GetVersion()
			result.Winner = proto.Clone(held).(*syncv1.SyncEntity)
		default:
			stored := proto.Clone(e).(*syncv1.SyncEntity)
			stored.SetVersion(e.GetVersion() + 1)
			stored.SetOperationId("")
			if held != nil {
				stored.SetVersion(held.GetVersion() + 1)
			}
			sp.current[e.GetId()] = stored
			sp.ops[e.GetOperationId()] = true
			sp.seq++
			sp.log = append(sp.log, syncv1.SyncChange_builder{Seq: sp.seq, Entity: stored}.Build())
			result.Status = syncv1.PushStatus_PUSH_STATUS_ACCEPTED
			result.NewVersion = stored.GetVersion()
			moved = true
		}
		results = append(results, result.Build())
	}
	if moved {
		s.c.notify()
	}
	return syncv1.PushResponse_builder{Results: results}.Build(), nil
}

func (s syncServer) Pull(ctx context.Context, req *syncv1.PullRequest) (*syncv1.PullResponse, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	sp, err := s.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	changes, next, more := page(sp.log, req.GetAfterSeq(), req.GetLimit(), (*syncv1.SyncChange).GetSeq)
	return syncv1.PullResponse_builder{Changes: changes, NextSeq: next, HasMore: more}.Build(), nil
}

func (s syncServer) Connect(req *syncv1.ConnectRequest, stream grpc.ServerStreamingServer[syncv1.ConnectResponse]) error {
	cursors := map[string]int64{}
	for _, w := range req.GetWorkspaces() {
		cursors[w.GetWorkspaceId()] = w.GetSyncSeq()
	}
	for {
		s.c.mu.Lock()
		var pokes []*syncv1.ConnectResponse
		for id, at := range cursors {
			if sp := s.c.workspaces[id]; sp != nil && (sp.seq > at) {
				cursors[id] = sp.seq
				pokes = append(pokes, syncv1.ConnectResponse_builder{
					Poke: syncv1.Poke_builder{WorkspaceId: id, SyncSeq: sp.seq, HistorySeq: sp.hseq}.Build(),
				}.Build())
			}
		}
		moved := s.c.moved
		s.c.mu.Unlock()
		for _, poke := range pokes {
			if err := stream.Send(poke); err != nil {
				return err
			}
		}
		select {
		case <-stream.Context().Done():
			return nil
		case <-moved:
		case <-time.After(30 * time.Second):
			if err := stream.Send(syncv1.ConnectResponse_builder{Heartbeat: &syncv1.Heartbeat{}}.Build()); err != nil {
				return err
			}
		}
	}
}

// --- history ---

type historyServer struct {
	historyv1.UnimplementedHistoryServiceServer
	c *fakeCloud
}

func (h historyServer) Append(ctx context.Context, req *historyv1.AppendRequest) (*historyv1.AppendResponse, error) {
	h.c.mu.Lock()
	defer h.c.mu.Unlock()
	sp, err := h.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	out := historyv1.AppendResponse_builder{}
	for _, entry := range req.GetRecords() {
		sp.hseq++
		sp.historyL = append(sp.historyL, historyv1.RecordChange_builder{
			Seq: sp.hseq, Record: proto.Clone(entry.GetRecord()).(*historyv1.Record),
		}.Build())
		out.Results = append(out.Results, historyv1.AppendResult_builder{Id: entry.GetRecord().GetId()}.Build())
	}
	for _, run := range req.GetRuns() {
		sp.hseq++
		sp.historyL = append(sp.historyL, historyv1.RecordChange_builder{
			Seq: sp.hseq, Run: proto.Clone(run).(*historyv1.Run),
		}.Build())
		out.RunResults = append(out.RunResults, historyv1.AppendResult_builder{Id: run.GetId()}.Build())
	}
	h.c.notify()
	return out.Build(), nil
}

func (h historyServer) PullRecords(ctx context.Context, req *historyv1.PullRecordsRequest) (*historyv1.PullRecordsResponse, error) {
	h.c.mu.Lock()
	defer h.c.mu.Unlock()
	sp, err := h.c.member(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	changes, next, more := page(sp.historyL, req.GetAfterSeq(), req.GetLimit(), (*historyv1.RecordChange).GetSeq)
	return historyv1.PullRecordsResponse_builder{
		Changes: changes, NextSeq: next, HasMore: more, HeadSeq: sp.hseq,
	}.Build(), nil
}

// page is the log above a sequence number, at most limit entries, and where the next read starts.
func page[T any](log []T, after int64, limit int32, seq func(T) int64) ([]T, int64, bool) {
	if limit <= 0 {
		limit = 500
	}
	var out []T
	next := after
	for _, entry := range log {
		if seq(entry) <= after {
			continue
		}
		if len(out) == int(limit) {
			return out, next, true
		}
		out = append(out, entry)
		next = seq(entry)
	}
	return out, next, false
}
