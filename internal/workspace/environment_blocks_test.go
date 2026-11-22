package workspace

import (
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func saveBlock(t *testing.T, s *Environments, env, collection string, vars ...domain.Variable) {
	t.Helper()
	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: env, Collection: collection}, vars); err != nil {
		t.Fatalf("SaveVariables: %v", err)
	}
}

func resolved(t *testing.T, s *Environments, levels domain.VarLevels, text string) string {
	t.Helper()
	out, err := s.SubstituteTexts(t.Context(), levels, []string{text}, false)
	if err != nil {
		t.Fatal(err)
	}
	return out[0]
}

// The last source to hold a name wins: globals, collection, environment, its block.
func TestResolutionReadsTheBlockOfTheRequestsCollectionLast(t *testing.T) {
	s, _ := newEnvironments(t)
	env := mustCreate(t, s, "Local")
	if err := s.Activate(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVariables(t.Context(), domain.EnvScope{}, []domain.Variable{text("g", "a", "global"), text("g2", "d", "global")}); err != nil {
		t.Fatal(err)
	}
	fill(t, s, env, text("e", "b", "env"), text("e2", "c", "env"))
	saveBlock(t, s, env, "catalog", text("k", "c", "block"))

	levels := domain.VarLevels{Collection: "catalog", Variables: []domain.Variable{
		text("c1", "a", "collection"), text("c2", "b", "collection"), text("c3", "d", "collection")}}
	if got := resolved(t, s, levels, "{{a}} {{b}} {{c}} {{d}}"); got != "collection env block collection" {
		t.Errorf("catalog = %q", got)
	}
	// Another collection's block is not read.
	levels.Collection = "billing"
	if got := resolved(t, s, levels, "{{c}}"); got != "env" {
		t.Errorf("billing = %q", got)
	}
}

func TestNestedValuesUnknownNamesAndCycles(t *testing.T) {
	s, _ := newEnvironments(t)
	if err := s.SaveVariables(t.Context(), domain.EnvScope{}, []domain.Variable{
		text("1", "host", "api.local"), text("2", "port", "8080"), text("3", "base", "{{host}}:{{port}}"),
		text("4", "loop", "x{{loop}}"),
	}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, s, domain.VarLevels{}, "{{base}}/{{nope}}"); got != "api.local:8080/{{nope}}" {
		t.Errorf("nested = %q", got)
	}
	if got := resolved(t, s, domain.VarLevels{}, "{{loop}}"); got != "x{{loop}}" {
		t.Errorf("cycle = %q", got)
	}
}

func TestUpdateVariableChangesOnlyItsScopeAndPreservesUnsentValues(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	id := mustCreate(t, s, "Local")
	fill(t, s, id, text("host", "host", "api.test"), secret("token", "token", "stored-secret"))
	saveBlock(t, s, id, "catalog", text("block", "host", "catalog.test"))
	name := "apiToken"
	if err := s.UpdateVariable(ctx, domain.EnvScope{Environment: id}, "token", VariablePatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Reveal(ctx, "token"); err != nil || value != "stored-secret" {
		t.Errorf("Reveal after name patch = %q, %v; want preserved secret", value, err)
	}
	value := "new-catalog.test"
	if err := s.UpdateVariable(ctx, domain.EnvScope{Environment: id, Collection: "catalog"}, "block", VariablePatch{Value: &value}); err != nil {
		t.Fatal(err)
	}
	env := envOf(t, s, id)
	if len(env.Vars) != 2 || env.Vars[0].Value != "api.test" || env.Vars[1].Name != name || env.Block("catalog")[0].Value != value {
		t.Errorf("State after individual patches = %+v; want independent scopes", env)
	}
	name = "host"
	if err := s.UpdateVariable(ctx, domain.EnvScope{Environment: id}, "token", VariablePatch{Name: &name}); code(err) != domain.CodeNameTaken {
		t.Errorf("UpdateVariable(duplicate name) = %v; want name taken", err)
	}
	value = ""
	if err := s.UpdateVariable(ctx, domain.EnvScope{Environment: id}, "token", VariablePatch{Value: &value}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Reveal(ctx, "token"); err != nil || value != "" {
		t.Errorf("Reveal after empty value patch = %q, %v; want empty", value, err)
	}
}

func TestImportCombinesBlocksMappedToTheSameCollection(t *testing.T) {
	s, _ := newEnvironments(t)
	report, err := s.ImportDrafts(t.Context(), []domain.EnvironmentDraft{{
		Name: "Local",
		Blocks: []domain.BlockDraft{
			{Collection: 0, Variables: []domain.Variable{text("a", "host", "first")}},
			{Collection: 1, Variables: []domain.Variable{text("b", "token", "second"), text("c", "host", "ignored")}},
		},
	}}, []string{"catalog", "catalog"}, "imported")
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 2 || report.Kept != 1 {
		t.Errorf("ImportDrafts(merged collection) = %+v; want 2 added, 1 kept", report)
	}
	block := report.State.Environments[0].Block("catalog")
	if len(block) != 2 || block[0].Name != "host" || block[0].Value != "first" || block[1].Name != "token" {
		t.Errorf("Imported block = %+v; want first host and second token", block)
	}
}

func TestReorderDuplicateAndMerge(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	a := mustCreate(t, s, "A")
	b := mustCreate(t, s, "B")
	fill(t, s, a, text("a1", "x", "from-a"), text("a2", "y", "from-a"))
	fill(t, s, b, text("b1", "x", "from-b"))
	saveBlock(t, s, a, "catalog", text("ak", "token", "a-token"))

	if err := s.Reorder(ctx, []string{b, a}); err != nil {
		t.Fatal(err)
	}
	if state := mustState(t, s); state.Environments[0].ID != b {
		t.Errorf("order = %+v", state.Environments)
	}

	copied, err := s.Duplicate(ctx, a, "A (copy)")
	if err != nil {
		t.Fatal(err)
	}
	copy := envOf(t, s, copied)
	if copy.Name != "A (copy)" || copy.Readonly || len(copy.Vars) != 2 || len(copy.Block("catalog")) != 1 ||
		copy.Vars[0].ID == "a1" {
		t.Errorf("copy = %+v", copy)
	}

	if err := s.Activate(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	state := mustState(t, s)
	merged, _ := state.Find(b)
	if _, gone := state.Find(a); gone || state.ActiveID != b {
		t.Fatalf("after merge = %+v", state)
	}
	if len(merged.Vars) != 2 || merged.Vars[0].Value != "from-b" || merged.Vars[1].Name != "y" ||
		len(merged.Block("catalog")) != 1 {
		t.Errorf("merged = %+v", merged)
	}
}

func TestImportVariablesAddsOnlyMissingNames(t *testing.T) {
	s, _ := newEnvironments(t)
	env := mustCreate(t, s, "Local")
	fill(t, s, env, text("1", "timeout", "10"))
	file := []domain.Variable{text("", "timeout", "30000"), text("", "retries", "3")}
	for range 2 {
		report, err := s.ImportVariables(t.Context(), domain.EnvScope{Environment: env}, file)
		if err != nil {
			t.Fatal(err)
		}
		_ = report
	}
	vars := envOf(t, s, env).Vars
	if len(vars) != 2 || vars[0].Value != "10" || vars[1].Name != "retries" {
		t.Errorf("vars = %+v", vars)
	}
}

func TestImportDraftsFindByNameAndFillBlocks(t *testing.T) {
	s, _ := newEnvironments(t)
	local := mustCreate(t, s, "Local")
	locked := mustCreate(t, s, "Prod")
	saveBlock(t, s, local, "catalog", text("1", "baseUrl", "kept"))
	env := envOf(t, s, locked)
	env.Readonly = true
	if err := s.Update(t.Context(), env.ID, EnvironmentPatch{Readonly: &env.Readonly}); err != nil {
		t.Fatal(err)
	}

	drafts := []domain.EnvironmentDraft{
		{Name: " local ", Blocks: []domain.BlockDraft{{Collection: 0, Variables: []domain.Variable{
			text("", "baseUrl", "from-file"), text("", "token", "t")}}}},
		{Name: "QA", Variables: []domain.Variable{text("", "region", "eu")}},
		{Name: "prod", Blocks: []domain.BlockDraft{{Collection: 0, Variables: []domain.Variable{text("", "baseUrl", "p")}}}},
	}
	report, err := s.ImportDrafts(t.Context(), drafts, []string{"catalog"}, "(import)")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 2 || report.Created[0] != "QA" || report.Created[1] != "prod (import)" ||
		report.Added != 3 || report.Kept != 1 {
		t.Errorf("report = %+v", report)
	}
	block := envOf(t, s, local).Block("catalog")
	if len(block) != 2 || block[0].Value != "kept" {
		t.Errorf("local block = %+v", block)
	}
	if len(envOf(t, s, locked).Block("catalog")) != 0 {
		t.Error("a read-only environment was written into")
	}
}

func TestCollectionDeleteAndDuplicateCarryTheirBlocks(t *testing.T) {
	s, store := newEnvironments(t)
	collections := NewCollections(store, domain.WorkspaceScope(ws), platform.NewIDGen())
	ctx := t.Context()
	if err := store.SaveCollection(ctx, ws, domain.Collection{ID: "catalog", Name: "Catalog"}); err != nil {
		t.Fatal(err)
	}
	env := mustCreate(t, s, "Local")
	saveBlock(t, s, env, "catalog", text("1", "baseUrl", "http://catalog"))

	if err := collections.Duplicate(ctx, "catalog", " copy"); err != nil {
		t.Fatal(err)
	}
	blocks := envOf(t, s, env).Collections
	if len(blocks) != 2 {
		t.Fatalf("blocks after duplicate = %+v", blocks)
	}
	if err := collections.Delete(ctx, "catalog"); err != nil {
		t.Fatal(err)
	}
	blocks = envOf(t, s, env).Collections
	if _, held := blocks["catalog"]; held || len(blocks) != 1 {
		t.Errorf("blocks after delete = %+v", blocks)
	}
}
