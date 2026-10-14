package oidc

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/account/v1"

	"schemo/internal/domain"
)

// The account API, in the app's own words. Every method here is a thin reading of one call — the
// decisions about when to make it are the use case's, and this is only the translation.

// Me is who the account is, as the server describes it. The session id in the answer is what makes
// this device recognisable in the list of them. What the account is entitled to is not in it: that
// is the organisation's, and `infra/server` asks for it on its own.
func (c *Client) Me(ctx context.Context, address, access string) (domain.Account, error) {
	answer, err := call(c, ctx, address, access,
		func(ctx context.Context, client account.AccountServiceClient) (*account.MeResponse, error) {
			return client.Me(ctx, &account.MeRequest{})
		})
	if err != nil {
		return domain.Account{}, err
	}

	account := answer.GetAccount()
	return domain.Account{
		UserID:    account.GetId(),
		Email:     account.GetEmail(),
		Name:      account.GetName(),
		SessionID: answer.GetSessionId(),
	}, nil
}

func (c *Client) Sessions(ctx context.Context, address, access string) ([]domain.Session, error) {
	answer, err := call(c, ctx, address, access,
		func(ctx context.Context,
			client account.AccountServiceClient) (*account.ListSessionsResponse, error) {
			return client.ListSessions(ctx, &account.ListSessionsRequest{})
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

// EndSession ends one device. The answer is the list that is left, so that the window redraws
// without asking again.
func (c *Client) EndSession(ctx context.Context, address, access, sessionID string) (
	[]domain.Session, error) {
	answer, err := call(c, ctx, address, access,
		func(ctx context.Context,
			client account.AccountServiceClient) (*account.EndSessionResponse, error) {
			return client.EndSession(ctx,
				account.EndSessionRequest_builder{SessionId: sessionID}.Build())
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

func (c *Client) EndOthers(ctx context.Context, address, access string) ([]domain.Session, error) {
	answer, err := call(c, ctx, address, access,
		func(ctx context.Context,
			client account.AccountServiceClient) (*account.EndOtherSessionsResponse, error) {
			return client.EndOtherSessions(ctx, &account.EndOtherSessionsRequest{})
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

// SignOut ends this device's session on the server. It is called on the way out and its answer is
// nothing: the app has already decided to forget the account, and a server that disagrees is a
// server to sort out the next time somebody signs in.
func (c *Client) SignOut(ctx context.Context, address, access string) error {
	_, err := call(c, ctx, address, access,
		func(ctx context.Context,
			client account.AccountServiceClient) (*account.SignOutResponse, error) {
			return client.SignOut(ctx, &account.SignOutRequest{})
		})
	return err
}

// DeleteAccount purges the account itself, and every session onto it with it. The confirmation is
// left empty: the contract's field is the address a person types, which the server checks against
// its own row and refuses when the two differ, and this app makes the act deliberate in its own
// window rather than by asking for an address it already holds.
func (c *Client) DeleteAccount(ctx context.Context, address, access string) error {
	_, err := call(c, ctx, address, access,
		func(ctx context.Context,
			client account.AccountServiceClient) (*account.DeleteAccountResponse, error) {
			return client.DeleteAccount(ctx, account.DeleteAccountRequest_builder{}.Build())
		})
	return err
}

// sessions is the contract's list as the app's own.
func sessions(rows []*account.Session) []domain.Session {
	out := make([]domain.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Session{
			ID: row.GetId(),
			Device: domain.Device{
				Name:       row.GetDevice().GetName(),
				Platform:   row.GetDevice().GetPlatform(),
				App:        row.GetDevice().GetApp(),
				AppVersion: row.GetDevice().GetAppVersion(),
			},
			IP:         row.GetIp(),
			CreatedAt:  moment(row.GetCreatedAt()),
			LastSeenAt: moment(row.GetLastSeenAt()),
			IsCurrent:  row.GetIsCurrent(),
		})
	}
	return out
}

// moment is a timestamp that may not be there at all: the server leaves one out rather than sending
// 1970, and a session without one is a session whose time nobody knows.
func moment(stamp *timestamppb.Timestamp) time.Time {
	if stamp == nil {
		return time.Time{}
	}
	return stamp.AsTime()
}

// refuse turns what a call failed with into the app's own refusal.
//
// What matters to everything above is one distinction: a server that answered has an opinion — and
// the opinion decides whether the account is still good — while a server that did not answer is
// merely away, and the next click may well work.
func refuse(err error) error {
	if err == nil {
		return nil
	}
	if domain.CodeOf(err) != "" {
		return err
	}
	return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
}
