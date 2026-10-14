package authflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/smithy-go/aws-http-auth/credentials"
	"github.com/aws/smithy-go/aws-http-auth/sigv4"
	"github.com/golang-jwt/jwt/v5"

	"schemo/internal/domain"
)

// jwtBearer mints a token here rather than being given one; what goes on the request is a bearer
// token like any other, which is why it is carried by the same code. There is no absorb:
// a signature over claims is not a value a person can edit. The window shows the row rather than
// offering to.
func jwtBearer(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	secret := auth.Answer("secret")
	if secret == "" {
		return domain.AuthOutput{}, nil
	}

	key, method, err := signingKey(auth)
	if err != nil {
		return domain.AuthOutput{}, err
	}

	claims := jwt.MapClaims{}
	if payload := strings.TrimSpace(auth.Answer("payload")); payload != "" {
		if err := json.Unmarshal([]byte(payload), &claims); err != nil {
			return domain.AuthOutput{}, fmt.Errorf("reading the claims of a JWT: %w", err)
		}
	}
	// The two claims a token needs to be accepted and to expire, added only where the user left them
	// out: a call that wants its own `exp`, or none at all, has said so by writing one.
	minted := time.Now()
	if _, given := claims["iat"]; !given {
		claims["iat"] = minted.Unix()
	}
	if _, given := claims["exp"]; !given {
		if expires := expiresIn(auth.OrDefault("expiresIn")); expires > 0 {
			claims["exp"] = minted.Add(expires).Unix()
		}
	}

	token := jwt.NewWithClaims(method, claims)
	if err := setJOSEHeader(token, auth.Answer("joseHeader")); err != nil {
		return domain.AuthOutput{}, err
	}
	signed, err := token.SignedString(key)
	if err != nil {
		return domain.AuthOutput{}, fmt.Errorf("signing a JWT: %w", err)
	}
	return carry(auth, signed), nil
}

// signingKey is the material a token of this algorithm is signed with, and the algorithm itself.
// The two families take different things — HMAC takes the secret's bytes and RSA takes a parsed key
// — which is the whole of why this is a switch and not a table.
func signingKey(auth domain.Auth) (any, jwt.SigningMethod, error) {
	algorithm := auth.OrDefault("algorithm")
	if !offersOption(auth.Type, "algorithm", algorithm) {
		return nil, nil, fmt.Errorf("signing a JWT with %q: %w", algorithm, domain.ErrNotAllowed)
	}
	method := jwt.GetSigningMethod(algorithm)

	secret := []byte(auth.Answer("secret"))
	if auth.OrDefault("secretEncoding") == encodingBase64 {
		decoded, err := base64.StdEncoding.DecodeString(string(secret))
		if err != nil {
			return nil, nil, fmt.Errorf("reading a base64 secret: %w", err)
		}
		secret = decoded
	}

	switch method.(type) {
	case *jwt.SigningMethodHMAC:
		return secret, method, nil
	case *jwt.SigningMethodRSA:
		key, err := jwt.ParseRSAPrivateKeyFromPEM(secret)
		if err != nil {
			return nil, nil, fmt.Errorf("reading an RSA secret: %w", err)
		}
		return key, method, nil
	}
	return nil, nil, fmt.Errorf("signing a JWT with %q: %w", algorithm, domain.ErrNotAllowed)
}

// joseHeaderKeys is what the design lets a JOSE header carry. `alg` is not here on purpose: it
// comes from the algorithm, and a header that said otherwise would describe a token nobody signed.
var joseHeaderKeys = []string{"kid", "typ", "cty", "x5t", "x5u", "jku"}

func setJOSEHeader(token *jwt.Token, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var written map[string]any
	if err := json.Unmarshal([]byte(raw), &written); err != nil {
		return fmt.Errorf("reading a JOSE header: %w", err)
	}
	for _, key := range joseHeaderKeys {
		if value, ok := written[key]; ok {
			token.Header[key] = value
		}
	}
	return nil
}

// expiresIn is how long a token is good for. A number that is not one, and the zero the design
// gives for "a token with no expiry", both come to nothing.
func expiresIn(raw string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// awsSignature signs the request itself rather than putting a credential on it: the method, the
// address, the headers and the body all go into the signature, and the signer writes what it made
// back onto the request. No absorb — the signature is over the request, so an edited one would be a
// signature for something nobody sent.
func awsSignature(auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput, error) {
	accessKey := strings.TrimSpace(auth.Answer("accessKeyId"))
	secretKey := auth.Answer("secretAccessKey")
	service := strings.TrimSpace(auth.OrDefault("service"))
	region := strings.TrimSpace(auth.OrDefault("region"))
	// A signature over a service nobody named is a request the server refuses, and one the window
	// would have drawn as if it worked. Nothing is the honest answer to a half-filled scheme.
	if accessKey == "" || secretKey == "" || service == "" || region == "" {
		return domain.AuthOutput{}, nil
	}

	signable, err := http.NewRequest(req.Method, req.URL, seekable{bytes.NewReader(req.Body)})
	if err != nil {
		return domain.AuthOutput{}, fmt.Errorf("building the request to sign: %w", err)
	}
	for _, pair := range req.Headers {
		signable.Header.Add(pair.Name, pair.Value)
	}

	// What the request already carried is not the signature's to report: only the headers the signer
	// added are what this scheme puts on the wire.
	before := map[string]bool{}
	for name := range signable.Header {
		before[name] = true
	}

	err = sigv4.New().SignRequest(&sigv4.SignRequestInput{
		Request: signable,
		Credentials: credentials.Credentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
			SessionToken:    auth.Answer("sessionToken"),
		},
		Service: service,
		Region:  region,
	})
	if err != nil {
		return domain.AuthOutput{}, fmt.Errorf("signing a request for AWS: %w", err)
	}

	out := domain.AuthOutput{}
	for name, values := range signable.Header {
		if before[name] {
			continue
		}
		for _, value := range values {
			out.Headers = append(out.Headers, domain.HeaderPair{Name: name, Value: value})
		}
	}
	return out, nil
}

// seekable is the bytes of a request in something the signer can read, rewind and close. It hashes
// the body to sign it and goes back over it afterwards, which it can only do for a body it can
// seek. `http.NewRequest` wraps a non-ReadCloser in a plain reader, and an unsigned body is one
// many services refuse; bytes.Reader seeks but does not close.
type seekable struct{ *bytes.Reader }

func (seekable) Close() error { return nil }

// queryTokenName is what a token carried in the query string is called. It is not the scheme's to
// choose: OAuth 2 names it, and a server reading it there looks for this word.
const queryTokenName = "access_token"

// carry puts a token where the scheme was told to carry it. It is the same credential either way,
// and some servers read it only one of the two.
func carry(auth domain.Auth, token string) domain.AuthOutput {
	if auth.OrDefault("place") == domain.PlaceQuery {
		return domain.AuthOutput{Query: []domain.HeaderPair{{Name: queryTokenName, Value: token}}}
	}
	return header("Authorization", withPrefix(auth.Answer("prefix"), token))
}

// offersOption is whether a scheme lets a field take this value at all. The table is the allowlist,
// so a value that got here is one the window could have offered — and one that is not, like an
// algorithm nobody implements, is refused rather than signed with.
func offersOption(kind domain.AuthType, key, value string) bool {
	scheme, ok := domain.SchemeFor(kind)
	if !ok {
		return false
	}
	for _, field := range scheme.Fields {
		if field.Key != key {
			continue
		}
		if len(field.Options) == 0 {
			return true
		}
		for _, option := range field.Options {
			if option.Value == value {
				return true
			}
		}
		return false
	}
	return false
}
