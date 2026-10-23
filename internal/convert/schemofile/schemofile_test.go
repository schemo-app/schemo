package schemofile

import (
	"errors"
	"testing"

	"schemo/internal/domain"
)

// sample is a document with everything the format has to keep: folders in their order, a request
// with rows and a body, authorization both ways, scripts on a level and on a request, and an
// environment with a secret in it.
func sample() domain.Document {
	return domain.Document{
		Collections: []domain.Collection{
			{
				Name:        "Storefront",
				Description: "Everything a shop needs.",
				Position:    1,
				Auth: &domain.Auth{
					Type: domain.AuthBearer, Fields: map[string]string{"token": "{{token}}"},
				},
				Variables: []domain.Variable{
					{
						Name: "base_url", Value: "https://api.example.com", Kind: domain.VariableText,
						Enabled: true, Position: 1,
					},
				},
				Scripts: &domain.Scripts{Pre: "vars.set('a', '1')"},
				Items: []domain.CollectionNode{
					{
						Name: "List", Position: 2, Method: "GET",
						URL: "https://api.example.com/products/:product?page[size]=25",
						Params: []domain.Row{
							{Name: "page[size]", Value: "25", Enabled: true, Description: "Page size"},
						},
						PathParams: []domain.Row{
							{Name: "product", Value: "42", Enabled: true, Description: "Which product"},
						},
						Headers: []domain.Row{
							{Name: "Accept", Value: "application/vnd.api+json", Enabled: true,
								Description: "The media type"},
						},
						BodyKind: domain.BodyRaw, Body: `{"a":1}`,
						Scripts:     &domain.Scripts{Post: "pm.test('ok', () => {});", PostOff: true},
						Description: "One page of them.",
						Responses: []domain.ResponseExample{{
							Name: "The list", Status: 200, Body: `{"data":[]}`,
							Headers: []domain.HeaderPair{
								{Name: "Content-Type", Value: "application/json"},
							},
						}},
					},
				},
				Children: []domain.Collection{
					{
						Name: "Admin", Position: 1,
						Items: []domain.CollectionNode{{Name: "Users", Position: 1, Method: "GET",
							URL: "https://api.example.com/users"}},
					},
				},
			},
		},
		Environments: []domain.EnvironmentDraft{
			{
				Name:  "Staging",
				Color: "#ff9500",
				Variables: []domain.Variable{
					{
						Name: "base_url", Value: "https://staging.example.com", Kind: domain.VariableText,
						Enabled: true, Position: 1,
					},
					{Name: "token", Value: "st-42", Kind: domain.VariableSecret, Enabled: true, Position: 2},
				},
			},
		},
	}
}

// The round trip is the whole point of the format: a document written out and read back is the
// document it was, folders and scripts and the kinds of its variables included.
func TestWriteAndReadKeepEverything(t *testing.T) {
	data, err := Write(sample(), true)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	back, err := Read(data)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(back.Collections) != 1 || len(back.Environments) != 1 {
		t.Fatalf("document = %+v, want a collection and an environment", back)
	}
	collection := back.Collections[0]
	if collection.Name != "Storefront" || collection.Description != "Everything a shop needs." {
		t.Errorf("collection = %+v, want its name and description", collection)
	}
	if collection.Auth == nil || collection.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want it as it was", collection.Auth)
	}
	if len(collection.Variables) != 1 || collection.Variables[0].Value != "https://api.example.com" {
		t.Errorf("variables = %+v, want the value kept", collection.Variables)
	}
	if collection.Scripts == nil || collection.Scripts.Pre != "vars.set('a', '1')" {
		t.Errorf("scripts = %+v, want the level's code kept", collection.Scripts)
	}

	node := collection.Items[0]
	if node.URL != "https://api.example.com/products/:product?page[size]=25" ||
		node.Body != `{"a":1}` {
		t.Errorf("request = %+v, want the address and the body", node)
	}
	if len(node.PathParams) != 1 || node.PathParams[0].Value != "42" ||
		node.PathParams[0].Description != "Which product" {
		t.Errorf("path params = %+v, want the row kept with its value", node.PathParams)
	}
	if node.Scripts == nil || !node.Scripts.PostOff || node.Description != "One page of them." {
		t.Errorf("request = %+v, want its scripts and description", node)
	}
	if len(node.Headers) != 1 || node.Headers[0].Name != "Accept" ||
		node.Headers[0].Description != "The media type" {
		t.Errorf("headers = %+v, want the row kept with its description", node.Headers)
	}
	if len(node.Responses) != 1 || node.Responses[0].Name != "The list" ||
		node.Responses[0].Status != 200 || node.Responses[0].Body != `{"data":[]}` ||
		len(node.Responses[0].Headers) != 1 {
		t.Errorf("responses = %+v, want the saved example kept", node.Responses)
	}

	// The two kinds of row share one number line, so the order the tree draws comes back as it was.
	if len(collection.Children) != 1 || collection.Children[0].Name != "Admin" ||
		collection.Children[0].Position != 1 {
		t.Fatalf("children = %+v, want the folder where it stood", collection.Children)
	}
	order := collection.Level()
	if order[0].Collection == nil || order[1].Node == nil {
		t.Errorf("level = %+v, want the folder first", order)
	}
}

// A secret's value is written only when somebody asked for it: the name and the kind travel either
// way, so the other side knows what to fill in.
func TestWriteLeavesSecretValuesOut(t *testing.T) {
	plain, err := Write(sample(), false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	back, err := Read(plain)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	token := back.Environments[0].Variables[1]
	if token.Name != "token" || token.Kind != domain.VariableSecret || token.Value != "" {
		t.Errorf("token = %+v, want a secret with its name and no value", token)
	}

	shown, err := Write(sample(), true)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	back, err = Read(shown)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := back.Environments[0].Variables[1].Value; got != "st-42" {
		t.Errorf("token = %q, want the value when it was asked for", got)
	}
}

func TestReadRefusesWhatIsNotASchemoFile(t *testing.T) {
	for name, data := range map[string]string{
		"not json":        `curl -X GET https://example.com`,
		"another format":  `{"info": {"name": "Storefront"}, "item": []}`,
		"another version": `{"schemo": "2.0", "collections": [{"name": "A"}]}`,
		"nothing in it":   `{"schemo": "1.0"}`,
	} {
		if _, err := Read([]byte(data)); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s: Read = %v, want ErrNotAllowed", name, err)
		}
	}
}
