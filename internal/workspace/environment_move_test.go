package workspace

import (
	"errors"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func TestMoveVariableBetweenCollectionAndEnvironments(t *testing.T) {
	s, store := newEnvironments(t)
	ctx := t.Context()
	collections := NewCollections(store, domain.WorkspaceScope(domain.WorkspacePersonalID), platform.NewIDGen())
	collection, err := collections.CreateCollection(ctx, "Catalog", "", "")
	if err != nil {
		t.Fatal(err)
	}
	own := []domain.Variable{text("own", "baseUrl", "https://catalog.test")}
	if err := collections.Update(ctx, collection.ID, CollectionPatch{Variables: &own}); err != nil {
		t.Fatal(err)
	}
	local := mustCreate(t, s, "Local")
	production := mustCreate(t, s, "Production")
	saveBlock(t, s, local, collection.ID, text("held", "baseUrl", "https://old.test"))
	saveBlock(t, s, production, collection.ID, text("other", "unchanged", "keep"))

	replaced, err := s.MoveVariable(ctx, collection.ID, "own", "", local)
	if err != nil || !replaced {
		t.Fatalf("MoveVariable(collection -> local) = %v, %v; want replacement", replaced, err)
	}
	tree, err := collections.Tree(ctx)
	if err != nil || len(tree[0].Variables) != 0 {
		t.Fatalf("Tree after move = %+v, %v; want no collection variables", tree, err)
	}
	block := envOf(t, s, local).Block(collection.ID)
	if len(block) != 1 || block[0].Value != "https://catalog.test" {
		t.Fatalf("Local block after move = %+v; want catalog address only", block)
	}
	if _, err := s.MoveVariable(ctx, collection.ID, block[0].ID, local, production); err != nil {
		t.Fatal(err)
	}
	if len(envOf(t, s, local).Block(collection.ID)) != 0 {
		t.Error("MoveVariable(local -> production) left the source row")
	}
	block = envOf(t, s, production).Block(collection.ID)
	if len(block) != 2 || block[0].Name != "unchanged" || block[1].Name != "baseUrl" {
		t.Fatalf("Production block = %+v; want unchanged followed by baseUrl", block)
	}
	if _, err := s.MoveVariable(ctx, collection.ID, block[1].ID, production, ""); err != nil {
		t.Fatal(err)
	}
	tree, err = collections.Tree(ctx)
	if err != nil || len(tree[0].Variables) != 1 || tree[0].Variables[0].Value != own[0].Value {
		t.Errorf("Tree after moving back = %+v, %v; want original value", tree, err)
	}
}

func TestMoveVariablePreservesSecretsAndRefusesReadOnlyScopes(t *testing.T) {
	s, store := newEnvironments(t)
	ctx := t.Context()
	collections := NewCollections(store, domain.WorkspaceScope(domain.WorkspacePersonalID), platform.NewIDGen())
	collection, err := collections.CreateCollection(ctx, "Catalog", "", "")
	if err != nil {
		t.Fatal(err)
	}
	from := mustCreate(t, s, "From")
	into := mustCreate(t, s, "Into")
	secret := text("secret", "token", "test-only-value")
	secret.Kind = domain.VariableSecret
	saveBlock(t, s, from, collection.ID, secret)
	if _, err := s.MoveVariable(ctx, collection.ID, secret.ID, from, ""); domain.CodeOf(err) != domain.CodeVariableSecret {
		t.Errorf("MoveVariable(secret -> collection) = %v; want secret refusal", err)
	}
	locked := envOf(t, s, into)
	locked.Readonly = true
	if err := s.Update(ctx, locked.ID, EnvironmentPatch{Readonly: &locked.Readonly}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveVariable(ctx, collection.ID, secret.ID, from, into); domain.CodeOf(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("MoveVariable(secret -> readonly) = %v; want readonly refusal", err)
	}
	if _, err := s.MoveVariable(ctx, collection.ID, "missing", from, from); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("MoveVariable(missing) = %v; want not found", err)
	}
	if len(envOf(t, s, from).Block(collection.ID)) != 1 {
		t.Fatal("refused moves changed the source")
	}
	locked.Readonly = false
	if err := s.Update(ctx, locked.ID, EnvironmentPatch{Readonly: &locked.Readonly}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveVariable(ctx, collection.ID, secret.ID, from, into); err != nil {
		t.Fatal(err)
	}
	block := envOf(t, s, into).Block(collection.ID)
	if len(block) != 1 || block[0].Value != "" || !block[0].HasValue {
		t.Fatalf("State after secret move = %+v; want withheld value", block)
	}
	value, err := s.Reveal(ctx, block[0].ID)
	if err != nil || value != secret.Value {
		t.Errorf("Reveal(moved secret) = %q, %v; want original value", value, err)
	}
}
