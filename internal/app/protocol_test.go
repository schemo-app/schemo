package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/grpc"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"

	"schemo/internal/domain"
	"schemo/internal/request"
)

func TestGraphQLDraftAndHistoryKeepTheOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query, OperationName string
			Variables            map[string]int
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" ||
			body.OperationName != "Read" || body.Variables["id"] != 7 || !strings.Contains(body.Query, "query Read") {
			t.Errorf("GraphQL request = %s, %v, %+v", r.Method, r.Header, body)
		}
		w.Header().Set("Content-Type", "application/graphql-response+json")
		fmt.Fprint(w, `{"data":{"item":{"id":7}}}`)
	}))
	defer server.Close()

	device := newDevice(t)
	ctx := t.Context()
	draft := domain.NewDraft()
	draft.ID, draft.Protocol, draft.URL = "tab:gql", domain.ProtocolGraphQL, server.URL
	draft.Body = "query Read($id: Int!) { item(id: $id) { id } }"
	draft.Options = &domain.ProtocolOptions{Variables: `{"id":7}`, OperationName: "Read"}

	collection, err := device.Collections.CreateCollection(ctx, "API", "", "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := device.Drafts.SaveAsNode(ctx, collection.ID, "Read", draft)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := device.Drafts.OpenNode(ctx, node.ID)
	if err != nil || saved.Draft.Protocol != draft.Protocol || saved.Draft.Options == nil ||
		saved.Draft.Options.Variables != draft.Options.Variables {
		t.Fatalf("saved draft = %+v, %v", saved.Draft, err)
	}

	record := send(t, device, saved.Draft)
	if record.Status != http.StatusOK || record.Error != "" {
		t.Fatalf("record = %+v", record)
	}
	if err := device.Drafts.FromRecord(ctx, "tab:replay", record.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := device.Drafts.Open(ctx, "tab:replay")
	if err != nil || replay.Body != draft.Body || replay.Protocol != draft.Protocol ||
		replay.Options == nil || replay.Options.Variables != draft.Options.Variables {
		t.Fatalf("reopened draft = %+v, %v", replay, err)
	}
	stored, err := device.History.Record(ctx, record.ID)
	if err != nil || stored.Operation != "Read" {
		t.Fatalf("stored operation = %q, %v", stored.Operation, err)
	}
}

func TestSSEStopKeepsTheTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Last-Event-ID") != "41" {
			t.Errorf("SSE request = %s, %v", r.Method, r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, "\ufeffid: 42\revent: order\r\ndata: first\ndata: second\r\n\r\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	device := newDevice(t)
	draft := domain.NewDraft()
	draft.ID, draft.Protocol, draft.URL = "tab:sse", domain.ProtocolSSE, server.URL
	draft.Headers = []domain.Row{{Name: "Last-Event-ID", Value: "41", Enabled: true}}
	id := startStream(t, device, draft)
	device.eventually("SSE event", func() bool {
		stream := device.History.Live(t.Context(), id, 0)
		return stream != nil && len(stream.Messages) == 1
	})
	message := device.History.Live(t.Context(), id, 0).Messages[0]
	if message.Name != "order" || message.ID != "42" || message.Data != "first\nsecond" {
		t.Errorf("event = %+v", message)
	}
	finishStream(t, device, id, 1)
}

func TestSecureWebSocketSendsBinaryAndRecordsBothDirections(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"echo"}})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		if conn.Subprotocol() != "echo" {
			t.Errorf("subprotocol = %q", conn.Subprotocol())
		}
		kind, data, err := conn.Read(r.Context())
		if err != nil {
			t.Error(err)
			return
		}
		if kind != websocket.MessageBinary || string(data) != "\x01\x02" {
			t.Errorf("message = %v %x", kind, data)
		}
		if err := conn.Write(r.Context(), kind, data); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(server.Close)

	device := newDevice(t)
	draft := domain.NewDraft()
	settings := domain.DefaultSettings()
	device.Network.Apply(settings.Network())
	draft.ID, draft.Protocol, draft.URL = "tab:ws", domain.ProtocolWebSocket, "ws"+strings.TrimPrefix(server.URL, "http")
	draft.Headers = []domain.Row{{Name: "Sec-WebSocket-Protocol", Value: "echo", Enabled: true}}
	if _, err := device.Environments.SetInScope(t.Context(), domain.EnvScope{}, "payload", "0x0102", true); err != nil {
		t.Fatal(err)
	}
	id := startStream(t, device, draft)
	if _, err := device.Environments.SetInScope(t.Context(), domain.EnvScope{}, "payload", "0x0909", true); err != nil {
		t.Fatal(err)
	}
	if err := device.Drafts.SendMessage(t.Context(), id, domain.SavedMessage{Format: domain.MessageBinary, Data: "{{payload}}"}); err != nil {
		t.Fatal(err)
	}
	device.eventually("WebSocket echo", func() bool {
		stream := device.History.Live(t.Context(), id, 0)
		return stream != nil && stream.Count == 2
	})
	live := device.History.Live(t.Context(), id, 0)
	var masked bool
	for _, message := range live.Messages {
		if message.Direction == "out" {
			masked = message.Data == domain.SecretMask
		}
	}
	if !masked {
		t.Fatalf("outgoing message was not masked: %+v", live.Messages)
	}
	finishStream(t, device, id, 2)
}

type protocolHealth struct {
	health.UnimplementedHealthServer
}

func (protocolHealth) Check(ctx context.Context, input *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	if input.Service != "catalog" || len(metadata.ValueFromIncomingContext(ctx, "x-client")) != 1 {
		return nil, fmt.Errorf("unexpected request: %v", input)
	}
	return &health.HealthCheckResponse{Status: health.HealthCheckResponse_SERVING}, nil
}

func (protocolHealth) Watch(input *health.HealthCheckRequest, stream grpc.ServerStreamingServer[health.HealthCheckResponse]) error {
	for _, state := range []health.HealthCheckResponse_ServingStatus{
		health.HealthCheckResponse_NOT_SERVING, health.HealthCheckResponse_SERVING,
	} {
		if err := stream.Send(&health.HealthCheckResponse{Status: state}); err != nil {
			return err
		}
	}
	return nil
}

func TestGRPCReflectionAndCalls(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	health.RegisterHealthServer(server, protocolHealth{})
	reflection.Register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	device := newDevice(t)
	draft := domain.NewDraft()
	draft.ID, draft.Protocol, draft.URL = "tab:rpc", domain.ProtocolGRPC, listener.Addr().String()
	draft.Headers = []domain.Row{{Name: "x-client", Value: "schemo", Enabled: true}}
	schema, err := device.Drafts.LoadRPCSchema(t.Context(), draft, []domain.SchemaSource{{Kind: domain.SchemaSourceServer}})
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Methods) == 0 || schema.Methods[0].Name != "Check" {
		t.Fatalf("methods = %+v", schema.Methods)
	}
	draft.Options = &domain.ProtocolOptions{RPCMethod: "grpc.health.v1.Health/Check"}
	draft.Body = `{"service":"catalog"}`
	record := send(t, device, draft)
	if record.Status != 0 || record.StatusText != "OK" || record.Error != "" ||
		record.Operation != draft.Options.RPCMethod || record.ResponseBody == nil ||
		!strings.Contains(record.ResponseBody.Inline, "SERVING") {
		t.Fatalf("gRPC record = %+v", record)
	}
	draft.Options.RPCMethod = "grpc.health.v1.Health/Watch"
	stream := send(t, device, draft)
	if stream.StatusText != "OK" || stream.Error != "" || stream.ResponseBody == nil {
		t.Fatalf("gRPC stream = %+v", stream)
	}
	var messages []map[string]string
	if err := json.Unmarshal([]byte(stream.ResponseBody.Inline), &messages); err != nil || len(messages) != 2 ||
		messages[0]["status"] != "NOT_SERVING" || messages[1]["status"] != "SERVING" {
		t.Fatalf("gRPC stream messages = %+v, %v", messages, err)
	}
}

func startStream(t *testing.T, device *device, draft domain.Draft) string {
	t.Helper()
	id, err := device.Drafts.Send(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { device.History.Cancel(id) })
	device.eventually("stream handshake", func() bool {
		if answer := device.events.last(request.TopicRequestFinished); answer != nil {
			t.Fatalf("stream ended before connecting: %+v", answer)
		}
		return device.events.count(request.TopicStreamOpened) > 0
	})
	rows, err := device.History.List(t.Context(), domain.SourceManual, 10)
	if err != nil || len(rows) != 1 || !rows[0].Active || rows[0].ID != id {
		t.Fatalf("live history = %+v, %v", rows, err)
	}
	return id
}

func finishStream(t *testing.T, device *device, id string, count int) {
	t.Helper()
	if !device.History.Cancel(id) {
		t.Fatal("stream is not active")
	}
	device.eventually("saved transcript", func() bool {
		return device.events.count(request.TopicRequestFinished) > 0
	})
	record, err := device.History.Record(t.Context(), id)
	if err != nil || record.Active || record.MessageCount != count || record.Error != "" {
		t.Fatalf("stream record = %+v, %v", record, err)
	}
	body, err := device.History.Body(t.Context(), id, domain.SideResponse)
	if err != nil || strings.Count(body.Inline, "\n") != count {
		t.Fatalf("transcript = %q, %v", body.Inline, err)
	}
}

func TestSchemaSourcesSurviveSaving(t *testing.T) {
	device := newDevice(t)
	ctx := t.Context()
	draft := domain.NewDraft()
	draft.ID, draft.Protocol = "tab:proto", domain.ProtocolGRPC
	draft.Options = &domain.ProtocolOptions{Descriptors: "CgA=", SchemaSources: []domain.SchemaSource{
		{Kind: domain.SchemaSourceServer, LoadedAt: 1, Methods: 3},
		{Kind: domain.SchemaSourceFolder, Path: `C:\api\proto`, Skipped: []string{"v1/old.proto"},
			Files: []domain.SchemaFile{{Name: "v1/billing.proto", Methods: 2}}},
	}}
	if err := device.Drafts.Save(ctx, draft); err != nil {
		t.Fatal(err)
	}
	reopened, err := device.Drafts.Open(ctx, draft.ID)
	if err != nil || reopened.Options == nil || !reflect.DeepEqual(reopened.Options.SchemaSources, draft.Options.SchemaSources) {
		t.Fatalf("tab draft = %+v, %v", reopened.Options, err)
	}
	collection, err := device.Collections.CreateCollection(ctx, "API", "", "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := device.Drafts.SaveAsNode(ctx, collection.ID, "Call", draft)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := device.Drafts.OpenNode(ctx, node.ID)
	if err != nil || saved.Draft.Options == nil || !reflect.DeepEqual(saved.Draft.Options.SchemaSources, draft.Options.SchemaSources) {
		t.Fatalf("saved node = %+v, %v", saved.Draft.Options, err)
	}
}
