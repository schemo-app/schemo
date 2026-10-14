package draft

import (
	"context"
	"strings"
	"testing"

	"schemo/internal/command"
	"schemo/internal/domain"
)

// A saved request copies without a draft of its own, so the card's draft under the same id keeps
// its unsaved edits: an export copies a whole collection, and replacing the edits of every request
// in it is not what exporting is.
func TestCopySavedLeavesTheOpenDraftAlone(t *testing.T) {
	uc, store := newUseCase()
	ctx := context.Background()

	edited := domain.Draft{ID: "n1", Method: "POST", URL: "https://api.example.com/edited"}
	if err := store.SaveDraft(ctx, "", edited); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	copied, err := uc.CopySaved(ctx, domain.ResolvedRequest{
		Node: domain.CollectionNode{ID: "n1", Method: "GET", URL: "https://api.example.com/saved"},
	}, command.FormatCurl, true)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if !strings.Contains(copied.Text, "/saved") || !strings.Contains(copied.Text, "curl") {
		t.Errorf("text = %q, want the saved request written out", copied.Text)
	}
	if store.saved.Method != "POST" || store.saved.URL != "https://api.example.com/edited" {
		t.Errorf("the open draft = %+v, want the edits untouched", store.saved)
	}
}

// Secrets are as the caller asks: a command meant to be run carries the values, and one meant to be
// handed on leaves each secret as a reference to itself.
func TestCopySavedRendersSecretsAsAsked(t *testing.T) {
	uc, _ := newUseCase()
	ctx := context.Background()

	from := domain.ResolvedRequest{
		Node: domain.CollectionNode{
			ID: "n1", Method: "GET", URL: "https://{{host}}/users",
			Headers: []domain.Row{{Name: "X-Key", Value: "{{token}}", Enabled: true}},
		},
	}

	withValues, err := uc.CopySaved(ctx, from, command.FormatCurl, true)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if !strings.Contains(withValues.Text, "abc123") {
		t.Errorf("text = %q, want the secret's value in a copy meant to be run", withValues.Text)
	}

	named, err := uc.CopySaved(ctx, from, command.FormatCurl, false)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if strings.Contains(named.Text, "abc123") {
		t.Errorf("text = %q, want no secret value in a copy meant to be handed on", named.Text)
	}
	if !strings.Contains(named.Text, "$SCHEMO_TOKEN") {
		t.Errorf("text = %q, want the secret named where its value would be", named.Text)
	}
}

// A paste of commands is the requests it holds, one per command, and a `# name` line above one
// names it — which is how this app's own copy writes them down.
func TestReadCommandsReadsEachCommandWithItsName(t *testing.T) {
	nodes, err := ReadCommands(`# List users
curl 'https://api.example.com/users?page=2' -H 'Accept: application/json'

# Create a user
curl -X POST https://api.example.com/users -H 'Content-Type: application/json' \
  --data-raw '{"name":"Ada"}'
`)
	if err != nil {
		t.Fatalf("ReadCommands: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want both commands", len(nodes))
	}

	list := nodes[0]
	if list.Name != "List users" || list.Method != "GET" ||
		list.URL != "https://api.example.com/users?page=2" {
		t.Errorf("first = %+v, want the first command as it was pasted", list)
	}
	if len(list.Params) != 1 || list.Params[0].Name != "page" || !list.Params[0].Enabled {
		t.Errorf("params = %+v, want the query as rows beside the address", list.Params)
	}

	created := nodes[1]
	if created.Name != "Create a user" || created.Method != "POST" ||
		created.BodyKind != domain.BodyJSON || created.Body != `{"name":"Ada"}` {
		t.Errorf("second = %+v, want the second command with its body", created)
	}
	if len(created.Headers) != 1 || created.Headers[0].Name != "Content-Type" {
		t.Errorf("headers = %+v, want what the command carried", created.Headers)
	}
}

// Text with no command in it is refused: an import has nothing to write, and what a person pastes
// into the window's own field is left untouched by that field's own rule.
func TestReadCommandsRefusesAPasteThatIsNotACommand(t *testing.T) {
	if _, err := ReadCommands("GET https://api.example.com/users\n"); domain.CodeOf(err) !=
		domain.CodeNotCommand {
		t.Errorf("ReadCommands = %v, want notCommand", err)
	}
}
