package request

import (
	"errors"
	"strings"

	"schemo/internal/domain"
)

const eventStreamMediaType = "text/event-stream"

type protocolRules struct {
	bodyKind     domain.BodyKind
	metadata     bool
	cookieURL    func(string) string
	resolveFrame bool
	headers      []domain.HeaderPair
	encodeBody   func(parts, bool) (string, error)
	replay       func(domain.ProtocolOptions) *domain.ReplayOptions
	record       func(string, SendInput) (string, *domain.ReplayOptions, string)
	commandError error
}

var protocols = map[domain.Protocol]protocolRules{
	domain.ProtocolGraphQL: {
		bodyKind: domain.BodyJSON,
		headers: []domain.HeaderPair{
			{Name: "Content-Type", Value: domain.MediaJSON},
			{Name: "Accept", Value: "application/graphql-response+json, application/json"},
		},
		encodeBody: graphQLBody,
		replay:     graphQLReplayOptions,
		record:     graphQLRecord,
	},
	domain.ProtocolGRPC: {
		bodyKind:     domain.BodyJSON,
		metadata:     true,
		encodeBody:   func(input parts, _ bool) (string, error) { return input.body, nil },
		replay:       rpcReplayOptions,
		record:       rpcRecord,
		commandError: errors.New("gRPC requires an RPC client"),
	},
	domain.ProtocolWebSocket: {
		bodyKind:     domain.BodyRaw,
		cookieURL:    webSocketHTTPURL,
		resolveFrame: true,
		commandError: errors.New("WebSocket requires a session client"),
	},
	domain.ProtocolSSE: {
		bodyKind: domain.BodyRaw,
		headers:  []domain.HeaderPair{{Name: "Accept", Value: eventStreamMediaType}},
	},
}

func optionsOf(options *domain.ProtocolOptions) domain.ProtocolOptions {
	if options != nil {
		return *options
	}
	return domain.ProtocolOptions{}
}

func requestBodyKind(draft domain.Draft) domain.BodyKind {
	if kind := protocols[draft.Protocol].bodyKind; kind != "" {
		return kind
	}
	return domain.KindOf(draft.BodyKind)
}

func (rules protocolRules) body(input parts, masked bool) (string, error) {
	if rules.encodeBody == nil {
		return "", nil
	}
	return rules.encodeBody(input, masked)
}

func (s *Drafts) renderRequestBody(
	draft domain.Draft, input parts, kind domain.BodyKind, boundary string, masked bool,
) (string, string, error) {
	if draft.Protocol == "" {
		return s.renderBody(kind, input.body, draft.BodyFile, input.form, boundary, masked)
	}
	body, err := protocols[draft.Protocol].body(input, masked)
	return body, "", err
}

func requestHeaders(draft domain.Draft, kind domain.BodyKind, boundary string, headers []domain.HeaderPair) []domain.HeaderPair {
	if draft.Protocol == "" {
		return withContentType(headers, kind, draft.BodyFile, boundary)
	}
	for _, header := range protocols[draft.Protocol].headers {
		if !hasHeader(headers, header.Name) {
			headers = append(headers, header)
		}
	}
	return headers
}

func (rules protocolRules) httpURL(raw string) string {
	if rules.cookieURL != nil {
		return rules.cookieURL(raw)
	}
	return raw
}

func webSocketHTTPURL(raw string) string {
	if rest, ok := strings.CutPrefix(raw, "wss://"); ok {
		return "https://" + rest
	}
	if rest, ok := strings.CutPrefix(raw, "ws://"); ok {
		return "http://" + rest
	}
	return raw
}

func recordRequest(body string, in SendInput) (string, *domain.ReplayOptions, string) {
	if in.Protocol.Live() {
		return "", in.RecordOptions, ""
	}
	if record := protocols[in.Protocol].record; record != nil {
		return record(body, in)
	}
	return body, in.RecordOptions, ""
}
