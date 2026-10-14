package draft

import (
	"context"
	"mime"
	"strings"

	"schemo/internal/domain"
)

func partsOf(draft domain.Draft, inherits *domain.Auth, cookie string) parts {
	raw := collect(draft, cookie)
	raw.auth = authToApply(draft.Auth, inherits)
	return raw
}

func (u *UseCase) prepare(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
	cookie string,
) (Prepared, error) {
	raw := partsOf(draft, inherits, cookie)
	texts := raw.texts()

	live, err := u.vars.SubstituteTexts(ctx, above, texts, false, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}
	hidden, err := u.vars.SubstituteTexts(ctx, above, texts, true, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}

	return u.assembled(ctx, draft, raw.putBack(live), raw.putBack(hidden))
}

// maskOf renders a request that has already been filled in — a script handed it back, and the copy
// written down is made from it: nothing is resolved, because its braces are its own text by then,
// and reading them again would eat a value that happens to carry one. Only the secrets a value
// carries are hidden; the sent side is the text as it came.
func (u *UseCase) maskOf(ctx context.Context, draft domain.Draft) (Prepared, error) {
	raw := partsOf(draft, nil, "")
	texts := raw.texts()

	hidden, err := u.vars.MaskTexts(ctx, nil, texts, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}

	return u.assembled(ctx, draft, raw.putBack(texts), raw.putBack(hidden))
}

// assembled is the request built from its two renderings: what goes out, and the copy that is
// written down.
func (u *UseCase) assembled(
	ctx context.Context,
	draft domain.Draft,
	sent, stored parts,
) (Prepared, error) {
	kind := domain.KindOf(draft.BodyKind)

	// The body is rendered twice from one encoding. The boundary is minted by the first rendering and
	// handed to the second, because a boundary that differed between the two would make the copy that
	// is written down a description of a request that was never sent.
	body, boundary, err := u.renderBody(kind, sent.body, draft.BodyFile, sent.form, "", false)
	if err != nil {
		return Prepared{}, err
	}
	masked, _, err := u.renderBody(kind, stored.body, draft.BodyFile, stored.form, boundary, true)
	if err != nil {
		return Prepared{}, err
	}

	// The authorization is worked out from each rendering of the fields, so the copy that is written
	// down carries masked credentials rather than the ones that went out. A scheme that has to hold a
	// conversation to answer — a token to fetch, a challenge to meet — has to recognise the second
	// call as the same one and not hold it twice: same scheme, same answers, same request.
	sentAuth, err := u.auth.Materialize(ctx, sent.auth,
		authRequest(draft.Method, sent.url, sent.headers, body))
	if err != nil {
		return Prepared{}, err
	}
	storedAuth, err := u.auth.Materialize(ctx, stored.auth,
		authRequest(draft.Method, stored.url, stored.headers, masked))
	if err != nil {
		return Prepared{}, err
	}

	// A credential that cannot be put on the request yet goes to the engine instead of onto the
	// request — unless the user wrote an Authorization row themselves, which is the more precise
	// answer and not one a challenge should be answered over.
	digest := sentAuth.Digest
	if digest != nil && hasHeader(sent.headers, "Authorization") {
		digest = nil
	}

	sentURL, sentHeaders := withAuth(sent.url, sent.headers, sentAuth)
	storedURL, storedHeaders := withAuth(stored.url, stored.headers, storedAuth)

	return Prepared{
		Method: draft.Method,
		URL:    sentURL,
		Headers: withContentType(withCookie(sentHeaders, sent.cookie), kind, draft.BodyFile,
			boundary),
		Body:      body,
		BodyKind:  kind,
		Form:      sent.form,
		BodyFile:  draft.BodyFile,
		MaskedURL: storedURL,
		MaskedHeaders: withContentType(withCookie(storedHeaders, stored.cookie), kind, draft.BodyFile,
			boundary),
		MaskedBody: masked,
		Digest:     digest,
	}, nil
}

func authRequest(
	method, rawURL string,
	headers []domain.HeaderPair,
	body string,
) domain.AuthRequest {
	return domain.AuthRequest{Method: method, URL: rawURL, Headers: headers, Body: []byte(body)}
}

// authToApply is the authorization this request goes out with: the one the draft chose, or — when
// it chose «Inherit» — the one the levels above it answered with, which the caller resolved.
// Nothing above a request is «None»: a request that inherits from nothing sends no credentials.
func authToApply(chosen domain.Auth, inherits *domain.Auth) domain.Auth {
	if chosen.Type != domain.AuthInherit {
		return chosen
	}
	if inherits == nil {
		return domain.NewAuth(domain.AuthNone)
	}
	return *inherits
}

// A row of the same name the user wrote wins: an explicit row is the more precise answer, and two
// Authorization headers are a request no server reads the way the window drew it. The query side
// works on the URL being sent — a projection into the draft's rows dies on the next keystroke.
func withAuth(
	rawURL string,
	headers []domain.HeaderPair,
	out domain.AuthOutput,
) (string, []domain.HeaderPair) {
	for _, pair := range out.Headers {
		if hasHeader(headers, pair.Name) {
			continue
		}
		headers = append(headers, pair)
	}
	for _, pair := range out.Query {
		if hasParam(rawURL, pair.Name) {
			continue
		}
		rawURL = appendParam(rawURL, pair)
	}
	return rawURL, headers
}

func hasHeader(headers []domain.HeaderPair, name string) bool {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return true
		}
	}
	return false
}

func hasParam(rawURL, name string) bool {
	for _, row := range paramsFromURL(rawURL) {
		if row.Name == name {
			return true
		}
	}
	return false
}

// appendParam puts one more parameter on an address, before the fragment if it has one: a fragment
// is not a parameter, and a query string after it is a query string to nothing.
func appendParam(rawURL string, pair domain.HeaderPair) string {
	fragment := ""
	if hash := strings.IndexByte(rawURL, '#'); hash >= 0 {
		rawURL, fragment = rawURL[:hash], rawURL[hash:]
	}
	separator := "?"
	if strings.ContainsRune(rawURL, '?') {
		separator = "&"
	}
	return rawURL + separator + encodeQuery(pair.Name) + "=" + encodeQuery(pair.Value) + fragment
}

// withCookie is where the jar becomes the header that carries it.
func withCookie(headers []domain.HeaderPair, cookie string) []domain.HeaderPair {
	if cookie == "" {
		return headers
	}
	return append(headers, domain.HeaderPair{Name: "Cookie", Value: cookie})
}

// withContentType is where the format chosen in the Body popover becomes the header that declares
// it. A row of the same name the user wrote themselves wins, for the reason it wins over the Auth
// chip and one more: a written Content-Type is the only way to send a type the kind cannot name,
// and this app's own Accept is a vendor one, so that is the expected case rather than the exotic
// one.
func withContentType(
	headers []domain.HeaderPair,
	kind domain.BodyKind,
	file string,
	boundary string,
) []domain.HeaderPair {
	value, ok := contentTypeFor(kind, file, boundary)
	if !ok {
		return headers
	}
	if hasHeader(headers, "Content-Type") {
		return headers
	}
	return append(headers, domain.HeaderPair{Name: "Content-Type", Value: value})
}

// Raw answers with nothing on purpose: a raw body is text this app does not interpret, so it
// declares nothing about itself and the server reads it as it will. Naming a type for it would be
// the window guessing at what the user meant. A written header wins, so a text/plain typed once
// sticks.
func contentTypeFor(kind domain.BodyKind, file string, boundary string) (string, bool) {
	switch kind {
	case domain.BodyJSON:
		return domain.MediaJSON, true
	case domain.BodyXML:
		return domain.MediaXML, true
	case domain.BodyForm:
		// The boundary is the one the body was actually written with, which is why it is handed in
		// rather than minted here: two boundaries would be a header describing nothing.
		return mime.FormatMediaType(domain.MediaMultipart, map[string]string{"boundary": boundary}), true
	case domain.BodyBinary:
		return typeOfFile(file), true
	default:
		return "", false
	}
}
