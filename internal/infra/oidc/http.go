package oidc

import (
	"net/http"
	"time"

	"schemo/internal/infra/channel"
)

// HTTPClient keeps account TLS independent of request editor settings.
func HTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: http.DefaultTransport.(*http.Transport).Clone()}
}

type accountTransport struct{ base http.RoundTripper }

func (t accountTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := channel.URL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}
