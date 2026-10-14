package collection

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// A single send is the run's send with one request under it: the levels above are resolved, the
// rows a person switched off stay home, and the record is filed under the space the caller named —
// which is the whole reason the space is an argument: an agent sends requests of spaces the window
// is not showing.
func TestSendingOneSavedRequestAnswersWithItsRecord(t *testing.T) {
	uc, _, sender, _, _ := newTestRun()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Users", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	if _, err := uc.SaveVariables(ctx, collectionID, []domain.Variable{
		{ID: "v-1", Name: "version", Value: "v1", Enabled: true},
	}); err != nil {
		t.Fatalf("SaveVariables: %v", err)
	}

	_, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: collectionID, Name: "List users", Method: "GET",
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	node := findInTree(t, tree, "List users")
	if _, err := uc.SaveNode(ctx, domain.CollectionNode{
		ID: node.ID, Name: "List users", Method: "GET", URL: "https://api.example/users",
		Headers: []domain.Row{
			{ID: "h-on", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
			{ID: "h-off", Name: "X-Off", Value: "no", Enabled: false},
		},
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	sender.reply("https://api.example/users", 200, 1000)

	rec, err := uc.Send(ctx, "space-b", node.ID, "Claude Desktop")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if rec.Status != 200 {
		t.Errorf("record = %+v, want what the sender answered", rec)
	}

	sent := sender.requests()
	if len(sent) != 1 {
		t.Fatalf("sent %d requests, want one", len(sent))
	}
	if sent[0].Workspace != "space-b" {
		t.Errorf("workspace = %q, want the one the caller named", sent[0].Workspace)
	}
	if sent[0].NodeID != node.ID {
		t.Errorf("node = %q, want the request the caller named", sent[0].NodeID)
	}
	// The levels above travel with the request: it was saved inside a collection that answers for
	// `{{version}}`, and the send is the moment that answer is used.
	if len(sent[0].Variables) != 1 || sent[0].Variables[0].Name != "version" {
		t.Errorf("variables = %+v, want the collection's own", sent[0].Variables)
	}
	if len(sent[0].Headers) != 1 || sent[0].Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, want only the enabled row", sent[0].Headers)
	}
	if sent[0].Run != "" {
		t.Errorf("run = %q, want a send that is part of no run", sent[0].Run)
	}
	// Who is sending travels with the request: it ends up in the record, which is where history
	// says whose doing the send was.
	if sent[0].Agent != "Claude Desktop" {
		t.Errorf("agent = %q, want the name the caller sent as", sent[0].Agent)
	}
}

// A request that is not there is a refusal and not a send: an id from outside names something that
// may have been deleted since it was read.
func TestSendingARequestThatIsNotThereSendsNothing(t *testing.T) {
	uc, _, sender, _, _ := newTestRun()
	ctx := context.Background()

	if _, err := uc.Send(ctx, "space-a", "no-such-node", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Send = %v, want not-found", err)
	}
	if sent := sender.sent; len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing", sent)
	}
}

// A path parameter is the request's own: a saved request goes out with the values it was saved
// with written into its address, the way the window sends it.
func TestSendingASavedRequestFillsItsPathParameters(t *testing.T) {
	uc, _, sender, _, _ := newTestRun()
	ctx := context.Background()

	_, tree, err := uc.CreateCollection(ctx, "Shop", "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	_, tree, err = uc.CreateNode(ctx, NodeDraft{
		CollectionID: only(t, tree).ID, Name: "One product", Method: "GET",
		URL:        "https://api.example/products/:product/prices/:price",
		PathParams: []domain.Row{{Name: "product", Value: "a b", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	node := findInTree(t, tree, "One product")
	sender.reply("https://api.example/products/a%20b/prices/:price", 200, 1000)

	if _, err := uc.Send(ctx, "space-b", node.ID, ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sent := sender.requests()
	if len(sent) != 1 || sent[0].URL != "https://api.example/products/a%20b/prices/:price" {
		t.Errorf("sent = %+v, want the filled name written in and the empty one left", sent)
	}
}
