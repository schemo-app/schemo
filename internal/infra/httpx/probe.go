package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
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

func (e *Engine) ProxyEnvironment() []domain.EnvEntry {
	names := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	out := make([]domain.EnvEntry, 0, len(names))
	for _, name := range names {
		out = append(out, domain.EnvEntry{Key: name, Value: envValue(name)})
	}
	return out
}

func envValue(name string) string {
	return firstNonEmpty(os.Getenv(strings.ToLower(name)), os.Getenv(name))
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
		return parseProbeTarget(firstNonEmpty(held.HTTPSProxy, held.HTTPProxy))
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
	if _, err := io.WriteString(conn, connectRequest(target)); err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return domain.ProxyProbe{Millis: elapsed(start), Error: err.Error()}
	}
	status := statusOf(line)
	return domain.ProxyProbe{
		OK:        status >= 200 && status < 300,
		Millis:    elapsed(start),
		NeedsAuth: status == 407,
		Status:    status,
	}
}

func connectRequest(target probeTarget) string {
	held := "CONNECT " + probeHost + " HTTP/1.1\r\nHost: " + probeHost + "\r\n"
	if target.user != "" {
		token := base64.StdEncoding.EncodeToString([]byte(target.user + ":" + target.password))
		held += "Proxy-Authorization: Basic " + token + "\r\n"
	}
	return held + "\r\n"
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

func statusOf(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return code
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
