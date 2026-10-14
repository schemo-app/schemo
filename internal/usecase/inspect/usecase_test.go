package inspect

import (
	"errors"
	"testing"

	"schemo/internal/domain"
)

func TestABodyIsOpenedOnceAndAskedAboutByHandle(t *testing.T) {
	uc := NewUseCase(nil, nil)
	opened, err := uc.Open(`{"a": {"b": 1}}`, "application/json")
	if err != nil || opened.ID == "" || opened.Kind != domain.DocJSON {
		t.Fatalf("Open = %+v, %v, want a JSON handle", opened, err)
	}
	again, _ := uc.Open(`{"a": {"b": 1}}`, "application/json")
	if again.ID != opened.ID {
		t.Errorf("the same body opened twice got %q and %q, want one handle", opened.ID, again.ID)
	}

	view, err := uc.At(opened.ID, 12)
	if err != nil || view.Path != "$.a.b" {
		t.Errorf("At = %+v, %v, want $.a.b", view, err)
	}
}

func TestATextIsNotADocumentAndThatIsNotAnError(t *testing.T) {
	opened, err := NewUseCase(nil, nil).Open("plain words", "text/plain")
	if err != nil || opened.ID != "" {
		t.Errorf("Open = %+v, %v, want no handle and no error", opened, err)
	}
}

func TestTheOldestDocumentIsForgottenAndAskedForAgainByOpening(t *testing.T) {
	uc := NewUseCase(nil, nil)
	first, _ := uc.Open(`{"n": 0}`, "application/json")
	for i := 1; i <= kept; i++ {
		if _, err := uc.Open(`{"n": `+string(rune('0'+i))+`}`, "application/json"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := uc.At(first.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("At on an evicted handle = %v, want ErrNotFound", err)
	}
}
