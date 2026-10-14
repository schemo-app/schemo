package draft

import (
	"context"

	"schemo/internal/domain"
)

type Store interface {
	Draft(ctx context.Context, workspaceID string, id domain.DraftID) (domain.Draft, error)
	SaveDraft(ctx context.Context, workspaceID string, draft domain.Draft) error
	// DeleteDraft drops a draft; one that was never written is not an error, because most drafts
	// never are.
	DeleteDraft(ctx context.Context, workspaceID string, id domain.DraftID) error
}

type Jar interface {
	CookieHeader(ctx context.Context, workspace, rawURL string) (string, error)
}

// NetDefaults is what the engine decides about every request by itself: the address a typed one is
// completed into, the name the app calls itself, and whether the server is allowed to compress. The
// editor draws these before a send, so they are read from the engine and not guessed at here.
type NetDefaults interface {
	CompleteURL(raw string) string
	WireDefaults() (userAgent string, acceptEncoding bool)
}

type FileSource interface {
	Read(path string) ([]byte, error)
}

type VariableSource interface {
	Missing(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
	SubstituteTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		mask bool,
		envID string,
	) ([]string, error)
	// ReferenceTexts is the same substitution for an export that leaves secret values out: a secret
	// resolves to a reference to itself, its `{{name}}` standing where the value would.
	ReferenceTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
	MaskTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
}

type AuthMaterializer interface {
	Materialize(ctx context.Context, auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput,
		error)
	Project(auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput, error)
	Obtain(ctx context.Context, auth domain.Auth) error
	Forget(auth domain.Auth)
	Held(auth domain.Auth) domain.AuthToken
}
