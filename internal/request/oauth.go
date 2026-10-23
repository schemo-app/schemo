package request

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"schemo/internal/domain"
)

// oauthConfig is the configuration all four grants share. Client authentication is the one thing
// the user chooses, since no server can be guessed about; the two answers are the two styles the
// library knows.
func oauthConfig(auth domain.Auth) oauth2.Config {
	return oauth2.Config{
		ClientID:     strings.TrimSpace(auth.Answer("clientId")),
		ClientSecret: auth.Answer("clientSecret"),
		Endpoint: oauth2.Endpoint{
			AuthURL:   strings.TrimSpace(auth.Answer("authUrl")),
			TokenURL:  strings.TrimSpace(auth.Answer("tokenUrl")),
			AuthStyle: authStyle(auth),
		},
		Scopes: scope(auth.Answer("scope")),
	}
}

func authStyle(auth domain.Auth) oauth2.AuthStyle {
	if auth.OrDefault("clientAuth") == clientAuthBody {
		return oauth2.AuthStyleInParams
	}
	return oauth2.AuthStyleInHeader
}

// clientAuthBody is the answer to "Client Authentication" that puts the client's own credential in
// the form rather than in a header.
const clientAuthBody = "body"

// scope splits what the user typed into the scopes a provider expects — space-separated, which is
// how OAuth writes them; a provider that wanted them some other way would be the first.
func scope(raw string) []string {
	return strings.Fields(raw)
}

// endpointParams is what the token request carries besides the grant: a provider that issues tokens
// for a named audience is told which one, and the ones that do not care never see the word.
func endpointParams(auth domain.Auth) url.Values {
	params := url.Values{}
	if audience := strings.TrimSpace(auth.Answer("audience")); audience != "" {
		params.Set("audience", audience)
	}
	return params
}

// oauthToken asks the provider for a token by the grant the user chose. Three of the four are the
// library's; the fourth needs a browser and lives in browser.go.
func oauthToken(
	ctx context.Context,
	auth domain.Auth,
	exchange codeExchange,
) (string, time.Time, error) {
	grant := auth.OrDefault("grant")

	switch grant {
	case domain.GrantClientCredentials:
		config := clientcredentials.Config{
			ClientID:       strings.TrimSpace(auth.Answer("clientId")),
			ClientSecret:   auth.Answer("clientSecret"),
			TokenURL:       strings.TrimSpace(auth.Answer("tokenUrl")),
			Scopes:         scope(auth.Answer("scope")),
			EndpointParams: endpointParams(auth),
			AuthStyle:      authStyle(auth),
		}
		token, err := config.Token(ctx)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("asking for a token as the client: %w", err)
		}
		return token.AccessToken, token.Expiry, nil

	case domain.GrantPassword:
		config := oauthConfig(auth)
		token, err := config.PasswordCredentialsToken(ctx, auth.Answer("owner"),
			auth.Answer("ownerPassword"))
		if err != nil {
			return "", time.Time{}, fmt.Errorf("asking for a token as the user: %w", err)
		}
		return token.AccessToken, token.Expiry, nil

	case domain.GrantAuthorizationCode, domain.GrantImplicit:
		// These two need the person: without a browser to open — a test, or a platform the app
		// cannot reach one on — saying so is better than a request that would hang.
		if exchange == nil {
			return "", time.Time{}, fmt.Errorf("asking for a token with the %s grant: %w", grant,
				domain.ErrNotAllowed)
		}
		return exchange(ctx, auth)

	default:
		// A grant nobody implements is not one to guess at by falling back to another.
		return "", time.Time{}, fmt.Errorf("asking for a token with the %q grant: %w", grant,
			domain.ErrNotAllowed)
	}
}

// fetcher is the scheme's way of getting what it carries, called only by Materialize: the window
// draws rows without asking anybody for anything. The exchange is passed in because the registry is
// shared by every materializer, only some of which have a browser.
type fetcher func(ctx context.Context, auth domain.Auth, exchange codeExchange) (token string,
	expires time.Time, err error)

// codeExchange is the two grants that need a browser: the user says yes at the provider and comes
// back with a token. It is a function so the materializer can be built without one (a test, or a
// build with no browser).
type codeExchange func(ctx context.Context, auth domain.Auth) (string, time.Time, error)
