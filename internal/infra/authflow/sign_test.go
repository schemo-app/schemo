package authflow

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"schemo/internal/domain"
)

// What is tested here is the wiring and not the cryptography: a signature computed correctly is
// still a broken request if the wrong secret, service or place on the wire went into it. Those are
// the things that can be wrong on this side.

func bearerToken(t *testing.T, out domain.AuthOutput) string {
	t.Helper()
	value, ok := headerOf(out, "Authorization")
	if !ok {
		t.Fatalf("headers = %+v, want an authorization", out.Headers)
	}
	token, found := strings.CutPrefix(value, "Bearer ")
	if !found {
		t.Fatalf("authorization = %q, want the prefix the scheme names", value)
	}
	return token
}

func TestJWTSignsWhatItWasAskedTo(t *testing.T) {
	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("algorithm", "HS256").
		With("secret", "s3cret").
		With("payload", `{"sub":"1234567890","custom":true}`).
		With("expiresIn", "60")

	token := bearerToken(t, materialize(t, auth))

	// Parsed back with the key rather than merely decoded: a token whose signature does not check out
	// is one the server refuses, and a test that only read the claims would not notice.
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte("s3cret"), nil })
	if err != nil {
		t.Fatalf("the token does not verify: %v", err)
	}
	if parsed.Method.Alg() != "HS256" {
		t.Errorf("alg = %q, want the algorithm the user chose", parsed.Method.Alg())
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims = %T, want a claims map", parsed.Claims)
	}
	if claims["sub"] != "1234567890" || claims["custom"] != true {
		t.Errorf("claims = %+v, want the payload the user wrote", claims)
	}
	// iat and exp are added at send time, and both are claims rather than headers: a server reads
	// them from the payload.
	issued, hasIat := claims["iat"].(float64)
	expires, hasExp := claims["exp"].(float64)
	if !hasIat || !hasExp {
		t.Fatalf("claims = %+v, want iat and exp added", claims)
	}
	if got := time.Duration(expires-issued) * time.Second; got != time.Minute {
		t.Errorf("the token is good for %s, want the minute that was asked for", got)
	}
}

// The claims the user wrote win: a call that wants its own `exp`, or none at all, has said so.
func TestJWTKeepsTheClaimsItWasGiven(t *testing.T) {
	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secret", "s3cret").
		With("payload", `{"exp":1,"iat":2}`)

	token := bearerToken(t, materialize(t, auth))
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte("s3cret"), nil },
		jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("the token does not verify: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["exp"] != float64(1) || claims["iat"] != float64(2) {
		t.Errorf("claims = %+v, want the two the user wrote", claims)
	}

	// A zero means a token with no expiry, which the design spells out under the field.
	forever := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secret", "s3cret").With("expiresIn", "0")
	parsed, err = jwt.Parse(bearerToken(t, materialize(t, forever)),
		func(*jwt.Token) (any, error) { return []byte("s3cret"), nil }, jwt.WithoutClaimsValidation())
	if err != nil {
		t.Fatalf("the token does not verify: %v", err)
	}
	if _, hasExp := parsed.Claims.(jwt.MapClaims)["exp"]; hasExp {
		t.Error("a token asked to have no expiry was given one")
	}
}

func TestJWTReadsABase64Secret(t *testing.T) {
	raw := "not-the-secret"
	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secretEncoding", "base64").
		With("secret", base64.StdEncoding.EncodeToString([]byte(raw)))

	token := bearerToken(t, materialize(t, auth))
	if _, err := jwt.Parse(token,
		func(*jwt.Token) (any, error) { return []byte(raw), nil }); err != nil {
		t.Errorf("the token does not verify against the decoded secret: %v", err)
	}
}

// The JOSE header carries the keys the design names and no others: `alg` comes from the algorithm.
func TestJWTHeaderIsRestricted(t *testing.T) {
	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secret", "s3cret").
		With("algorithm", "HS512").
		With("joseHeader", `{"kid":"key-1","alg":"none","nonsense":"x"}`)

	parsed, err := jwt.Parse(bearerToken(t, materialize(t, auth)),
		func(*jwt.Token) (any, error) { return []byte("s3cret"), nil })
	if err != nil {
		t.Fatalf("the token does not verify: %v", err)
	}
	if parsed.Header["kid"] != "key-1" {
		t.Errorf("header = %+v, want the kid the user wrote", parsed.Header)
	}
	if parsed.Method.Alg() != "HS512" {
		t.Errorf("alg = %q, want the algorithm the user chose", parsed.Method.Alg())
	}
	if _, ok := parsed.Header["nonsense"]; ok {
		t.Errorf("header = %+v, want only the keys the design names", parsed.Header)
	}
}

// Nothing filled in is nothing on the wire, and that goes for the algorithm too: the table is the
// allowlist, so a method that is not one of its choices is refused rather than signed with.
func TestJWTSaysNothingWhenItCannotSign(t *testing.T) {
	if out := materialize(t, domain.NewAuth(domain.AuthJWT).Normalized()); !out.Empty() {
		t.Errorf("output = %+v, want nothing without a secret", out)
	}

	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secret", "s3cret").With("algorithm", "none")
	if _, err := New(nil, nil).Project(auth, domain.AuthRequest{}); err == nil {
		t.Error("an algorithm the scheme does not offer was signed with anyway")
	}
}

// A signed request carries more than one header, and what it carries has to name the service and
// the region the user chose — a signature over the wrong ones is a request the server refuses.
func TestAWSSignsTheRequestItWasGiven(t *testing.T) {
	auth := domain.NewAuth(domain.AuthAWS).Normalized().
		With("accessKeyId", "AKIAEXAMPLE").
		With("secretAccessKey", "secret").
		With("region", "us-east-1").
		With("service", "execute-api")

	req := domain.AuthRequest{
		Method:  "GET",
		URL:     "https://example.execute-api.us-east-1.amazonaws.com/prod/thing",
		Headers: []domain.HeaderPair{{Name: "Accept", Value: "application/json"}},
	}
	out, err := New(nil, nil).Project(auth, req)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}

	signature, ok := headerOf(out, "Authorization")
	if !ok {
		t.Fatalf("headers = %+v, want an authorization", out.Headers)
	}
	for _, want := range []string{
		"AWS4-HMAC-SHA256",
		"Credential=AKIAEXAMPLE/",
		"/us-east-1/execute-api/aws4_request",
		"SignedHeaders=",
		"Signature=",
	} {
		if !strings.Contains(signature, want) {
			t.Errorf("authorization = %q, want it to name %q", signature, want)
		}
	}
	// The date is signed, so it has to be on the request as well.
	if _, ok := headerOf(out, "X-Amz-Date"); !ok {
		t.Errorf("headers = %+v, want the date that was signed", out.Headers)
	}
	// The row the user wrote is not the signature's to report: it travels on its own.
	if _, reported := headerOf(out, "Accept"); reported {
		t.Errorf("headers = %+v, want only what the signing added", out.Headers)
	}
}

// A session token is a third credential and travels as its own header; without one there is no such
// header, because an empty one is a request the server refuses.
func TestAWSSessionTokenIsOptional(t *testing.T) {
	auth := domain.NewAuth(domain.AuthAWS).Normalized().
		With("accessKeyId", "AKIAEXAMPLE").
		With("secretAccessKey", "secret").
		With("region", "eu-west-1").
		With("service", "s3")

	out, err := New(nil, nil).Project(auth,
		domain.AuthRequest{Method: "GET", URL: "https://example.com/a"})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if _, ok := headerOf(out, "X-Amz-Security-Token"); ok {
		t.Errorf("headers = %+v, want no session token header without one", out.Headers)
	}

	withSession, err := New(nil, nil).Project(auth.With("sessionToken", "sts"),
		domain.AuthRequest{Method: "GET", URL: "https://example.com/a"})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if value, ok := headerOf(withSession, "X-Amz-Security-Token"); !ok || value != "sts" {
		t.Errorf("headers = %+v, want the session token the user gave", withSession.Headers)
	}
}

// The body is part of what is signed: two requests that differ only in it are two different
// signatures, which is what signing the request rather than the credential means.
func TestAWSSignsTheBody(t *testing.T) {
	auth := domain.NewAuth(domain.AuthAWS).Normalized().
		With("accessKeyId", "AKIAEXAMPLE").
		With("secretAccessKey", "secret").
		With("region", "us-east-1").
		With("service", "execute-api")

	sign := func(body string) string {
		out, err := New(nil, nil).Project(auth,
			domain.AuthRequest{Method: "POST", URL: "https://example.com/a", Body: []byte(body)})
		if err != nil {
			t.Fatalf("Project: %v", err)
		}
		value, _ := headerOf(out, "Authorization")
		_, after, _ := strings.Cut(value, "Signature=")
		return after
	}
	if sign("") == sign(`{"a":1}`) {
		t.Error("two different bodies were signed the same way")
	}
}

// A signature over a service nobody named is a request the server refuses.
func TestAWSSaysNothingWhenItIsHalfFilled(t *testing.T) {
	half := domain.NewAuth(domain.AuthAWS).Normalized().
		With("accessKeyId", "AKIAEXAMPLE").
		With("secretAccessKey", "secret")

	if out := materialize(t, half); !out.Empty() {
		t.Errorf("output = %+v, want nothing without a region and a service", out)
	}
}

// The two schemes that mint rather than carry are carried the same way everything else is, which is
// what makes «Add token to» one field and not three.
func TestAMintedTokenCanTravelInTheQuery(t *testing.T) {
	auth := domain.NewAuth(domain.AuthJWT).Normalized().
		With("secret", "s3cret").
		With("place", domain.PlaceQuery)

	out, err := New(nil, nil).Project(auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(out.Headers) != 0 {
		t.Errorf("headers = %+v, want none for a token that travels in the query", out.Headers)
	}
	if len(out.Query) != 1 || out.Query[0].Name != queryTokenName || out.Query[0].Value == "" {
		t.Errorf("query = %+v, want the minted token under the name OAuth 2 gives it", out.Query)
	}
}
