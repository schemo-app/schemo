package account

import (
	"time"

	"schemo/internal/domain"
)

// Status is the account as the window draws it: the stored row, a sign-in in flight, and whether
// the server is there.
type Status struct {
	SignedIn    bool           `json:"signedIn"`
	SessionOnly bool           `json:"sessionOnly"`
	Account     domain.Account `json:"account"`
	// Address is where the account actually talks: the stored choice, or the app's own server when
	// nobody chose one. The stored row keeps "" for the default, and a panel link needs the address
	// rather than the answer "the default one".
	Address string `json:"address"`
	// Waiting indicates an active sign-in flow waiting for browser confirmation.
	Waiting   bool             `json:"waiting"`
	Challenge domain.Challenge `json:"challenge"`
	// Failure contains the last sign-in error domain code, if any.
	Failure *domain.Failure `json:"failure,omitempty"`
	// Reachability reflects the outcome of the latest server communication attempt.
	Reachability Reachability `json:"reachability"`
}

// Reachability is what the last call to the server came to: whether the other side answered, and
// when it last did — a failed attempt does not move that moment, because "last contact" is not
// "last attempt". It is not part of the account: the account is a row this app keeps, and a
// server that is down or a laptop that is offline is weather rather than a fact about the person.
//
// Checked tells "not asked yet" from "did not answer", because a window words them differently:
// before the first call there is nothing to say, and after it there is.
type Reachability struct {
	Checked   bool      `json:"checked"`
	Reachable bool      `json:"reachable"`
	At        time.Time `json:"at"`
}
