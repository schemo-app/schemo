package domain

import (
	"net"
	"strconv"
	"strings"
)

type ProxyMode string

const (
	ProxySystem ProxyMode = "system"
	ProxyManual ProxyMode = "manual"
	ProxyOff    ProxyMode = "off"
)

func (m ProxyMode) Valid() bool {
	return m == ProxySystem || m == ProxyManual || m == ProxyOff
}

type ProxyProtocol string

const (
	ProxyHTTP   ProxyProtocol = "http"
	ProxyHTTPS  ProxyProtocol = "https"
	ProxySOCKS5 ProxyProtocol = "socks5"
)

func (p ProxyProtocol) Valid() bool {
	return p == ProxyHTTP || p == ProxyHTTPS || p == ProxySOCKS5
}

type ProxySettings struct {
	Mode        ProxyMode     `json:"mode"`
	Protocol    ProxyProtocol `json:"protocol"`
	Host        string        `json:"host"`
	Port        int           `json:"port"`
	User        string        `json:"user"`
	Password    string        `json:"password,omitempty"`
	HasPassword bool          `json:"hasPassword"`
	Bypass      []string      `json:"bypass"`
}

// DefaultProxy is what a fresh installation does: follow the environment, as it always has.
func DefaultProxy() ProxySettings {
	return ProxySettings{Mode: ProxySystem, Protocol: ProxyHTTP, Bypass: []string{}}
}

func (p ProxySettings) Address() string {
	if p.Host == "" {
		return ""
	}
	if p.Port <= 0 {
		return p.Host
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}

func (p ProxySettings) Valid() error {
	if !p.Mode.Valid() {
		return Refuse(CodeUnknownProxyMode, ErrNotAllowed, Args{"mode": string(p.Mode)})
	}
	if p.Mode != ProxyManual {
		return nil
	}
	if !p.Protocol.Valid() {
		return Refuse(CodeUnknownProxyProtocol, ErrNotAllowed, Args{"protocol": string(p.Protocol)})
	}
	if !isProxyHost(p.Host) {
		return Refuse(CodeBadProxyAddress, ErrNotAllowed, Args{"address": p.Host})
	}
	if p.Port < 0 || p.Port > 65535 {
		return Refuse(CodeBadProxyAddress, ErrNotAllowed, Args{"address": strconv.Itoa(p.Port)})
	}
	return nil
}

func isProxyHost(host string) bool {
	if host == "" {
		return true
	}
	if strings.ContainsAny(host, " /\\?#@") {
		return false
	}
	return !strings.Contains(host, ":") || net.ParseIP(host) != nil
}

type ProxyProbe struct {
	Error     string `json:"error,omitempty"`
	Millis    int    `json:"millis"`
	Status    int    `json:"status"`
	OK        bool   `json:"ok"`
	NeedsAuth bool   `json:"needsAuth"`
}

type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
