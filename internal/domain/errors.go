// Package domain holds the app's entities and the rules that hold for them, and nothing else: no
// database, no window, no HTTP. Every type that crosses the IPC boundary is declared here once, so
// the binding generator emits one model file for all of them.
package domain

import "errors"

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")

	// ErrStaleRevision is returned when a caller writes a text buffer or a row patch based on a
	// revision the draft has already moved past. The caller gets the current draft back and
	// re-sends its own text, so a keystroke is never silently dropped.
	ErrStaleRevision = errors.New("stale revision")

	// ErrNotAllowed marks an operation the current state forbids, as opposed to one that is
	// merely malformed — the UI phrases the two differently.
	ErrNotAllowed = errors.New("not allowed")

	// ErrUnreadable marks a body that is not written in any notation the inspector reads. It is an
	// answer and not a failure: most bodies are text, and text has no positions to inspect.
	ErrUnreadable = errors.New("not a readable document")
)
