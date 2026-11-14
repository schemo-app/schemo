package request

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"schemo/internal/domain"
)

type StreamSnapshot struct {
	Open     bool                   `json:"open"`
	Messages []domain.StreamMessage `json:"messages"`
	Count    int                    `json:"count"`
	First    int                    `json:"first"`
}

// Stream holds a bounded transcript shared by one transport and the history service.
type Stream struct {
	mu      sync.Mutex
	opened  func(*domain.Response)
	writer  func(context.Context, domain.SavedMessage, string) error
	writeMu sync.Mutex
	open    bool
	record  domain.Record
	lines   []domain.StreamMessage
	bytes   int64
	limit   int64
	count   int
}

const maxStreamMessages = 1000

func (s *Stream) connect(response *domain.Response, writer func(context.Context, domain.SavedMessage, string) error) {
	s.mu.Lock()
	s.open, s.writer = true, writer
	s.mu.Unlock()
	if s.opened != nil {
		s.opened(response)
	}
}

func (s *Stream) setRecord(record domain.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record = record
}

func (s *Stream) activeRecord() (domain.Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, s.record.ID != ""
}

func (s *Stream) append(message domain.StreamMessage) {
	message.At = time.Now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	message.Sequence = s.count
	size := streamMessageSize(message)
	if size > s.limit {
		return
	}
	for len(s.lines) >= maxStreamMessages || s.bytes+size > s.limit {
		s.bytes -= streamMessageSize(s.lines[0])
		s.lines[0] = domain.StreamMessage{}
		s.lines = s.lines[1:]
	}
	s.lines = append(s.lines, message)
	s.bytes += size
}

func streamMessageSize(message domain.StreamMessage) int64 {
	return int64(len(message.Data)+len(message.ID)+len(message.Name))*6 + 160
}

func (s *Stream) snapshot(after int) StreamSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := s.count + 1
	if len(s.lines) > 0 {
		first = s.lines[0].Sequence
	}
	at, _ := slices.BinarySearchFunc(s.lines, min(after, s.count)+1, func(message domain.StreamMessage, sequence int) int {
		return cmp.Compare(message.Sequence, sequence)
	})
	return StreamSnapshot{Open: s.open, Messages: slices.Clone(s.lines[at:]), Count: s.count, First: first}
}

func (s *Stream) finish(response *domain.Response) {
	s.writeMu.Lock()
	s.mu.Lock()
	s.open, s.writer = false, nil
	s.mu.Unlock()
	s.writeMu.Unlock()

	snapshot := s.snapshot(0)
	var body strings.Builder
	for _, message := range snapshot.Messages {
		line, _ := json.Marshal(message)
		body.Write(line)
		body.WriteByte('\n')
	}
	response.Body = body.String()
	response.BodyTruncated = snapshot.Count > len(snapshot.Messages)
	response.MessageCount = snapshot.Count
}

func (s *Stream) send(ctx context.Context, message domain.SavedMessage, masked string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	writer := s.writer
	s.mu.Unlock()
	if writer == nil {
		return fmt.Errorf("WebSocket is disconnected")
	}
	return writer(ctx, message, masked)
}

// Live returns the current transcript of a running connection.
func (s *History) Live(ctx context.Context, id string, after int) *StreamSnapshot {
	active := s.findAttempt(id)
	if active == nil || active.stream == nil {
		return nil
	}
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil || workspace != active.workspace {
		return nil
	}
	snapshot := active.stream.snapshot(max(0, after))
	return &snapshot
}

func (s *History) sendMessage(ctx context.Context, id string, message domain.SavedMessage) error {
	active := s.findAttempt(id)
	if active == nil || active.stream == nil || active.message == nil {
		return fmt.Errorf("WebSocket is disconnected")
	}
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if workspace != active.workspace {
		return domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed, nil)
	}
	value, masked := active.message(message.Data)
	message.Data = value
	return active.stream.send(ctx, message, masked)
}
