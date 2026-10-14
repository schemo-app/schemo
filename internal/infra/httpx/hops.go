package httpx

import (
	"context"
	"net/http"

	"schemo/internal/domain"
)

type hopJar struct {
	jar       Jar
	workspace string
	ctx       context.Context
	writes    []domain.CookieWrite
}

func (h *hopJar) carry(req *http.Request) {
	header, err := h.jar.CookieHeader(h.ctx, h.workspace, req.URL.String())
	if err != nil || header == "" {
		req.Header.Del("Cookie")
		return
	}
	req.Header.Set("Cookie", header)
}

func (h *hopJar) write(rawURL string, headers []domain.HeaderPair) {
	if len(headers) == 0 {
		return
	}
	h.writes = append(h.writes, h.jar.Apply(h.ctx, h.workspace, rawURL, headers)...)
}
