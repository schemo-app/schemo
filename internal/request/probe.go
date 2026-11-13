package request

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"

	"schemo/internal/domain"
)

const defaultProbeAddress = "example.com:443"

func (e *Engine) Probe(ctx context.Context, destination string) domain.ProxyProbe {
	address, err := probeAddress(destination)
	if err != nil {
		return domain.ProxyProbe{Error: err.Error()}
	}
	cfg := e.net.settings()
	target := probeTargetOf(cfg.Proxy)
	if target.address == "" {
		// No proxy and none in the environment: there is no tunnel to open, and the request the
		// button stands for goes out directly.
		return domain.ProxyProbe{}
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if target.protocol == domain.ProxySOCKS5 {
		return probeSOCKS(ctx, target, address, start)
	}
	return probeTunnel(ctx, target, address, tlsConfigFor(cfg), start)
}

func probeAddress(destination string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return defaultProbeAddress, nil
	}
	if !hasScheme(destination) {
		destination = "https://" + destination
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("enter a host, host:port, or HTTP(S) URL to test")
	}
	port := cmp.Or(parsed.Port(), "443")
	if parsed.Scheme == "http" && parsed.Port() == "" {
		port = "80"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("test destination port must be between 1 and 65535")
	}
	return net.JoinHostPort(parsed.Hostname(), port), nil
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
		address:  proxyProbeAddress(proxy.Host, strconv.Itoa(proxy.Port), proxy.Protocol),
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
	protocol := protocolOf(parsed.Scheme)
	out := probeTarget{protocol: protocol, address: proxyProbeAddress(parsed.Hostname(), parsed.Port(), protocol)}
	if parsed.User != nil {
		out.user = parsed.User.Username()
		out.password, _ = parsed.User.Password()
	}
	return out
}

func proxyProbeAddress(host, port string, protocol domain.ProxyProtocol) string {
	if host == "" {
		return ""
	}
	if port == "" || port == "0" {
		switch protocol {
		case domain.ProxyHTTPS:
			port = "443"
		case domain.ProxySOCKS5:
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
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
	address string,
	config *tls.Config,
	start time.Time,
) domain.ProxyProbe {
	raw, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", target.address)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()

	conn := net.Conn(raw)
	if target.protocol == domain.ProxyHTTPS {
		if config == nil {
			config = &tls.Config{}
		}
		config.ServerName, _, _ = net.SplitHostPort(target.address)
		held := tls.Client(raw, config)
		if err := held.HandshakeContext(ctx); err != nil {
			return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
		}
		conn = held
	}
	request := connectRequest(target, address)
	if err := request.Write(conn); err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	response, err := http.ReadResponse(bufio.NewReader(io.LimitReader(conn, http.DefaultMaxHeaderBytes)), request)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	// The raw connection is closed on return. Draining the body could block on the open tunnel.
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
func connectRequest(target probeTarget, address string) *http.Request {
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: address},
		Host:   address,
		Header: http.Header{},
	}
	if target.user != "" {
		request.Header.Set("Proxy-Authorization", domain.BasicAuthorization(target.user, target.password))
	}
	return request
}

func probeSOCKS(ctx context.Context, target probeTarget, address string, start time.Time) domain.ProxyProbe {
	through, err := socksThrough(target.address, target.user, target.password)
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	conn, err := through(ctx, "tcp", address)
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

func elapsed(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}
