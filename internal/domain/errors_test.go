package domain

import (
	"errors"
	"fmt"
	"testing"
)

// The window words a refusal from its code, so the code has to survive being an error: wrapped,
// joined, and marshalled for the bridge.
func TestARefusalKeepsItsCodeThroughAWrap(t *testing.T) {
	refusal := Refuse(CodeFileTooLarge, ErrNotAllowed, Args{"path": "/x/photo.png", "limit": "8"})
	wrapped := fmt.Errorf("sending the body: %w", refusal)

	if got := CodeOf(wrapped); got != CodeFileTooLarge {
		t.Errorf("CodeOf = %q, want the code the refusal was built with", got)
	}
	if !errors.Is(wrapped, ErrNotAllowed) {
		t.Error("a wrapped refusal no longer answers its sentinel")
	}
	if failure := AsFailure(wrapped); failure == nil || failure.Args["limit"] != "8" {
		t.Errorf("AsFailure = %+v, want the args the sentence needs", failure)
	}
}

// A site that says nothing but the sentinel still tells the window what kind of refusal it was: the
// sentinel is the code. This is what leaves most of the app untouched.
func TestASentinelIsACodeOfItsOwn(t *testing.T) {
	cases := []struct {
		err  error
		want Code
	}{
		{fmt.Errorf("draft %s: %w", "abc", ErrNotFound), CodeNotFound},
		{fmt.Errorf("parent %s: %w", "xyz", ErrNotAllowed), CodeNotAllowed},
		{fmt.Errorf("duplicate: %w", ErrConflict), CodeConflict},
		{fmt.Errorf("row abc: %w", ErrStaleRevision), CodeStaleRevision},
		{errors.New("disk on fire"), ""}, // an error the app did not build has no code
	}

	for _, tc := range cases {
		if got := CodeOf(tc.err); got != tc.want {
			t.Errorf("CodeOf(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// Two refusals with the same values must read the same in a log, whatever order the map was built
// in.
func TestAFailureReadsTheSameTwice(t *testing.T) {
	first := Refuse(CodeFileTooLarge, ErrNotAllowed, Args{"path": "/x", "limit": "8"})
	second := Refuse(CodeFileTooLarge, ErrNotAllowed, Args{"limit": "8", "path": "/x"})

	if first.Error() != second.Error() {
		t.Errorf("%q and %q differ, want the same line for the same values", first, second)
	}
	if got, want := first.Error(), "fileTooLarge(limit=8, path=/x)"; got != want {
		t.Errorf("Error = %q, want %q", got, want)
	}
}
