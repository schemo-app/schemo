package httpx

import (
	"net"
	"strings"
)

func complete(raw string) string {
	url := strings.TrimSpace(raw)
	switch {
	case url == "", strings.Contains(url, "{{"), hasScheme(url):
		return url
	case strings.HasPrefix(url, "//"):
		// Protocol-relative: the host is named and the scheme is left out on purpose.
		return "https:" + url
	case strings.HasPrefix(url, "/"):
		// A bare path is not an address at all, whatever is put in front of it.
		return url
	case isLocal(authority(url)):
		// A development server is the one host where https is the wrong guess, and it is what a
		// browser falls back to for these names too.
		return "http://" + url
	default:
		return "https://" + url
	}
}

func hasScheme(url string) bool {
	colon := strings.IndexByte(url, ':')
	if colon < 1 {
		return false
	}
	for i, r := range url[:colon] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}

	port := url[colon+1:]
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

func authority(url string) string {
	if end := strings.IndexAny(url, "/?#"); end >= 0 {
		return url[:end]
	}
	return url
}

func isLocal(authority string) bool {
	host := authority
	if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.HasSuffix(host, "]") {
		host = host[:i]
	}
	return isLocalHost(host)
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasPrefix(host, "127.")
}
