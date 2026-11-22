package openapi

import (
	"testing"

	"schemo/internal/domain"
)

// The first server is the collection's baseUrl; every other is an environment with a block for the
// collection, named by its description or else its host, its variables filled from their defaults.
func TestImportReadsOtherServersAsEnvironmentBlocks(t *testing.T) {
	doc, err := Import([]byte(`{"openapi": "3.1.0", "info": {"title": "Shop"},
		"servers": [
			{"url": "https://api.example.com/v1/"},
			{"url": "https://staging.example.com:{port}/v1", "description": "Staging",
				"variables": {"port": {"default": "8443"}}},
			{"url": "http://localhost:8080"},
			{"url": "{{host}}"}
		],
		"paths": {"/items": {"get": {"responses": {"200": {"description": "OK"}}}}}}`))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if base := variable(t, doc.Collections[0], domain.BaseURLVariable); base.Value != "https://api.example.com/v1" {
		t.Errorf("baseUrl = %+v, want the first server", base)
	}
	if len(doc.Environments) != 2 {
		t.Fatalf("environments = %+v, want the two servers with an address", doc.Environments)
	}
	for index, want := range []struct{ name, address string }{
		{"Staging", "https://staging.example.com:8443/v1"},
		{"localhost:8080", "http://localhost:8080"},
	} {
		draft := doc.Environments[index]
		if draft.Name != want.name || len(draft.Variables) != 0 || len(draft.Blocks) != 1 ||
			draft.Blocks[0].Collection != 0 || len(draft.Blocks[0].Variables) != 1 {
			t.Fatalf("environment %d = %+v, want %q with one block", index, draft, want.name)
		}
		if got := draft.Blocks[0].Variables[0]; got.Name != domain.BaseURLVariable || got.Value != want.address {
			t.Errorf("environment %q baseUrl = %+v, want %q", want.name, got, want.address)
		}
	}
}

// Swagger 2.0 has one address, so it makes no environment.
func TestImportReadsASwaggerAddressWithoutEnvironments(t *testing.T) {
	doc, err := Import([]byte(`{"swagger": "2.0", "info": {"title": "Old"}, "host": "old.example.com",
		"basePath": "/api", "schemes": ["http", "https"],
		"paths": {"/items": {"get": {"responses": {"200": {"description": "OK"}}}}}}`))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if base := variable(t, doc.Collections[0], domain.BaseURLVariable); base.Value != "http://old.example.com/api" {
		t.Errorf("baseUrl = %+v, want schemes[0] + host + basePath", base)
	}
	if len(doc.Environments) != 0 {
		t.Errorf("environments = %+v, want none", doc.Environments)
	}
}

// Export lists the collection's baseUrl first, then each environment's under its name, and the
// reader takes them back as the same environments.
func TestExportWritesEnvironmentServers(t *testing.T) {
	collection := domain.Collection{
		Name: "Shop",
		Variables: []domain.Variable{
			{Name: domain.BaseURLVariable, Value: "https://api.example.com", Kind: domain.VariableText, Enabled: true},
		},
		Items: []domain.CollectionNode{{Name: "List", Method: "GET", URL: "{{baseUrl}}/items"}},
	}
	environments := []domain.EnvironmentDraft{
		{Name: "Staging", Variables: []domain.Variable{
			{Name: domain.BaseURLVariable, Value: "https://staging.example.com", Kind: domain.VariableText, Enabled: true},
		}},
		{Name: "Empty"},
	}
	data, err := Export(collection, environments)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	doc, err := Import(data)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if base := variable(t, doc.Collections[0], domain.BaseURLVariable); base.Value != "https://api.example.com" {
		t.Errorf("baseUrl = %+v, want the collection's", base)
	}
	if len(doc.Environments) != 1 || doc.Environments[0].Name != "Staging" ||
		doc.Environments[0].Blocks[0].Variables[0].Value != "https://staging.example.com" {
		t.Errorf("environments = %+v, want Staging back", doc.Environments)
	}
}
