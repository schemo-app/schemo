package channel

import (
	"net/netip"
	"net/url"
	"strings"

	"schemo/internal/domain"
)

// URL accepts verified HTTPS or explicit HTTP loopback connections.
func URL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname())) {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	return u, nil
}

// Endpoint prevents remote providers and redirects from downgrading to local HTTP.
func Endpoint(origin, destination string) error {
	from, err := URL(origin)
	if err != nil {
		return err
	}
	to, err := URL(destination)
	if err != nil {
		return err
	}
	if to.Scheme == "http" && (from.Scheme == "https" || !loopback(from.Hostname())) {
		return domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	return nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}
