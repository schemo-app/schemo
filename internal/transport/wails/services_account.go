package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/account"
)

// AccountService is the account as the window sees it: who the app is signed in as, the sign-in
// itself, and the devices it is signed in on.
//
// Every method answers the whole state rather than the piece that changed — a window drawing the
// account wants one answer, not a puzzle of them — and every window is told as well, because the
// settings window is not the only one that draws it.
type AccountService struct {
	account *account.UseCase
}

func NewAccountService(uc *account.UseCase) *AccountService {
	return &AccountService{account: uc}
}

// Status is what the settings window draws: nothing signed in, a sign-in going on, or an account.
func (s *AccountService) Status(ctx context.Context) (account.Status, error) {
	return s.account.Status(ctx)
}

// Check asks the server whether it is there, and answers the state with the verdict in it. It is
// what a window calls when it opens the account, and it never fails over an unreachable server:
// that is the answer it went looking for.
func (s *AccountService) Check(ctx context.Context) (account.Status, error) {
	return s.account.Check(ctx)
}

// SetServer remembers a server choice and clears the old session when the server changes.
func (s *AccountService) SetServer(ctx context.Context, server string) (account.Status, error) {
	return s.account.SetServer(ctx, server)
}

// CheckServer asks whether an address is a server this app can sign in to. It answers the state
// unchanged, and refuses in the catalogue's own words when the address is not one.
func (s *AccountService) CheckServer(ctx context.Context, server string) (account.Status, error) {
	return s.account.CheckServer(ctx, server)
}

// Begin starts a sign-in: the server hands over a code, the browser opens on the page where it is
// confirmed, and the answer comes back as an event — see TopicChanged. The call returns as soon as
// there is a code to show, because waiting is the person's business and not the window's.
func (s *AccountService) Begin(ctx context.Context, server string) (account.Status, error) {
	return s.account.Begin(ctx, server)
}

// Cancel stops waiting for a code that nobody is going to confirm. The code itself lives on until
// it runs out on its own: the app cannot call it back.
func (s *AccountService) Cancel(ctx context.Context) (account.Status, error) {
	return s.account.Cancel(ctx)
}

// SignOut forgets the account on this machine. It works with the server out of reach: this is a
// local app, and being unable to reach anybody is not a reason to stay signed in. With `wipe` the
// spaces of the account, with everything in them, are deleted as well — the choice the window asks
// for before it calls this.
func (s *AccountService) SignOut(ctx context.Context, wipe bool) (account.Status, error) {
	return s.account.SignOut(ctx, wipe)
}

// Sessions is where else the account is signed in. It is a call to the server, so it can fail with
// the server being away — which the window says in its own words rather than showing an empty list.
func (s *AccountService) Sessions(ctx context.Context) ([]domain.Session, error) {
	return s.account.Sessions(ctx)
}

func (s *AccountService) EndSession(ctx context.Context,
	sessionID string) ([]domain.Session, error) {
	return s.account.EndSession(ctx, sessionID)
}

// Delete purges the account and everything this device holds of it. It is asked with nothing: the
// window is where the act was made deliberate, and the use case asks the server whether it may
// happen at all.
func (s *AccountService) Delete(ctx context.Context) (account.Status, error) {
	return s.account.Delete(ctx)
}

func (s *AccountService) EndOthers(ctx context.Context) ([]domain.Session, error) {
	return s.account.EndOthers(ctx)
}
