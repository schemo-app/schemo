package openapi

import (
	"strings"

	"schemo/internal/domain"
)

// operationAuth is what the operation itself asks for. An operation that says nothing about
// security inherits the collection's (nil auth); an empty list means it asks for nothing, which is
// `none`.
func (r *reader) operationAuth(op operation) *domain.Auth {
	if op.Security == nil {
		return nil
	}
	if len(op.Security) == 0 {
		return &domain.Auth{Type: domain.AuthNone}
	}
	if auth := r.scheme(op.Security[0]); auth != nil {
		return auth
	}
	// A scheme this app cannot read leaves the request with no authorization at all: inheriting the
	// collection's would send credentials the document never asked for.
	return &domain.Auth{Type: domain.AuthNone}
}

// documentAuth is what the whole document asks for, and it is the collection's own: the requests
// that say nothing inherit exactly this.
func (r *reader) documentAuth() *domain.Auth {
	if len(r.doc.Security) == 0 {
		return nil
	}
	return r.scheme(r.doc.Security[0])
}

func (r *reader) scheme(requirement requirement) *domain.Auth {
	name, ok := requirement.first()
	if !ok {
		return nil
	}
	schemes := r.doc.SwaggerDefs
	if !r.doc2 && r.doc.Components != nil {
		schemes = r.doc.Components.SecuritySchemes
	}
	declared, ok := schemes[name]
	if !ok {
		return nil
	}
	return r.authOf(declared, requirement[name])
}

// authOf maps one declared scheme. A scheme this app has no shape for is counted and left out, so
// the request visibly has no authorization instead of a wrong one. The scopes ride along: an OAuth
// 2.0 scheme is asked for with the scopes the requirement lists.
func (r *reader) authOf(declared map[string]any, scopes []string) *domain.Auth {
	auth := r.schemeOf(declared, scopes)
	if auth == nil {
		r.out.LeftOut(domain.CodeSkippedAuthScheme, 1)
	}
	return auth
}

func (r *reader) schemeOf(declared map[string]any, scopes []string) *domain.Auth {
	kind, _ := declared["type"].(string)
	// The spec spells the api-key scheme `apiKey`, and documents in the wild disagree about the case
	// of the type: it is read without one.
	switch strings.ToLower(kind) {
	case "http":
		scheme, _ := declared["scheme"].(string)
		switch strings.ToLower(scheme) {
		case "bearer":
			r.declare("token", "", true)
			return &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{
				"prefix": "Bearer", "token": "{{token}}",
			}}
		case "basic":
			return &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{}}
		}
	case "basic":
		return &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{}}
	case "apikey":
		key, _ := declared["name"].(string)
		place, _ := declared["in"].(string)
		if place != domain.PlaceQuery {
			place = domain.PlaceHeader
		}
		r.declare("apiKey", "", true)
		return &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{
			"key": key, "value": "{{apiKey}}", "place": place,
		}}
	case "oauth2":
		return r.oauth2(declared, scopes)
	case "openidconnect":
		// The discovery URL is not something this app reads; the grant is the authorization code
		// flow the document's own security asks for.
		return r.oauth2Auth(domain.GrantAuthorizationCode, "", "", scopes)
	}
	return nil
}

// oauth2 reads one scheme's flow. A document may offer several; the first present wins in the order
// authorization code, implicit, password, client credentials (as in Hoppscotch). A 2.0 document
// keeps one flow beside the scheme.
func (r *reader) oauth2(declared map[string]any, scopes []string) *domain.Auth {
	if flows, ok := declared["flows"].(map[string]any); ok {
		if code, ok := flows["authorizationCode"].(map[string]any); ok {
			authUrl, _ := code["authorizationUrl"].(string)
			tokenUrl, _ := code["tokenUrl"].(string)
			return r.oauth2Auth(domain.GrantAuthorizationCode, authUrl, tokenUrl, scopes)
		}
		if implicit, ok := flows["implicit"].(map[string]any); ok {
			authUrl, _ := implicit["authorizationUrl"].(string)
			return r.oauth2Auth(domain.GrantImplicit, authUrl, "", scopes)
		}
		if password, ok := flows["password"].(map[string]any); ok {
			tokenUrl, _ := password["tokenUrl"].(string)
			return r.oauth2Auth(domain.GrantPassword, "", tokenUrl, scopes)
		}
		if credentials, ok := flows["clientCredentials"].(map[string]any); ok {
			tokenUrl, _ := credentials["tokenUrl"].(string)
			return r.oauth2Auth(domain.GrantClientCredentials, "", tokenUrl, scopes)
		}
		return r.oauth2Auth(domain.GrantAuthorizationCode, "", "", scopes)
	}

	authUrl, _ := declared["authorizationUrl"].(string)
	tokenUrl, _ := declared["tokenUrl"].(string)
	switch flow, _ := declared["flow"].(string); flow {
	case domain.GrantImplicit:
		return r.oauth2Auth(domain.GrantImplicit, authUrl, "", scopes)
	case domain.GrantPassword:
		return r.oauth2Auth(domain.GrantPassword, "", tokenUrl, scopes)
	case "application":
		return r.oauth2Auth(domain.GrantClientCredentials, "", tokenUrl, scopes)
	default:
		return r.oauth2Auth(domain.GrantAuthorizationCode, authUrl, tokenUrl, scopes)
	}
}

// oauth2Auth is one OAuth 2.0 scheme as this app holds it: the grant, its addresses and the
// requested scopes. The client pair points at variables, like the bearer token, so a person fills
// them in once.
func (r *reader) oauth2Auth(grant, authUrl, tokenUrl string, scopes []string) *domain.Auth {
	r.declare("clientId", "", true)
	r.declare("clientSecret", "", true)
	fields := map[string]string{
		"grant":        grant,
		"clientId":     "{{clientId}}",
		"clientSecret": "{{clientSecret}}",
		"scope":        strings.Join(scopes, " "),
	}
	if authUrl != "" {
		fields["authUrl"] = authUrl
	}
	if tokenUrl != "" {
		fields["tokenUrl"] = tokenUrl
	}
	return &domain.Auth{Type: domain.AuthOAuth2, Fields: fields}
}
