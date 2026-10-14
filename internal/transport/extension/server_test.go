package extension

import (
	"encoding/json"
	"net"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

type recordingIngest struct {
	mu      sync.Mutex
	states  []CaptureState
	gone    int
	filters []domain.CaptureFilters
}

func (r *recordingIngest) Captured(CapturedRequest) {}
func (r *recordingIngest) StateChanged(st CaptureState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, st)
}
func (r *recordingIngest) ClientGone() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gone++
}
func (r *recordingIngest) FocusRequested(FocusRequest) {}
func (r *recordingIngest) FiltersChanged(f domain.CaptureFilters) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filters = append(r.filters, f)
}

func (r *recordingIngest) count() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states), r.gone
}

// shortDir keeps the socket path under the ~104-byte limit a unix socket has: the folder a test is
// handed gets long enough on macOS to cross it, and a socket over the limit fails to bind without
// saying why. Windows has no /tmp and no such limit, so there the system's own temp root will do.
func shortDir(t *testing.T) platform.DataDir {
	t.Helper()
	root := "/tmp"
	if runtime.GOOS == "windows" {
		root = os.TempDir()
	}
	dir, err := os.MkdirTemp(root, "sb")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return platform.DataDir(dir)
}

func startServer(t *testing.T, dir platform.DataDir) (*Server, *recordingIngest) {
	t.Helper()
	ingest := &recordingIngest{}
	s := NewServer(ingest, dir, platform.BuildInfo{Slug: "schemo", Version: "test"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(t.Context()) })
	return s, ingest
}

func connect(t *testing.T, dir platform.DataDir) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", dir.BridgeSocketPath())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func read(t *testing.T, conn net.Conn) map[string]string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, err := readFrame(conn)
	if err != nil {
		t.Fatalf("reading a frame: %v", err)
	}
	var frame map[string]string
	_ = json.Unmarshal(msg, &frame)
	return frame
}

func send(t *testing.T, conn net.Conn, frame any) {
	t.Helper()
	payload, _ := json.Marshal(frame)
	if err := writeFrame(conn, payload); err != nil {
		t.Fatalf("writing a frame: %v", err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAConnectionIsGreetedAndHeard(t *testing.T) {
	dir := shortDir(t)
	_, ingest := startServer(t, dir)
	conn := connect(t, dir)

	if hello := read(t, conn); hello["type"] != "hello" || hello["app"] != "schemo" {
		t.Fatalf("first frame %v, want the app's hello", hello)
	}
	send(t, conn, CaptureState{Type: "state", Tabs: 1})

	eventually(t, "the state frame", func() bool { n, _ := ingest.count(); return n == 1 })
}

func TestBroadcastReachesTheExtension(t *testing.T) {
	dir := shortDir(t)
	s, _ := startServer(t, dir)
	conn := connect(t, dir)
	read(t, conn)
	eventually(t, "the client", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.clients) == 1
	})

	s.Broadcast([]byte(`{"type":"pause"}`))

	if frame := read(t, conn); frame["type"] != "pause" {
		t.Errorf("got %v, want the pause", frame)
	}
}

func TestTheLastConnectionGoingIsReported(t *testing.T) {
	dir := shortDir(t)
	_, ingest := startServer(t, dir)
	conn := connect(t, dir)
	read(t, conn)
	send(t, conn, CaptureState{Type: "state", Tabs: 1})
	eventually(t, "the state frame", func() bool { n, _ := ingest.count(); return n == 1 })

	_ = conn.Close()

	eventually(t, "ClientGone", func() bool { _, gone := ingest.count(); return gone == 1 })
}

func TestAProbeLeavingIsNotTheExtensionGoing(t *testing.T) {
	dir := shortDir(t)
	_, ingest := startServer(t, dir)
	conn := connect(t, dir)
	read(t, conn)
	send(t, conn, map[string]string{"type": "probe"})

	_ = conn.Close()

	time.Sleep(200 * time.Millisecond)
	if _, gone := ingest.count(); gone != 0 {
		t.Errorf("a probe leaving was reported as the extension going away")
	}
}

// A window that opens after the extension has spoken is told what it said: the frames arrive when
// something changes, so a window that only listened would have nothing to draw.
func TestAWindowIsToldWhatTheExtensionLastSaid(t *testing.T) {
	dir := shortDir(t)
	s, _ := startServer(t, dir)
	conn := connect(t, dir)
	read(t, conn)

	if got := s.Status(); got.Connected {
		t.Errorf("status = %+v, want nothing before the extension has spoken", got)
	}

	send(t, conn, CaptureState{Type: "state", Browser: "Chrome 129", Tabs: 2})

	eventually(t, "the state to be remembered", func() bool {
		got := s.Status()
		return got.Connected && got.State.Browser == "Chrome 129" && got.State.Tabs == 2
	})
}

func TestTheSocketIsTheUsersAlone(t *testing.T) {
	// Windows has no group or other bits to read: a file there is the profile's, by its ACL.
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not describe access on Windows")
	}
	dir := shortDir(t)
	startServer(t, dir)

	info, err := os.Stat(dir.BridgeSocketPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("socket mode %v, want no access for group or others", perm)
	}
}

func TestASocketLeftByACrashIsReplaced(t *testing.T) {
	dir := shortDir(t)
	if err := os.WriteFile(dir.BridgeSocketPath(), nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	startServer(t, dir)

	read(t, connect(t, dir))
}

func TestASecondCopyDoesNotTakeTheSocket(t *testing.T) {
	dir := shortDir(t)
	startServer(t, dir)

	second := NewServer(nil, dir, platform.BuildInfo{})
	if err := second.Start(); err == nil {
		_ = second.Shutdown(t.Context())
		t.Fatalf("a second server took the socket of a running one")
	}
	read(t, connect(t, dir))
}

func TestFiltersFromOneBrowserAreStoredAndReachTheOthers(t *testing.T) {
	dir := shortDir(t)
	s, ingest := startServer(t, dir)
	chrome := connect(t, dir)
	firefox := connect(t, dir)
	read(t, chrome)
	read(t, firefox)
	eventually(t, "both clients", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.clients) == 2
	})

	send(t, chrome, FiltersFrame{Type: "filters", Filters: domain.CaptureFilters{Hosts: []string{"*.example.com"}, JSON: true}})

	if frame := read(t, firefox); frame["type"] != "filters" {
		t.Fatalf("the other browser got %v, want the filters", frame)
	}
	ingest.mu.Lock()
	defer ingest.mu.Unlock()
	if len(ingest.filters) != 1 || !ingest.filters[0].JSON {
		t.Errorf("stored %+v, want the edited filters", ingest.filters)
	}
}
