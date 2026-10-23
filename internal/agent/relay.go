package agent

import (
	"encoding/json"
	"io"
	"net"
	"time"
)

// Relay pipes the browser's stdio to the app's socket until either side closes. The app speaks the
// native messaging framing, so bytes are copied as is. Without the app it replies "unavailable", so
// the extension can tell "not running" from "not installed".
func Relay(stdin io.Reader, stdout io.Writer, socket string) error {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		// The one frame the relay writes itself: a browser that opened the host is told the app is
		// not running, which the extension words differently from "not installed".
		reply, marshalErr := json.Marshal(map[string]string{"type": "unavailable"})
		if marshalErr != nil {
			return marshalErr
		}
		return writeFrame(stdout, reply)
	}
	defer conn.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(conn, stdin)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stdout, conn)
		done <- struct{}{}
	}()
	<-done
	return nil
}
