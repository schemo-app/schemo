package httpx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	xproxy "golang.org/x/net/proxy"

	"schemo/internal/domain"
)

type Config struct {
	Timeout         time.Duration
	MaxBodyBytes    int64
	Proxy           domain.ProxySettings
	Requests        domain.Requests
	UserAgent       string
	SkipTLSVerify   bool
	TrustLocalhost  bool
	Roots           []domain.Root
	KeepConnections bool
}

const (
	defaultTimeout      = 60 * time.Second
	defaultMaxBodyBytes = 8 << 20
	dialTimeout         = 30 * time.Second
)

func dialing(
	proxy domain.ProxySettings,
) (func(*http.Request) (*url.URL, error), func(context.Context, string, string) (net.Conn, error)) {
	if proxy.Mode == domain.ProxyManual && proxy.Protocol == domain.ProxySOCKS5 {
		if proxy.Address() == "" {
			return nil, nil
		}
		return nil, socksDial(proxy)
	}
	return proxyOf(proxy), nil
}

func socksDial(proxy domain.ProxySettings) func(context.Context, string, string) (net.Conn, error) {
	direct := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialTimeout}
	proxied, err := socksThrough(proxy.Address(), proxy.User, proxy.Password)
	if err != nil {
		// tcp is a network the dialer always speaks, so this does not happen; a proxy the engine
		// could not reach through is one it does without.
		return direct.DialContext
	}
	kept := bypassOf(proxy)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if kept(addr) {
			return direct.DialContext(ctx, network, addr)
		}
		return proxied(ctx, network, addr)
	}
}

func socksThrough(
	address, user, password string,
) (func(context.Context, string, string) (net.Conn, error), error) {
	held, err := xproxy.SOCKS5("tcp", address, socksAuth(user, password),
		&net.Dialer{Timeout: dialTimeout, KeepAlive: dialTimeout})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if through, ok := held.(xproxy.ContextDialer); ok {
			return through.DialContext(ctx, network, addr)
		}
		return held.Dial(network, addr)
	}, nil
}

func socksAuth(user, password string) *xproxy.Auth {
	if user == "" {
		return nil
	}
	return &xproxy.Auth{User: user, Password: password}
}

func bypassOf(proxy domain.ProxySettings) func(addr string) bool {
	byURL := (&httpproxy.Config{
		HTTPProxy:  "socks5://" + proxy.Address(),
		HTTPSProxy: "socks5://" + proxy.Address(),
		NoProxy:    strings.Join(proxy.Bypass, ","),
	}).ProxyFunc()
	return func(addr string) bool {
		held, err := byURL(&url.URL{Scheme: "https", Host: addr})
		return err == nil && held == nil
	}
}

func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.Requests == (domain.Requests{}) {
		c.Requests = domain.DefaultRequests()
	}
	return c
}

func newTransport(cfg Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = !cfg.KeepConnections
	transport.Proxy, transport.DialContext = dialing(cfg.Proxy)
	if held := tlsConfigFor(cfg); held != nil {
		transport.TLSClientConfig = held
	}
	transport.DisableCompression = !cfg.Requests.AcceptEncoding
	if cfg.Requests.HTTPVersion == domain.HTTP1 {
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return transport
}

func proxyOf(proxy domain.ProxySettings) func(*http.Request) (*url.URL, error) {
	switch proxy.Mode {
	case domain.ProxyOff:
		return nil
	case domain.ProxyManual:
		address := proxyURL(proxy)
		if address == "" {
			// A manual mode with no address yet is a request that goes direct, which is what somebody
			// who has just switched to it and typed nothing means.
			return nil
		}
		byURL := (&httpproxy.Config{
			HTTPProxy:  address,
			HTTPSProxy: address,
			NoProxy:    strings.Join(proxy.Bypass, ","),
		}).ProxyFunc()
		return func(req *http.Request) (*url.URL, error) { return byURL(req.URL) }
	}
	return http.ProxyFromEnvironment
}

func proxyURL(proxy domain.ProxySettings) string {
	address := proxy.Address()
	if address == "" {
		return ""
	}
	held := url.URL{Scheme: "http", Host: address}
	if proxy.User != "" {
		held.User = url.UserPassword(proxy.User, proxy.Password)
	}
	return held.String()
}

func tlsConfigFor(cfg Config) *tls.Config {
	if !cfg.SkipTLSVerify && len(cfg.Roots) == 0 {
		return nil
	}
	held := &tls.Config{InsecureSkipVerify: cfg.SkipTLSVerify}
	if len(cfg.Roots) > 0 {
		held.RootCAs = rootPool(systemPool(), cfg.Roots)
	}
	return held
}

func systemPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		return x509.NewCertPool()
	}
	return pool
}

func rootPool(base *x509.CertPool, roots []domain.Root) *x509.CertPool {
	for _, root := range roots {
		base.AppendCertsFromPEM([]byte(root.PEM))
	}
	return base
}

func redirectPolicy(sent domain.Requests) func(*http.Request, []*http.Request) error {
	if !sent.FollowRedirects {
		return func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return func(_ *http.Request, via []*http.Request) error {
		if len(via) >= sent.MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", sent.MaxRedirects)
		}
		return nil
	}
}
