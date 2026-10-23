package request

import (
	"net/url"
	"strings"

	"schemo/internal/domain"
)

func complete(raw string) string {
	address := strings.TrimSpace(raw)
	switch {
	case address == "", strings.Contains(address, "{{"), hasScheme(address):
		return address
	case strings.HasPrefix(address, "//"):
		// Protocol-relative: the host is named and the scheme is left out on purpose.
		return "https:" + address
	case strings.HasPrefix(address, "/"):
		// A bare path is not an address at all, whatever is put in front of it.
		return address
	case namesThisMachine(authorityOf(address)):
		// A development server is the one host where https is the wrong guess, and it is what a
		// browser falls back to for these names too.
		return "http://" + address
	default:
		return "https://" + address
	}
}

// hasScheme reads the start of an address as the scheme of RFC 3986 §3.1, except that `host:8080`
// is a host and a port rather than a scheme named host.
func hasScheme(address string) bool {
	colon := strings.IndexByte(address, ':')
	if colon < 1 {
		return false
	}
	for i, r := range address[:colon] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}

	port := address[colon+1:]
	if end := strings.IndexAny(port, "/?#"); end >= 0 {
		port = port[:end]
	}
	if port == "" {
		return true
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return true
		}
	}
	return false
}

// authorityOf is what stands before the first `/`, `?` or `#`.
func authorityOf(address string) string {
	if end := strings.IndexAny(address, "/?#"); end >= 0 {
		return address[:end]
	}
	return address
}

// namesThisMachine says whether the host of an authority, with its user and port taken off, is a
// local one.
func namesThisMachine(authority string) bool {
	parsed, err := url.Parse("//" + authority)
	return err == nil && domain.LocalName(parsed.Hostname())
}
