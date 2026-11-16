package request

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"schemo/internal/domain"
)

func (e *Engine) eventStream(
	ctx context.Context, spec Spec, policy transportPolicy, start time.Time, cancel context.CancelCauseFunc,
) *domain.Response {
	var handshake *time.Timer
	if seconds := policy.cfg.Requests.Timeout; seconds > 0 {
		handshake = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
			cancel(context.DeadlineExceeded)
		})
		defer handshake.Stop()
		policy.cfg.Requests.Timeout = 0
	}
	return e.doHTTP(ctx, spec, policy, start, func(resp *http.Response, result *domain.Response, connected func()) bool {
		if resp.StatusCode != http.StatusOK {
			return false
		}
		if domain.MediaTypeOf(result.ContentType) != eventStreamMediaType {
			result.Error = fmt.Sprintf("expected %s, received %q", eventStreamMediaType, result.ContentType)
			return false
		}
		if handshake != nil && !handshake.Stop() {
			stopped(result, context.DeadlineExceeded)
			return true
		}
		if cause := context.Cause(ctx); cause != nil {
			stopped(result, cause)
			return true
		}
		connected()
		e.readEvents(ctx, e.liveStream(spec), resp.Body, result, start)
		return true
	})
}

func (e *Engine) readEvents(
	ctx context.Context, stream *Stream, body io.Reader, response *domain.Response, started time.Time,
) {
	response.ContentType = eventStreamMediaType
	stream.connect(response, nil)
	err := parseEvents(body, e.cfg.MaxBodyBytes, stream.append)
	if err != nil && ctx.Err() == nil {
		response.Error = err.Error()
	}
	response.DurationUs = time.Since(started).Microseconds()
	stream.finish(response)
}

func parseEvents(body io.Reader, limit int64, emit func(domain.StreamMessage)) error {
	reader := bufio.NewScanner(body)
	reader.Buffer(make([]byte, 4096), int(limit))
	reader.Split(eventLine)
	var data strings.Builder
	var name, id string
	first := true
	for reader.Scan() {
		line := strings.ToValidUTF8(reader.Text(), "�")
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if line == "" {
			if data.Len() > 0 {
				payload := strings.TrimSuffix(data.String(), "\n")
				event := name
				if event == "" {
					event = "message"
				}
				emit(domain.StreamMessage{Direction: "in", Name: event, ID: id, Data: payload, Bytes: len(payload)})
			}
			data.Reset()
			name = ""
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			name = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				id = value
			}
		case "data":
			if int64(data.Len()+len(value)+1) > limit {
				return fmt.Errorf("SSE event exceeds the response size limit")
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	return reader.Err()
}

func eventLine(data []byte, eof bool) (int, []byte, error) {
	for i, ch := range data {
		switch ch {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 == len(data) && !eof {
				return 0, nil, nil
			}
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
	}
	if eof && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
