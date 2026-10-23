package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"schemo/internal/domain"
	"strconv"
	"strings"
	"sync"
	"time"

	accountv1 "github.com/schemo-app/proto/go/account/v1"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The account API, in the app's own words. Every method here is a thin reading of one call — the
// decisions about when to make it are the service's, and this is only the translation.

// Me is who the account is, as the server describes it. The session id in the answer makes this
// device recognisable in the list of them. What the account is entitled to is the organisation's,
// and Client asks for it separately.
func (c *OIDC) Me(ctx context.Context, address, access string) (domain.Account, error) {
	answer, err := callAccount(c, ctx, address, access,
		func(ctx context.Context, client accountv1.AccountServiceClient) (*accountv1.MeResponse, error) {
			return client.Me(ctx, &accountv1.MeRequest{})
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

func (c *OIDC) Sessions(ctx context.Context, address, access string) ([]domain.Session, error) {
	answer, err := callAccount(c, ctx, address, access,
		func(ctx context.Context,
			client accountv1.AccountServiceClient) (*accountv1.ListSessionsResponse, error) {
			return client.ListSessions(ctx, &accountv1.ListSessionsRequest{})
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

// EndSession ends one device. The answer is the list that is left, so that the window redraws
// without asking again.
func (c *OIDC) EndSession(ctx context.Context, address, access, sessionID string) (
	[]domain.Session, error) {
	answer, err := callAccount(c, ctx, address, access,
		func(ctx context.Context,
			client accountv1.AccountServiceClient) (*accountv1.EndSessionResponse, error) {
			return client.EndSession(ctx,
				accountv1.EndSessionRequest_builder{SessionId: sessionID}.Build())
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

func (c *OIDC) EndOthers(ctx context.Context, address, access string) ([]domain.Session, error) {
	answer, err := callAccount(c, ctx, address, access,
		func(ctx context.Context,
			client accountv1.AccountServiceClient) (*accountv1.EndOtherSessionsResponse, error) {
			return client.EndOtherSessions(ctx, &accountv1.EndOtherSessionsRequest{})
		})
	if err != nil {
		return nil, err
	}
	return sessions(answer.GetSessions()), nil
}

// SignOut ends this device's session on the server. It is called on the way out and its answer is
// nothing: the app has already decided to forget the account.
func (c *OIDC) SignOut(ctx context.Context, address, access string) error {
	_, err := callAccount(c, ctx, address, access,
		func(ctx context.Context,
			client accountv1.AccountServiceClient) (*accountv1.SignOutResponse, error) {
			return client.SignOut(ctx, &accountv1.SignOutRequest{})
		})
	return err
}

// DeleteAccount purges the account and every session onto it. The confirmation is left empty: the
// contract's field is the address a person types, which the server checks against its own row, and
// this app makes the act deliberate in its own window instead.
func (c *OIDC) DeleteAccount(ctx context.Context, address, access string) error {
	_, err := callAccount(c, ctx, address, access,
		func(ctx context.Context,
			client accountv1.AccountServiceClient) (*accountv1.DeleteAccountResponse, error) {
			return client.DeleteAccount(ctx, accountv1.DeleteAccountRequest_builder{}.Build())
		})
	return err
}

// sessions is the contract's list as the app's own.
func sessions(rows []*accountv1.Session) []domain.Session {
	out := make([]domain.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Session{
			ID: row.GetId(),
			Device: domain.Device{
				Name:       row.GetDevice().GetName(),
				Platform:   row.GetDevice().GetPlatform(),
				App:        row.GetDevice().GetApp().GetName(),
				AppVersion: row.GetDevice().GetApp().GetVersion(),
			},
			IP:         row.GetIp(),
			CreatedAt:  fromTimestamp(row.GetCreatedAt()),
			LastSeenAt: fromTimestamp(row.GetLastSeenAt()),
			IsCurrent:  row.GetIsCurrent(),
		})
	}
	return out
}

// fromTimestamp is a timestamp that may not be there at all: the server leaves one out rather than sending
// 1970, and a session without one is a session whose time nobody knows.
func fromTimestamp(stamp *timestamppb.Timestamp) time.Time {
	if stamp == nil {
		return time.Time{}
	}
	return stamp.AsTime()
}

// refuse turns what a call failed with into the app's own refusal.
//
// The distinction that matters above: a server that answered has an opinion, which decides whether
// the account is still good, while a server that did not answer is merely away and the next click
// may work.
func refuse(err error) error {
	if err == nil {
		return nil
	}
	if domain.CodeOf(err) != "" {
		return err
	}
	return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
}

// scopes is what the app asks for: `openid` is the identity, `email` the address, and
// `offline_access` asks to stay signed in. Without it the server hands over no refresh token and
// the app would be signed out within the hour.
var scopes = []string{"openid", "email", "offline_access"}

// clientID is the name this app signs in under. It is not the slug: it is half of a protocol whose
// other half is the server's own list of clients, and a device introducing itself under any other
// name is turned away before a code is printed.
const clientID = "schemo-app"

const maxDiscoveryBytes = 1 << 20

// OIDC is the OIDC client and the API client in one, because they are one server: the tokens the
// first one buys are what the second one spends.
type OIDC struct {
	http *http.Client

	mu sync.Mutex
	// pending is the device codes in flight, by the short code a person is looking at: the long one is
	// a secret, and it stays here rather than travelling to a window that would only draw it.
	pending map[challengeKey]*oauth2.DeviceAuthResponse
	// dialer is the channels to the servers, shared with everything else in the app that speaks to
	// one: the same server over one connection, whichever feature is asking.
	dialer *Dialer
}

type challengeKey struct{ address, userCode string }

func NewOIDC(httpClient *http.Client, dialer *Dialer) *OIDC {
	client := *httpClient
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = accountTransport{base: transport}
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// OAuth POST bodies contain device and refresh credentials; never replay them on redirect.
		if via[0].Method != http.MethodGet && via[0].Method != http.MethodHead {
			return domain.Refuse(domain.CodeServerRefused, nil, nil)
		}
		if err := Endpoint(via[0].URL.String(), req.URL.String()); err != nil {
			return err
		}
		if redirect != nil {
			return redirect(req, via)
		}
		if len(via) >= 10 {
			return domain.Refuse(domain.CodeServerRefused, nil, nil)
		}
		return nil
	}
	return &OIDC{
		http:    &client,
		pending: map[challengeKey]*oauth2.DeviceAuthResponse{},
		dialer:  dialer,
	}
}

// Authorize keeps the device secret in Go and returns only the browser challenge.
func (c *OIDC) Authorize(ctx context.Context, address string, device domain.Device) (domain.Challenge, error) {
	config, err := c.config(ctx, address)
	if err != nil {
		return domain.Challenge{}, err
	}
	client := *c.http
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &deviceTransport{base: base, ctx: ctx, device: device}
	client.Transport = transport
	asked, err := config.DeviceAuth(context.WithValue(ctx, oauth2.HTTPClient, &client))
	if transport.body != nil {
		defer transport.body.Close()
	}
	if err != nil {
		return domain.Challenge{}, oauthError(ctx, err, domain.CodeServerRefused)
	}
	where := asked.VerificationURIComplete
	if where == "" {
		where = asked.VerificationURI
	}
	if asked.DeviceCode == "" || asked.UserCode == "" || where == "" || !asked.Expiry.After(time.Now()) || asked.Interval < 0 {
		return domain.Challenge{}, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if err := Endpoint(address, where); err != nil {
		return domain.Challenge{}, err
	}
	c.remember(challengeKey{address: address, userCode: asked.UserCode}, asked)
	return domain.Challenge{UserCode: asked.UserCode, URL: where, ExpiresAt: asked.Expiry}, nil
}

// Tokens delegates polling, slow_down and expiry to oauth2.
func (c *OIDC) Tokens(ctx context.Context, address string, challenge domain.Challenge) (domain.Tokens, error) {
	asked, err := c.take(challengeKey{address: address, userCode: challenge.UserCode})
	if err != nil {
		return domain.Tokens{}, err
	}
	config, err := c.config(ctx, address)
	if err != nil {
		return domain.Tokens{}, err
	}
	token, err := config.DeviceAccessToken(context.WithValue(ctx, oauth2.HTTPClient, c.http), asked)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return domain.Tokens{}, domain.Refuse(domain.CodeSignInFailed, domain.ErrNotAllowed, nil)
		}
		return domain.Tokens{}, oauthError(ctx, err, domain.CodeSignInFailed)
	}
	return tokensOf(token), nil
}

// Refresh uses oauth2's token source without retaining credentials in the client.
func (c *OIDC) Refresh(ctx context.Context, address, refresh string) (domain.Tokens, error) {
	config, err := c.config(ctx, address)
	if err != nil {
		return domain.Tokens{}, err
	}
	token, err := config.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, c.http), &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		return domain.Tokens{}, oauthError(ctx, err, domain.CodeNotSignedIn)
	}
	return tokensOf(token), nil
}

func (c *OIDC) config(ctx context.Context, address string) (*oauth2.Config, error) {
	endpoint, err := c.endpoints(ctx, address)
	if err != nil {
		return nil, err
	}
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return &oauth2.Config{ClientID: clientID, Scopes: scopes, Endpoint: endpoint}, nil
}

func tokensOf(token *oauth2.Token) domain.Tokens {
	return domain.Tokens{Access: token.AccessToken, Refresh: token.RefreshToken, ExpiresAt: token.Expiry}
}

func oauthError(ctx context.Context, err error, rejected domain.Code) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	var response *oauth2.RetrieveError
	if errors.As(err, &response) {
		if response.ErrorCode == "temporarily_unavailable" || response.ErrorCode == "server_error" ||
			(response.Response != nil && response.Response.StatusCode >= http.StatusInternalServerError) {
			return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
		}
		if response.ErrorCode != "" {
			if rejected == domain.CodeNotSignedIn && response.ErrorCode != "invalid_grant" && response.ErrorCode != "access_denied" {
				return domain.Refuse(domain.CodeServerRefused, nil, nil)
			}
			return domain.Refuse(rejected, domain.ErrNotAllowed, nil)
		}
		return domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	var network interface{ Timeout() bool }
	if errors.As(err, &network) {
		return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}
	return domain.Refuse(domain.CodeServerRefused, nil, nil)
}

type deviceTransport struct {
	base   http.RoundTripper
	ctx    context.Context
	device domain.Device
	body   io.ReadCloser
}

func (t *deviceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// oauth2 v0.37 DeviceAuth omits the caller context and response-body close.
	request = request.Clone(t.ctx)
	setDevice(request, t.device)
	response, err := t.base.RoundTrip(request)
	if err == nil {
		t.body = response.Body
	}
	return response, err
}

// Discover validates the provider's current device authorization endpoints.
func (c *OIDC) Discover(ctx context.Context, address string) error {
	_, err := c.endpoints(ctx, address)
	return err
}

// endpoints reads discovery when authorizing or refreshing, avoiding stale provider URLs.
func (c *OIDC) endpoints(ctx context.Context, address string) (oauth2.Endpoint, error) {
	if _, err := URL(address); err != nil {
		return oauth2.Endpoint{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(address, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oauth2.Endpoint{}, refuse(err)
	}
	request.Header.Set("Accept", "application/json")

	var document struct {
		Issuer                      string `json:"issuer"`
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
		TokenEndpoint               string `json:"token_endpoint"`
	}
	readFrom, err := c.do(request, &document)
	if err != nil {
		return oauth2.Endpoint{}, err
	}
	if !namesItself(document.Issuer, address, readFrom) {
		return oauth2.Endpoint{}, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}

	// A server that does not offer the device flow is not a server this app can sign in to — and
	// saying so now is kinder than failing three steps later with a code nobody understands.
	if document.DeviceAuthorizationEndpoint == "" || document.TokenEndpoint == "" {
		return oauth2.Endpoint{}, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	for _, endpoint := range []string{document.DeviceAuthorizationEndpoint, document.TokenEndpoint} {
		if err := Endpoint(address, endpoint); err != nil {
			return oauth2.Endpoint{}, err
		}
	}

	endpoints := oauth2.Endpoint{
		DeviceAuthURL: document.DeviceAuthorizationEndpoint,
		TokenURL:      document.TokenEndpoint,
	}
	return endpoints, nil
}

// namesItself says whether a discovery document claims the place it was read from (OpenID Connect
// Discovery §4.3): its issuer is the address the app was given, or the origin the document was
// finally fetched from when the address redirects to the identity provider. Without it a server
// could hand out the document of another issuer. A trailing slash is not a difference.
func namesItself(issuer, address string, readFrom *url.URL) bool {
	issuer = strings.TrimSuffix(issuer, "/")
	if issuer == "" {
		return false
	}
	if issuer == strings.TrimSuffix(address, "/") {
		return true
	}
	claimed, err := url.Parse(issuer)
	return err == nil && readFrom != nil && claimed.Scheme == readFrom.Scheme && claimed.Host == readFrom.Host
}

// do reads one JSON document and answers where it was finally read from, since a redirect may have
// carried the request elsewhere.
func (c *OIDC) do(request *http.Request, into any) (*url.URL, error) {
	response, err := c.http.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		return nil, domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, domain.Args{"status": strconv.Itoa(response.StatusCode)})
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryBytes+1))
	if err != nil {
		return nil, domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}
	if len(body) > maxDiscoveryBytes {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if response.Request != nil {
		return response.Request.URL, nil
	}
	return request.URL, nil
}

func (c *OIDC) remember(key challengeKey, response *oauth2.DeviceAuthResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, held := range c.pending {
		if time.Now().After(held.Expiry) {
			delete(c.pending, key)
		}
	}
	c.pending[key] = response
}

func (c *OIDC) take(key challengeKey) (*oauth2.DeviceAuthResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, ok := c.pending[key]
	if !ok {
		return nil, domain.Refuse(domain.CodeSignInFailed, domain.ErrNotFound, nil)
	}
	delete(c.pending, key)
	return response, nil
}

func setDevice(request *http.Request, device domain.Device) {
	request.Header.Set("X-Device-Name", device.Name)
	request.Header.Set("X-Device-App", device.App)
	request.Header.Set("X-Device-Platform", device.Platform)
	request.Header.Set("X-Device-Version", device.AppVersion)
}

// callAccount is one API request: the channel, the token in the metadata, and the refusal it came back
// with.
func callAccount[T any](c *OIDC, ctx context.Context, address, access string,
	do func(context.Context, accountv1.AccountServiceClient) (T, error)) (T, error) {
	var nothing T

	conn, err := c.dialer.Client(address)
	if err != nil {
		return nothing, err
	}

	answer, err := do(Authorized(ctx, access), accountv1.NewAccountServiceClient(conn))
	if err != nil {
		return nothing, FromStatus(err)
	}
	return answer, nil
}

// HTTPClient keeps account TLS independent of request editor settings.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: http.DefaultTransport.(*http.Transport).Clone()}
}

type accountTransport struct{ base http.RoundTripper }

func (t accountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := URL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}
