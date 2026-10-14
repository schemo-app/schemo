package wails

import (
	"context"
	"strings"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/record"
)

// A captured request is kept as the browser sent it — that is what makes it worth having — so the
// agent's surface is the one place it must not read the value out again. An address is where a
// page's own token sits and a header is where a session comes back; both are masked by value.
func TestAgentReadsNoSecretOutOfACapture(t *testing.T) {
	const secret = "captured-token-value"

	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	ctx := context.Background()
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatalf("desktop.OpenStorage: %v", err)
	}

	// A global secret is what both readers below are given to find: it applies whatever environment
	// is active, which is the one thing a history row cannot say about itself.
	if err := store.SaveVariable(ctx, domain.WorkspacePersonalID, domain.EnvScope{}, domain.Variable{
		ID: "var-1", Name: "token", Value: secret, Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}

	records := record.NewUseCase(store, store, nil, nil, &heard{},
		record.RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
			return domain.RetainForever, nil
		}), nil, nil, nil, platform.NewIDGen(), platform.BuildInfo{Name: "Schemo"})

	captured, err := records.Ingest(ctx, record.IngestInput{
		Method:          "GET",
		URL:             "https://api.example.com/things?token=" + secret,
		Status:          200,
		ResponseHeaders: []domain.HeaderPair{{Name: "Set-Cookie", Value: "session=" + secret}},
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	// The store holds what the browser sent: hiding a value is the reader's business, and a capture
	// written down masked would be one that cannot be replayed.
	if !strings.Contains(captured.URL, secret) {
		t.Fatalf("stored url = %q, want the address as it arrived", captured.URL)
	}

	tools := mcpTools{
		records:     records,
		environment: environment.NewUseCase(store, store, platform.NewIDGen()),
	}

	rows, err := tools.History(ctx, domain.WorkspacePersonalID, 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the one capture", rows)
	}
	if strings.Contains(rows[0].URL, secret) {
		t.Errorf("agent's url = %q, want the secret masked", rows[0].URL)
	}

	answer, err := tools.Response(ctx, captured.ID)
	if err != nil {
		t.Fatalf("Response: %v", err)
	}
	if len(answer.Headers) != 1 {
		t.Fatalf("headers = %+v, want the one the server sent", answer.Headers)
	}
	if strings.Contains(answer.Headers[0].Value, secret) {
		t.Errorf("agent's header = %q, want the secret masked", answer.Headers[0].Value)
	}
}
