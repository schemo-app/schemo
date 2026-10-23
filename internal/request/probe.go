package request

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"

	"schemo/internal/domain"
)

const probeHost = "example.com:443"

func (e *Engine) Probe(ctx context.Context) domain.ProxyProbe {
	cfg := e.net.settings()
	target := probeTargetOf(cfg.Proxy)
	if target.address == "" {
		// No proxy and none in the environment: there is no tunnel to open, and the request the
		// button stands for goes out directly.
		return domain.ProxyProbe{}
	}
	start := time.Now()
	if target.protocol == domain.ProxySOCKS5 {
		return probeSOCKS(ctx, target, start)
	}
	return probeTunnel(ctx, target, cfg.SkipTLSVerify, start)
}

// ProxyEnvironment is what the environment says about proxies, read the way the transport reads it:
// the upper-case name wins over the lower-case one.
func (e *Engine) ProxyEnvironment() []domain.EnvEntry {
	held := httpproxy.FromEnvironment()
	return []domain.EnvEntry{
		{Key: "HTTP_PROXY", Value: held.HTTPProxy},
		{Key: "HTTPS_PROXY", Value: held.HTTPSProxy},
		{Key: "NO_PROXY", Value: held.NoProxy},
	}
}

type probeTarget struct {
	protocol domain.ProxyProtocol
	address  string
	user     string
	password string
}

func probeTargetOf(proxy domain.ProxySettings) probeTarget {
	if proxy.Mode == domain.ProxySystem {
		held := httpproxy.FromEnvironment()
		return parseProbeTarget(cmp.Or(held.HTTPSProxy, held.HTTPProxy))
	}
	if proxy.Mode != domain.ProxyManual {
		return probeTarget{}
	}
	return probeTarget{
		protocol: proxy.Protocol,
		address:  proxy.Address(),
		user:     proxy.User,
		password: proxy.Password,
	}
}

func parseProbeTarget(raw string) probeTarget {
	if raw == "" {
		return probeTarget{}
	}
	held := raw
	if !hasScheme(held) {
		held = "http://" + held
	}
	parsed, err := url.Parse(held)
	if err != nil || parsed.Host == "" {
		return probeTarget{}
	}
	out := probeTarget{protocol: protocolOf(parsed.Scheme), address: parsed.Host}
	if parsed.User != nil {
		out.user = parsed.User.Username()
		out.password, _ = parsed.User.Password()
	}
	return out
}

func protocolOf(scheme string) domain.ProxyProtocol {
	switch strings.ToLower(scheme) {
	case "https":
		return domain.ProxyHTTPS
	case "socks5", "socks5h":
		return domain.ProxySOCKS5
	default:
		return domain.ProxyHTTP
	}
}

func probeTunnel(
	ctx context.Context,
	target probeTarget,
	skipVerify bool,
	start time.Time,
) domain.ProxyProbe {
	raw, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", target.address)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	defer raw.Close()

	conn := net.Conn(raw)
	if target.protocol == domain.ProxyHTTPS {
		held := tls.Client(raw, &tls.Config{
			ServerName:         hostOnly(target.address),
			InsecureSkipVerify: skipVerify,
		})
		if err := held.HandshakeContext(ctx); err != nil {
			return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
		}
		conn = held
	}
	if err := conn.SetDeadline(time.Now().Add(dialTimeout)); err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	request := connectRequest(target)
	if err := request.Write(conn); err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	_ = response.Body.Close()
	status := response.StatusCode
	return domain.ProxyProbe{
		OK:        status >= 200 && status < 300,
		Millis:    elapsed(start),
		NeedsAuth: status == 407,
		Status:    status,
	}
}

// connectRequest is the request that opens a tunnel to the probe host (RFC 9110 §9.3.6), with the
// proxy credentials the way a proxy asks for them (RFC 7235 §4.4).
func connectRequest(target probeTarget) *http.Request {
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: probeHost},
		Host:   probeHost,
		Header: http.Header{},
	}
	if target.user != "" {
		request.Header.Set("Proxy-Authorization", domain.BasicAuthorization(target.user, target.password))
	}
	return request
}

func probeSOCKS(ctx context.Context, target probeTarget, start time.Time) domain.ProxyProbe {
	through, err := socksThrough(target.address, target.user, target.password)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	conn, err := through(ctx, "tcp", probeHost)
	if err != nil {
		return domain.ProxyProbe{
			Millis:    elapsed(start),
			Error:     err.Error(),
			NeedsAuth: strings.Contains(err.Error(), "authentication"),
		}
	}
	_ = conn.Close()
	return domain.ProxyProbe{OK: true, Millis: elapsed(start)}
}

func hostOnly(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

func elapsed(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}
