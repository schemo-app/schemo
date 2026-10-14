package channel

import "testing"

func TestAccountURLs(t *testing.T) {
	for _, raw := range []string{"https://cloud.example", "https://cloud.example:8443/token", "http://localhost:8080", "http://127.0.0.2:8080", "http://[::1]:8080"} {
		if _, err := URL(raw); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://cloud.example", "http://192.168.1.1", "http://localhost.example", "ftp://localhost", "https://user:password@cloud.example", "https://cloud.example/#fragment", "/token", "cloud.example", "https://cloud.example:bad"} {
		if _, err := URL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestRemoteProvidersCannotDowngradeToLoopback(t *testing.T) {
	if err := Endpoint("https://cloud.example", "http://127.0.0.1/token"); err == nil {
		t.Fatal("accepted a plaintext endpoint from a remote provider")
	}
	if err := Endpoint("https://localhost", "http://localhost/token"); err == nil {
		t.Fatal("accepted an HTTPS downgrade on loopback")
	}
	if err := Endpoint("http://localhost:8080", "http://127.0.0.1:8080/token"); err != nil {
		t.Fatal(err)
	}
}

func TestGRPCRejectsRemotePlaintextBeforeDialing(t *testing.T) {
	dialer := New()
	defer dialer.Close()
	if _, err := dialer.Client("http://cloud.example:8080"); err == nil {
		t.Fatal("created an insecure remote gRPC connection")
	}
}
