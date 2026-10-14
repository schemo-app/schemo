package extension

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
)

func frame(payload string) []byte {
	out := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(out, uint32(len(payload)))
	copy(out[4:], payload)
	return out
}

func TestWithoutTheAppTheExtensionIsToldItIsUnavailable(t *testing.T) {
	var out bytes.Buffer

	if err := Relay(bytes.NewReader(nil), &out, filepath.Join(t.TempDir(), "none.sock")); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var size uint32
	_ = binary.Read(&out, binary.LittleEndian, &size)
	var message map[string]string
	_ = json.Unmarshal(out.Next(int(size)), &message)
	if message["type"] != "unavailable" {
		t.Errorf("told %v, want unavailable", message)
	}
}

func TestRelayCarriesFramesBothWaysUnchanged(t *testing.T) {
	dir := shortDir(t)
	socket := filepath.Join(string(dir), "s.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	fromBrowser := frame(`{"type":"state"}`)
	fromApp := frame(`{"type":"hello"}`)
	received := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write(fromApp)
		got := make([]byte, len(fromBrowser))
		_, _ = io.ReadFull(conn, got)
		received <- got
	}()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	finished := make(chan error, 1)
	go func() { finished <- Relay(stdinR, stdoutW, socket) }()

	_, _ = stdinW.Write(fromBrowser)
	toBrowser := make([]byte, len(fromApp))
	_, _ = io.ReadFull(stdoutR, toBrowser)

	if got := <-received; !bytes.Equal(got, fromBrowser) {
		t.Errorf("the app got %q, want %q", got, fromBrowser)
	}
	if !bytes.Equal(toBrowser, fromApp) {
		t.Errorf("the browser got %q, want %q", toBrowser, fromApp)
	}

	_ = stdinW.Close()
	if err := <-finished; err != nil {
		t.Errorf("Relay: %v", err)
	}
}

func TestLaunchedTellsABrowserStartFromAPersonsStart(t *testing.T) {
	manifestPath := "/home/u/.mozilla/native-messaging-hosts/" + HostName + ".json"
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"chrome-extension://klimdljbmlanhmdjfgbilkkhpahgjmhd/"}, true},
		{[]string{"chrome-extension://klimdljbmlanhmdjfgbilkkhpahgjmhd/", "--parent-window=0"}, true},
		{[]string{manifestPath, FirefoxExtensionID}, true},
		{nil, false},
		{[]string{"schemo://open?tab=2"}, false},
	}
	for _, c := range cases {
		if got := Launched(c.args); got != c.want {
			t.Errorf("Launched(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}
