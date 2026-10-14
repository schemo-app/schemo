package settings

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"schemo/internal/domain"
)

func (u *UseCase) SetProxy(ctx context.Context, patch ProxyPatch) (domain.Settings, error) {
	held, err := u.network(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	proxy := held.Proxy
	if patch.Mode != nil {
		proxy.Mode = *patch.Mode
	}
	if patch.Protocol != nil {
		proxy.Protocol = *patch.Protocol
	}
	if patch.Host != nil {
		proxy.Host = strings.TrimSpace(*patch.Host)
	}
	if patch.Port != nil {
		proxy.Port = *patch.Port
	}
	if patch.User != nil {
		proxy.User = strings.TrimSpace(*patch.User)
	}
	if patch.Password != nil {
		proxy.Password = *patch.Password
	}
	if patch.Bypass != nil {
		proxy.Bypass = cleanBypass(*patch.Bypass)
	}
	if err := proxy.Valid(); err != nil {
		return domain.Settings{}, err
	}
	if err := u.saveBlob(ctx, domain.SettingProxy, proxy); err != nil {
		return domain.Settings{}, err
	}
	held.Proxy = proxy
	u.outbound.Apply(held)
	return u.announced(u.Snapshot(ctx))
}

func (u *UseCase) ParseProxyAddress(raw string) (ProxyPatch, error) {
	held := strings.TrimSpace(raw)
	if !strings.Contains(held, "://") {
		held = "http://" + held
	}
	parsed, err := url.Parse(held)
	if err != nil || parsed.Hostname() == "" {
		return ProxyPatch{}, domain.Refuse(
			domain.CodeBadProxyAddress, domain.ErrNotAllowed, domain.Args{"address": raw})
	}

	protocol, ok := protocolOfScheme(parsed.Scheme)
	if !ok {
		return ProxyPatch{}, domain.Refuse(
			domain.CodeUnknownProxyProtocol, domain.ErrNotAllowed,
			domain.Args{"protocol": parsed.Scheme})
	}
	host := parsed.Hostname()
	out := ProxyPatch{Protocol: &protocol, Host: &host}
	if port := parsed.Port(); port != "" {
		if number, err := strconv.Atoi(port); err == nil {
			out.Port = &number
		}
	}
	if parsed.User != nil {
		user := parsed.User.Username()
		out.User = &user
		if password, ok := parsed.User.Password(); ok {
			out.Password = &password
		}
	}
	return out, nil
}

func protocolOfScheme(scheme string) (domain.ProxyProtocol, bool) {
	switch strings.ToLower(scheme) {
	case "http":
		return domain.ProxyHTTP, true
	case "https":
		return domain.ProxyHTTPS, true
	case "socks5", "socks5h":
		return domain.ProxySOCKS5, true
	}
	return "", false
}

func (u *UseCase) RevealProxyPassword(ctx context.Context) (string, error) {
	held, err := u.network(ctx)
	if err != nil {
		return "", err
	}
	return held.Proxy.Password, nil
}

func parseProxy(raw string) domain.ProxySettings {
	out := domain.DefaultProxy()
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.DefaultProxy()
	}
	if !out.Mode.Valid() {
		out.Mode = domain.ProxySystem
	}
	if !out.Protocol.Valid() {
		out.Protocol = domain.ProxyHTTP
	}
	out.Bypass = domain.OrEmpty(out.Bypass)
	return out
}

func cleanBypass(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if trimmed := strings.TrimSpace(host); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
