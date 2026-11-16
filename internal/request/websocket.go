package request

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"schemo/internal/domain"
)

func (e *Engine) webSocket(ctx context.Context, spec Spec, policy transportPolicy, started time.Time) *domain.Response {
	response := &domain.Response{}
	defer func() { response.DurationUs = time.Since(started).Microseconds() }()

	raw := strings.TrimSpace(spec.URL)
	if !strings.HasPrefix(raw, "ws://") && !strings.HasPrefix(raw, "wss://") {
		response.Error = "WebSocket address must start with ws:// or wss://"
		return response
	}
	if spec.Digest != nil {
		response.Error = "WebSocket does not support Digest authorization"
		return response
	}
	req, err := e.newRequest(ctx, spec, policy.cfg)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	subprotocols := []string{}
	for _, value := range req.Header.Values("Sec-WebSocket-Protocol") {
		for protocol := range strings.SplitSeq(value, ",") {
			if protocol = strings.TrimSpace(protocol); protocol != "" {
				subprotocols = append(subprotocols, protocol)
			}
		}
	}
	req.Header.Del("Sec-WebSocket-Protocol")
	host := req.Header.Get("Host")
	req.Header.Del("Host")

	client := policy.client(nil)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conn, handshake, err := websocket.Dial(ctx, raw, &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: req.Header, Host: host, Subprotocols: subprotocols,
	})
	if handshake != nil {
		response.Status, response.StatusText = handshake.StatusCode, handshake.Status
		response.Headers = headerPairs(handshake.Header)
		if spec.Jar != nil {
			hops := hopJar{jar: spec.Jar, workspace: spec.Workspace, ctx: ctx}
			hops.write(webSocketHTTPURL(raw), response.Headers)
			response.CookieWrites = hops.writes
		}
	}
	if err != nil {
		if handshake != nil && handshake.Body != nil {
			defer handshake.Body.Close()
			if data, _, readErr := readCapped(handshake.Body, 1024); readErr == nil {
				response.Body = string(data)
			}
		}
		return stopped(response, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(e.cfg.MaxBodyBytes)

	stream := e.liveStream(spec)
	stream.connect(response, func(call context.Context, message domain.SavedMessage, masked string) error {
		kind, payload, err := webSocketPayload(message)
		if err != nil {
			return err
		}
		if int64(len(payload)) > e.cfg.MaxBodyBytes {
			return fmt.Errorf("WebSocket message exceeds the size limit")
		}
		writeCtx, cancel := context.WithTimeout(call, dialTimeout)
		defer cancel()
		if err := conn.Write(writeCtx, kind, payload); err != nil {
			return err
		}
		entry := socketMessage("out", kind, payload)
		if masked != message.Data {
			entry.Data = masked
		}
		stream.append(entry)
		return nil
	})
	for {
		kind, payload, err := conn.Read(ctx)
		if err != nil {
			code := websocket.CloseStatus(err)
			if ctx.Err() == nil && code != websocket.StatusNormalClosure && code != websocket.StatusGoingAway &&
				!errors.Is(err, context.Canceled) {
				response.Error = err.Error()
			}
			break
		}
		stream.append(socketMessage("in", kind, payload))
	}
	_ = conn.CloseNow()
	stream.finish(response)
	return response
}

func socketMessage(direction string, kind websocket.MessageType, payload []byte) domain.StreamMessage {
	message := domain.StreamMessage{Direction: direction, Data: string(payload), Bytes: len(payload)}
	if kind == websocket.MessageBinary {
		message.Binary = true
		message.Data = base64.StdEncoding.EncodeToString(payload)
	}
	return message
}

func webSocketPayload(message domain.SavedMessage) (websocket.MessageType, []byte, error) {
	switch message.Format {
	case domain.MessageBinary:
		raw := strings.TrimSpace(message.Data)
		var payload []byte
		var err error
		if hexText, ok := strings.CutPrefix(raw, "0x"); ok {
			payload, err = hex.DecodeString(hexText)
		} else {
			payload, err = base64.StdEncoding.DecodeString(raw)
		}
		if err != nil {
			return 0, nil, fmt.Errorf("invalid binary message: %w", err)
		}
		return websocket.MessageBinary, payload, nil
	case domain.MessageJSON:
		if !json.Valid([]byte(message.Data)) {
			return 0, nil, fmt.Errorf("invalid JSON message")
		}
	case domain.MessageText:
	default:
		return 0, nil, fmt.Errorf("unknown WebSocket message format %q", message.Format)
	}
	if !utf8.ValidString(message.Data) {
		return 0, nil, fmt.Errorf("message must be valid UTF-8")
	}
	return websocket.MessageText, []byte(message.Data), nil
}
