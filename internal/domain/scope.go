package domain

import "context"

// Scope answers which space a call works in: the one the window is showing. A port rather than a
// call into the workspace use case, because features never import each other — and the same port in
// eight of them, which is why it is declared once, beside the word it answers with.
type Scope interface {
	ActiveWorkspace(ctx context.Context) (string, error)
}
