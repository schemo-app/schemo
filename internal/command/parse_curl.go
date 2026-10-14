package command

import (
	"encoding/base64"
	"net/url"
	"strings"

	"schemo/internal/domain"
)

// curlArgs is what the flags of a command said, before any of it is put together into a request.
type curlArgs struct {
	method       optString
	explicitURL  optString
	user         optString
	bearer       optString
	nonBasicAuth bool
	digest       bool
	get          bool
	entries      []headerEntry
	data         []string
	forms        []headerEntry
	positionals  []string
}

func fromCurl(tokens []string) (pendingRequest, Reason) {
	args, reason := readCurlFlags(tokens)
	if reason != "" {
		return pendingRequest{}, reason
	}
	return args.request()
}

// readCurlFlags walks the tokens once, taking each flag this app has an answer for and stepping
// over the ones it has none for, so that an operand is never taken for a flag's argument.
func readCurlFlags(tokens []string) (curlArgs, Reason) {
	args := curlArgs{entries: []headerEntry{}}

	for cursor := 0; cursor < len(tokens); cursor++ {
		raw := tokens[cursor]
		if raw == "--" {
			// Everything after `--` is an operand, even if it starts with a dash.
			args.positionals = append(args.positionals, tokens[cursor+1:]...)
			break
		}

		flag, inline := splitFlag(raw)
		if inline == nil && !strings.HasPrefix(raw, "-") {
			args.positionals = append(args.positionals, raw)
			continue
		}

		switch flag {
		case "-X", "--request":
			if v := take(tokens, &cursor, inline); v.set {
				args.method = optString{value: strings.ToUpper(v.value), set: true}
			}
		case "-H", "--header":
			// An empty argument is not a header; anything else is one even without a colon.
			if v := take(tokens, &cursor, inline); v.set && v.value != "" {
				args.entries = append(args.entries, parseHeaderArg(v.value))
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, v.value)
			}
		case "--data-urlencode":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, urlencodeData(v.value))
			}
		case "--json":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, v.value)
				args.entries = append(args.entries,
					headerEntry{name: "Content-Type", value: domain.MediaJSON})
			}
		case "--url":
			if v := take(tokens, &cursor, inline); v.set {
				args.explicitURL = v
			}
		case "-I", "--head":
			args.method = optString{value: "HEAD", set: true}
		case "-T", "--upload-file":
			// curl uploads a file with PUT. The path itself goes: this app sends a body, and a file
			// on the pasting machine's disk is what a command line cannot carry over.
			if v := take(tokens, &cursor, inline); v.set && !args.method.set {
				args.method = optString{value: "PUT", set: true}
			}
		case "-G", "--get":
			args.get = true
		case "-u", "--user":
			if v := take(tokens, &cursor, inline); v.set {
				args.user = v
			}
		case "-A", "--user-agent":
			if v := take(tokens, &cursor, inline); v.set {
				args.entries = append(args.entries, headerEntry{name: "User-Agent", value: v.value})
			}
		case "-b", "--cookie":
			// A value without `=` names a cookie *file*, not a cookie.
			if v := take(tokens, &cursor, inline); v.set && v.value != "" && strings.Contains(v.value, "=") {
				args.entries = append(args.entries, headerEntry{name: "Cookie", value: v.value})
			}
		case "-F", "--form", "--form-string":
			if v := take(tokens, &cursor, inline); v.set {
				name, value, _ := strings.Cut(v.value, "=")
				// `-F 'file=@path'` is an upload this app has no way to reproduce.
				if strings.HasPrefix(value, "@") || strings.HasPrefix(value, "<") {
					return curlArgs{}, ReasonUnsupportedMultipart
				}
				args.forms = append(args.forms, headerEntry{name: name, value: value})
			}
		case "--digest":
			args.digest = true
		case "--oauth2-bearer":
			if v := take(tokens, &cursor, inline); v.set {
				args.bearer = v
			}
		case "--ntlm", "--negotiate", "--anyauth", "--proxy-anyauth":
			// Schemes this app has no answer for. Inventing a Basic credential for them would be
			// sending one the command never asked for, so `-u` is left alone.
			args.nonBasicAuth = true
		default:
			if curlFlags[flag] {
				take(tokens, &cursor, inline)
			}
		}
	}
	return args, ""
}

// request puts what the flags said together: where it goes, what it carries, who it says it is and
// how it is sent.
func (a curlArgs) request() (pendingRequest, Reason) {
	url := pickURL(a.positionals)
	if a.explicitURL.set {
		url = a.explicitURL.value
	}
	if url == "" {
		return pendingRequest{}, ReasonNoURL
	}
	if hasLeftover(a.positionals, url) {
		return pendingRequest{}, ReasonLeftover
	}

	entries, auth := writtenAuth(a.entries, a.auth())

	body := strings.Join(a.data, "&")
	formRows := []domain.FormRow{}
	if body == "" && len(a.forms) > 0 {
		// A plain `-F name=value` is a multipart field, not a form-encoded body: the draft composes
		// the multipart body from the rows, so the request goes out as the command meant it.
		for _, f := range a.forms {
			formRows = append(formRows, domain.FormRow{Name: f.name, Value: f.value, Enabled: true})
		}
	}
	if a.get {
		// `-G` moves the data into the query string instead of sending it, and with it goes any
		// body the request would have carried.
		url = appendQuery(url, body)
		body = ""
	}

	kind, contentType := bodyShape(body, formRows)
	result := pendingRequest{
		url:     url,
		entries: withContentType(entries, contentType),
		body:    body,
		form:    formRows,
		kind:    kind,
		auth:    auth,
	}
	switch {
	case a.method.set:
		result.method = a.method.value
	case (body != "" || len(formRows) > 0) && !a.get:
		result.method = "POST"
	default:
		result.method = "GET"
	}
	return result, ""
}

// auth is the scheme the flags name, or none. A bearer token wins over a login the command also
// carries — curl takes both, and the server reads the bearer one; a written header wins over both,
// which `writtenAuth` sees to.
func (a curlArgs) auth() *domain.Auth {
	switch {
	case a.bearer.set:
		return bearerScheme(a.bearer.value)
	case a.user.set && !a.nonBasicAuth:
		login, password := splitCredential(a.user.value)
		if a.digest {
			return digestScheme(login, password)
		}
		return basicScheme(login, password)
	}
	return nil
}

// urlencodeData is one `--data-urlencode` argument as the part of the body it stands for. The name
// keeps its spelling and what follows the first `=` is percent-encoded — a space as `%20`, not the
// `+` a form would use. The `@file` forms read a file this app has nowhere to read from, so they
// travel as they were written: the same honesty the multipart refusal keeps out loud.
func urlencodeData(value string) string {
	name, content, found := strings.Cut(value, "=")
	if found && strings.Contains(name, "@") {
		return value
	}
	if !found {
		if strings.HasPrefix(value, "@") {
			return value
		}
		return escaped(value)
	}
	if name == "" {
		// `=content` is the content alone, encoded, with no name in front of it.
		return escaped(content)
	}
	return name + "=" + escaped(content)
}

// escaped is curl's own percent-encoding: the unreserved characters keep their spelling and a
// space is `%20` — QueryEscape's set with its `+` turned back into one.
func escaped(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// The row goes with the credential: left beside the scheme it would win over it, since a written
// header is what goes out, and editing the chip would then do nothing. A scheme that cannot be
// rebuilt from a header — Digest, Negotiate, NTLM — stays the header it is: Digest holds no
// password to put in a field, and Negotiate and NTLM have no fields at all.
func writtenAuth(entries []headerEntry, auth *domain.Auth) ([]headerEntry, *domain.Auth) {
	for i, e := range entries {
		if !strings.EqualFold(e.name, "Authorization") {
			continue
		}
		scheme, ok := authFromHeader(e.value)
		if !ok {
			return entries, auth
		}
		rest := make([]headerEntry, 0, len(entries)-1)
		rest = append(rest, entries[:i]...)
		return append(rest, entries[i+1:]...), scheme
	}
	return entries, auth
}

func authFromHeader(value string) (*domain.Auth, bool) {
	name, rest, found := strings.Cut(strings.TrimSpace(value), " ")
	if !found {
		return nil, false
	}
	switch {
	case strings.EqualFold(name, "basic"):
		raw, err := decodeBase64(strings.TrimSpace(rest))
		if err != nil {
			return nil, false
		}
		login, password, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, false
		}
		return basicScheme(login, password), true
	case strings.EqualFold(name, "bearer"):
		if token := strings.TrimSpace(rest); token != "" {
			return bearerScheme(token), true
		}
	}
	return nil, false
}

// A Basic credential's padding is optional in practice — both spellings are written by clients in
// the wild — so both are tried.
func decodeBase64(s string) (string, error) {
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(raw), nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
