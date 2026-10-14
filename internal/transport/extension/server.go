// Package extension serves the extension on a unix socket in the app's data folder, reached through
// the native messaging host (infra/nativehost). No network port is opened.
package extension

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"schemo/internal/platform"
)

// maxFrame defines the maximum allowed payload size (32 MB) for incoming and outgoing socket frames.
const maxFrame = 32 << 20

type client struct {
	conn net.Conn
	mu   sync.Mutex
	// heard tracks whether the peer has sent a valid operational frame (popup probes connect and disconnect without sending messages).
	heard bool
}

func (c *client) write(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeFrame(c.conn, payload)
}

// Server manages Unix domain socket connections for browser extension integration.
type Server struct {
	ingest Ingest
	dir    platform.DataDir
	build  platform.BuildInfo

	mu       sync.Mutex
	clients  map[*client]struct{}
	listener net.Listener
	// state is what the extension last said about itself. It is kept beside the clients because a
	// window that opens while a browser is already connected has to draw that, and the frames
	// arrive when something changes rather than on a schedule.
	state CaptureState

	listening atomic.Bool
}

// Status is what a window draws about the extension: whether a browser is connected right now, and
// what it last said about itself. The state of a connection that ended is not one to draw — it
// describes a browser that is gone.
type Status struct {
	Connected bool         `json:"connected"`
	State     CaptureState `json:"state"`
}

// NewServer initializes and returns a new extension Unix socket server instance.
func NewServer(ingest Ingest, dir platform.DataDir, build platform.BuildInfo) *Server {
	if ingest == nil {
		ingest = nopIngest{}
	}
	return &Server{ingest: ingest, dir: dir, build: build, clients: make(map[*client]struct{})}
}

// Listening returns true if the server is currently accepting connections on its socket.
func (s *Server) Listening() bool {
	return s.listening.Load()
}

// Start binds to the Unix socket path without blocking, clearing stale sockets or returning an error if another instance is active.
func (s *Server) Start() error {
	path := s.dir.BridgeSocketPath()

	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = conn.Close()
		return fmt.Errorf("another copy of the app is listening on %s", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clearing %s: %w", path, err)
	}
	if err := s.dir.Create(); err != nil {
		return err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", path, err)
	}
	// Said rather than returned: the folder is 0700, so this is the second lock on a socket that
	// carries what the browser sent, and a filesystem without a mode must not cost the extension.
	if err := os.Chmod(path, 0o600); err != nil {
		log.Printf("[extension] %s could not be made private: %v", path, err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	s.listening.Store(true)
	log.Printf("[extension] listening on %s", path)

	go s.accept(listener)
	return nil
}

func (s *Server) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("[extension] error: %v", err)
			}
			return
		}
		go s.serve(conn)
	}
}

// Shutdown gracefully closes all active client connections, stops the socket listener, and removes the socket file.
func (s *Server) Shutdown(context.Context) error {
	s.listening.Store(false)

	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	for c := range s.clients {
		_ = c.conn.Close()
	}
	s.clients = make(map[*client]struct{})
	s.mu.Unlock()

	if listener == nil {
		return nil
	}
	err := listener.Close()
	_ = os.Remove(s.dir.BridgeSocketPath())
	return err
}

// Broadcast writes a payload to all connected clients concurrently outside the mutex lock to prevent stalled peers from blocking others.
func (s *Server) Broadcast(payload []byte) {
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		_ = c.write(payload)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()

	c := &client{conn: conn}
	hello, _ := json.Marshal(HelloFrame{Type: "hello", App: s.build.Slug, Version: s.build.Version})
	if err := c.write(hello); err != nil {
		return
	}

	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		remaining := 0
		for other := range s.clients {
			if other.heard {
				remaining++
			}
		}
		heard := c.heard
		if heard && remaining == 0 {
			// The browser that described this is gone, and a window that opens now must not be told
			// about the tabs it used to have.
			s.state = CaptureState{}
		}
		s.mu.Unlock()

		if heard && remaining == 0 {
			s.ingest.ClientGone()
		}
	}()

	for {
		msg, err := readFrame(conn)
		if err != nil {
			return
		}
		if s.dispatch(msg) {
			s.mu.Lock()
			c.heard = true
			s.mu.Unlock()
		}
	}
}

// remember keeps what the extension last said about itself.
func (s *Server) remember(state CaptureState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// Status is whether a browser is connected right now, and what it last said about itself. A client
// that has said nothing does not count: the extension's popup probes the socket and goes away
// without a word, and a probe is not a connection.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		if c.heard {
			return Status{Connected: true, State: s.state}
		}
	}
	return Status{}
}

// dispatch routes incoming frame messages based on their envelope type and notifies the ingest handler.
func (s *Server) dispatch(msg []byte) bool {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return false
	}

	switch envelope.Type {
	case "request":
		var req CapturedRequest
		if json.Unmarshal(msg, &req) != nil {
			return false
		}
		s.ingest.Captured(req)
	case "state":
		var st CaptureState
		if json.Unmarshal(msg, &st) != nil {
			return false
		}
		s.remember(st)
		s.ingest.StateChanged(st)
	case "focus":
		var fr FocusRequest
		if json.Unmarshal(msg, &fr) != nil {
			return false
		}
		s.ingest.FocusRequested(fr)
	case "filters":
		var ff FiltersFrame
		if json.Unmarshal(msg, &ff) != nil {
			return false
		}
		s.ingest.FiltersChanged(ff.Filters)
		// Other connected browsers apply the same rules.
		s.Broadcast(msg)
	default:
		return false
	}
	return true
}

// readFrame reads a native messaging format frame consisting of a 4-byte little-endian length header followed by the JSON payload.
func readFrame(r io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	if size > maxFrame {
		return nil, fmt.Errorf("a %d-byte frame", size)
	}
	msg := make([]byte, size)
	_, err := io.ReadFull(r, msg)
	return msg, err
}

func writeFrame(w io.Writer, payload []byte) error {
	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	_, err := w.Write(frame)
	return err
}
