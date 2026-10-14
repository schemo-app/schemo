package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// An imported environment arrives whole — its name, its colour and its variables — and nothing is
// activated: importing is not switching to.
func TestImportWritesTheEnvironmentWithoutActivating(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	_, local := seed(t, u, "Local")

	created, err := u.Import(ctx, domain.EnvironmentDraft{
		Name:  "  Staging  ",
		Color: "#ff9500",
		Variables: []domain.Variable{
			{Name: "base_url", Value: "https://staging.example.com", Enabled: true},
			{Name: "api_key", Value: "sk-1", Kind: domain.VariableSecret, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if created.ActiveID != local.ID {
		t.Errorf("activeId = %q, want the one that was active before the import", created.ActiveID)
	}
	if len(created.Environments) != 2 {
		t.Fatalf("environments = %d, want the imported one beside the seeded one",
			len(created.Environments))
	}

	imported := created.Environments[1]
	if imported.Name != "Staging" || imported.Color != "#ff9500" {
		t.Errorf("imported = %+v, want the file's own name and colour", imported)
	}
	if len(imported.Vars) != 2 {
		t.Fatalf("vars = %+v, want both", imported.Vars)
	}
	if url := imported.Vars[0]; url.Name != "base_url" ||
		url.Value != "https://staging.example.com" || url.Enabled != true {
		t.Errorf("base_url = %+v, want the value as it came", url)
	}
	if key := imported.Vars[1]; key.Kind != domain.VariableSecret || !key.HasValue ||
		key.Value != "" {
		t.Errorf("api_key = %+v, want a secret with its value held back", key)
	}
	// The value is there, which is what the snapshot's hasValue says; Reveal is what shows it.
	value, err := u.Reveal(ctx, imported.Vars[1].ID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if value != "sk-1" {
		t.Errorf("revealed = %q, want what the file carried", value)
	}
}

// A name the space already holds is numbered beside it, and a name the file listed twice is written
// once with the last value: one scope holds a name once.
func TestImportNumbersATakenNameAndKeepsTheLastValue(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	seed(t, u, "Local")

	created, err := u.Import(ctx, domain.EnvironmentDraft{
		Name: "Local",
		Variables: []domain.Variable{
			{Name: "token", Value: "first"},
			{Name: "  "},
			{Name: "token", Value: "last"},
		},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	imported := created.Environments[1]
	if imported.Name != "Local 2" {
		t.Errorf("name = %q, want the number beside the taken one", imported.Name)
	}
	if len(imported.Vars) != 1 || imported.Vars[0].Name != "token" {
		t.Fatalf("vars = %+v, want the one name once", imported.Vars)
	}
	if imported.Vars[0].Value != "last" {
		t.Errorf("token = %q, want the last value", imported.Vars[0].Value)
	}
}

// A number is added even when the name already stands at the ceiling: the number is not the part to
// drop, and a clipped name is a different name.
func TestImportNumbersANameAtTheCeiling(t *testing.T) {
	u, _ := newUseCase(t)

	full := strings.Repeat("n", maxNameLength)
	seed(t, u, full)

	created, err := u.Import(context.Background(), domain.EnvironmentDraft{Name: full})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	want := strings.Repeat("n", maxNameLength-2) + " 2"
	if imported := created.Environments[1]; imported.Name != want {
		t.Errorf("name = %q, want %q", imported.Name, want)
	}
}

// A name longer than the ceiling is clipped rather than refused: a file arriving with one is not
// the person's mistake.
func TestImportClipsALongName(t *testing.T) {
	u, _ := newUseCase(t)

	created, err := u.Import(context.Background(), domain.EnvironmentDraft{
		Name: strings.Repeat("n", maxNameLength+10),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if name := created.Environments[0].Name; len([]rune(name)) != maxNameLength {
		t.Errorf("name = %q, want it clipped to the ceiling", name)
	}
}

// An environment with no name is nothing the sheet could list, so a file asking for one is refused.
func TestImportRefusesAnEmptyName(t *testing.T) {
	u, _ := newUseCase(t)

	if _, err := u.Import(context.Background(), domain.EnvironmentDraft{Name: "  "}); !errors.Is(
		err, domain.ErrNotAllowed) {
		t.Errorf("Import of an empty name = %v, want ErrNotAllowed", err)
	}
}
