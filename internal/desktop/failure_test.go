package desktop

import (
	"errors"
	"fmt"
	"testing"

	"schemo/internal/domain"
)

// What the window reads off a rejected call. The code and the args have to be there; the sentinel,
// the English text and Go's own fields must not.
func TestARefusalCrossesAsItsCode(t *testing.T) {
	encoded := marshalFailure(domain.Refuse(domain.CodeNameTooLong, domain.ErrNotAllowed,
		domain.Args{"max": "40"}))

	if got, want := string(encoded), `{"code":"nameTooLong","args":{"max":"40"}}`; got != want {
		t.Errorf("marshalled = %s, want %s", got, want)
	}
}

// A site that wraps a bare sentinel says nothing, and the window still learns what kind of refusal
// it was — that is what keeps most call sites out of this work.
func TestASentinelStillCrosses(t *testing.T) {
	encoded := marshalFailure(fmt.Errorf("node %s: %w", "abc", domain.ErrNotFound))

	if got, want := string(encoded), `{"code":"notFound"}`; got != want {
		t.Errorf("marshalled = %s, want %s", got, want)
	}
}

// Nothing comes back for an error the app did not build: the option then falls through to Wails'
// own marshaller, and the window shows the machine's message rather than a sentence nobody wrote.
func TestAForeignErrorCrossesAsItself(t *testing.T) {
	if encoded := marshalFailure(errors.New("disk on fire")); encoded != nil {
		t.Errorf("marshalled = %s, want nothing so the default marshaller takes it", encoded)
	}
}
