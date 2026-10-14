// Package oidc adapts OAuth device authorization and the account RPC API.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/schemo-app/proto/go/account/v1"

	"schemo/internal/domain"
	"schemo/internal/infra/channel"
)

// scopes is what the app asks for. `openid` is the identity, `email` is the address, and
// `offline_access` is the one that asks to stay signed in — without it the server hands over an
// access token and no refresh token, and the app would be signed out within the hour.
var scopes = []string{"openid", "email", "offline_access"}

// clientID is the name this app signs in under, and it is not the slug: the slug names the
// program's files on disk, while this is half of a protocol whose other half is the server's own
// list of clients. A device that introduces itself under any other name is turned away before a
// code is printed, and the person sees a refusal where the code should be.
const clientID = "schemo-app"

const maxDiscoveryBytes = 1 << 20

// Client is the OIDC client and the API client in one, because they are one server: the tokens the
// first one buys are what the second one spends.
type Client struct {
	http *http.Client

	mu sync.Mutex
	// pending is the device codes in flight, by the short code a person is looking at: the long one is
	// a secret, and it stays here rather than travelling to a window that would only draw it.
	pending map[challengeKey]*oauth2.DeviceAuthResponse
	// dialer is the channels to the servers, shared with everything else in the app that speaks to
	// one:
	// the same server over one connection, whichever feature is asking.
	dialer *channel.Dialer
}

type challengeKey struct{ address, userCode string }

func New(httpClient *http.Client, dialer *channel.Dialer) *Client {
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
		if err := channel.Endpoint(via[0].URL.String(), req.URL.String()); err != nil {
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
	return &Client{
		http:    &client,
		pending: map[challengeKey]*oauth2.DeviceAuthResponse{},
		dialer:  dialer,
	}
}

// Authorize keeps the device secret in Go and returns only the browser challenge.
func (c *Client) Authorize(ctx context.Context, address string, device domain.Device) (domain.Challenge, error) {
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
	if err := channel.Endpoint(address, where); err != nil {
		return domain.Challenge{}, err
	}
	c.remember(challengeKey{address: address, userCode: asked.UserCode}, asked)
	return domain.Challenge{UserCode: asked.UserCode, URL: where, ExpiresAt: asked.Expiry}, nil
}

// Tokens delegates polling, slow_down and expiry to oauth2.
func (c *Client) Tokens(ctx context.Context, address string, challenge domain.Challenge) (domain.Tokens, error) {
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
func (c *Client) Refresh(ctx context.Context, address, refresh string) (domain.Tokens, error) {
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

func (c *Client) config(ctx context.Context, address string) (*oauth2.Config, error) {
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
func (c *Client) Discover(ctx context.Context, address string) error {
	_, err := c.endpoints(ctx, address)
	return err
}

// endpoints reads discovery when authorizing or refreshing, avoiding stale provider URLs.
func (c *Client) endpoints(ctx context.Context, address string) (oauth2.Endpoint, error) {
	if _, err := channel.URL(address); err != nil {
		return oauth2.Endpoint{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(address, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oauth2.Endpoint{}, refuse(err)
	}
	request.Header.Set("Accept", "application/json")

	var document struct {
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
		TokenEndpoint               string `json:"token_endpoint"`
	}
	if err := c.do(request, &document); err != nil {
		return oauth2.Endpoint{}, err
	}

	// A server that does not offer the device flow is not a server this app can sign in to — and
	// saying so now is kinder than failing three steps later with a code nobody understands.
	if document.DeviceAuthorizationEndpoint == "" || document.TokenEndpoint == "" {
		return oauth2.Endpoint{}, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	for _, endpoint := range []string{document.DeviceAuthorizationEndpoint, document.TokenEndpoint} {
		if err := channel.Endpoint(address, endpoint); err != nil {
			return oauth2.Endpoint{}, err
		}
	}

	endpoints := oauth2.Endpoint{
		DeviceAuthURL: document.DeviceAuthorizationEndpoint,
		TokenURL:      document.TokenEndpoint,
	}
	return endpoints, nil
}

func (c *Client) do(request *http.Request, into any) error {
	response, err := c.http.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return request.Context().Err()
		}
		return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.Refuse(domain.CodeServerRefused, nil, domain.Args{"status": strconv.Itoa(response.StatusCode)})
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryBytes+1))
	if err != nil {
		return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}
	if len(body) > maxDiscoveryBytes {
		return domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	return nil
}

func (c *Client) remember(key challengeKey, response *oauth2.DeviceAuthResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, held := range c.pending {
		if time.Now().After(held.Expiry) {
			delete(c.pending, key)
		}
	}
	c.pending[key] = response
}

func (c *Client) take(key challengeKey) (*oauth2.DeviceAuthResponse, error) {
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

// call is one API request: the channel, the token in the metadata, and the refusal it came back
// with.
func call[T any](c *Client, ctx context.Context, address, access string,
	do func(context.Context, account.AccountServiceClient) (T, error)) (T, error) {
	var nothing T

	conn, err := c.dialer.Client(address)
	if err != nil {
		return nothing, err
	}

	answer, err := do(channel.Authorized(ctx, access), account.NewAccountServiceClient(conn))
	if err != nil {
		return nothing, channel.FromStatus(err)
	}
	return answer, nil
}
