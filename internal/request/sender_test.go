package request

import (
	"context"
	"schemo/internal/domain"
	"testing"
)

type senderExecutor struct{ switchAccount func() }

func (e senderExecutor) Execute(context.Context, Request) (*domain.Response, error) {
	e.switchAccount()
	return &domain.Response{Status: 200}, nil
}

func TestHistoryKeepsOnlyTheSenderIDFromBeforeExecution(t *testing.T) {
	history, held, _, _ := newHistory(t)
	ctx := t.Context()
	if err := held.store.Save(ctx, domain.Account{UserID: "alice", Name: "Alice", Email: "alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	history.executor = senderExecutor{switchAccount: func() {
		if err := held.store.Save(ctx, domain.Account{UserID: "bob"}); err != nil {
			t.Error(err)
		}
	}}
	record, err := history.SendAndWait(ctx, domain.WorkspacePersonalID, input())
	if err != nil || record.SenderID != "alice" {
		t.Fatalf("SendAndWait sender = %q, %v, want alice", record.SenderID, err)
	}
	list, err := history.List(ctx, "", 10)
	if err != nil || len(list) != 1 || list[0].SenderID != "alice" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	captured, err := history.Ingest(ctx, IngestInput{Method: "GET", URL: "https://example.test"})
	if err != nil || captured.SenderID != "bob" {
		t.Fatalf("Ingest sender = %q, %v, want bob", captured.SenderID, err)
	}
}
