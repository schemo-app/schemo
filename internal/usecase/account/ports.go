package account

import (
	"context"

	"schemo/internal/domain"
)

// Store is the one row this feature keeps: who the app is signed in as. It is declared here, next
// to its user, so the feature knows nothing about SQLite.
type Store interface {
	// Account is the account as it was last written. Nothing is signed in and nothing ever was
	// answers ErrNotFound, which is an ordinary state — the app is usable without an account.
	Account(ctx context.Context) (domain.Account, error)
	// Save writes account metadata without credentials.
	Save(ctx context.Context, account domain.Account) error
	// Forget removes account metadata.
	Forget(ctx context.Context) error
}

// Credentials stores refresh tokens outside the application database.
type Credentials interface {
	Get(account domain.Account) (string, error)
	Set(account domain.Account, token string) error
	Delete(account domain.Account) error
}

// Server is the other side: the OIDC endpoints a device signs in through and the API of the account
// it signs in to. One interface for one server, because a caller can never have half of it.
//
// Every method takes the address it is about: the account is what the app keeps, and the client
// underneath is stateless — the same process may well talk to one server today and to another one
// tomorrow.
type Server interface {
	// Discover reads the server's discovery document and says whether the address is one this app
	// can sign in to. It is what a window checks an address with before offering to sign in there:
	// it answers without an account and creates nothing on the server.
	Discover(ctx context.Context, address string) error
	// Authorize asks for a device code: what the person has to confirm, and where.
	Authorize(ctx context.Context, address string, device domain.Device) (domain.Challenge, error)
	// Tokens waits for the person to confirm the code and answers the pair of tokens. It returns when
	// the code is confirmed, refused, expired, or the context is cancelled — there is no third thing
	// for it to do in between.
	Tokens(ctx context.Context, address string, challenge domain.Challenge) (domain.Tokens, error)
	// Refresh trades a refresh token for a new pair, which is how a device stays signed in without
	// anybody typing anything.
	Refresh(ctx context.Context, address, refresh string) (domain.Tokens, error)

	Me(ctx context.Context, address, access string) (domain.Account, error)
	Sessions(ctx context.Context, address, access string) ([]domain.Session, error)
	EndSession(ctx context.Context, address, access, sessionID string) ([]domain.Session, error)
	EndOthers(ctx context.Context, address, access string) ([]domain.Session, error)
	SignOut(ctx context.Context, address, access string) error
	// DeleteAccount purges the account and every session onto it. It is asked with nothing: the
	// address the server would check the act against is one this app already holds, and a window that
	// made the person type it back would be asking them to spell their own name to be believed.
	DeleteAccount(ctx context.Context, address, access string) error
}

// Browser is where a person confirms the code. The app never asks for a password, so this is the
// whole of the sign-in as far as the window is concerned: it hands a URL to something that can show
// a page.
type Browser interface {
	// OpenURL is the same name the window's own port uses, so the window is what serves this one
	// without an adapter in between.
	OpenURL(url string) error
}

// Data is what this installation holds of an account: the spaces tagged with its key, with all that
// stands in them. It is the one port that touches another feature's tables, and it is here rather
// than there because signing out is what asks for it.
type Data interface {
	// Wipe removes every space of the key, and answers how many there were. Nothing is removed when
	// the key names nothing.
	Wipe(ctx context.Context, key string) (int, error)
}
