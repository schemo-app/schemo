package updater

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/domain"
)

// No webview or helper process is started by these integration tests.
type testHost struct{}

func (testHost) Emit(string, ...any) bool         { return true }
func (testHost) OnEvent(string, func(any)) func() { return func() {} }
func (testHost) OpenWindow(wailsupdater.WindowOptions) wailsupdater.WindowHandle {
	panic("unexpected update window")
}
func (testHost) Quit() { panic("unexpected application shutdown") }

func TestWailsAuthenticatesUpdateBeforeStaging(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed update artifact")
	digest := sha512.Sum512(payload)
	signature, err := private.Sign(rand.Reader, digest[:], &ed25519.Options{Hash: crypto.SHA512})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"signed", "unsigned", "digest-only", "tampered", "wrong-key"} {
		t.Run(mode, func(t *testing.T) {
			staging := t.TempDir()
			for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
				t.Setenv(name, staging)
			}
			artifact := map[string]any{
				"url": "/artifact", "filename": "schemo.exe",
				"digestAlgo": "sha512", "digest": base64.StdEncoding.EncodeToString(digest[:]),
				"signatureAlgo": "ed25519ph", "signature": base64.StdEncoding.EncodeToString(signature),
			}
			if mode == "unsigned" || mode == "digest-only" {
				delete(artifact, "signature")
				delete(artifact, "signatureAlgo")
			}
			if mode == "unsigned" {
				delete(artifact, "digest")
				delete(artifact, "digestAlgo")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/artifact" {
					if mode == "tampered" {
						_, _ = w.Write([]byte("substituted artifact"))
						return
					}
					_, _ = w.Write(payload)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"schemaVersion": 1, "version": "1.1.0", "channel": "stable",
					"artifacts": []any{artifact},
				})
			}))
			t.Cleanup(server.Close)
			source, err := newSource(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			key := public
			if mode == "wrong-key" {
				key, _, err = ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
			}
			updater := wailsupdater.New(testHost{})
			if err := updater.Init(wailsupdater.Config{
				CurrentVersion: "1.0.0", Providers: []wailsupdater.Provider{source},
				PublicKey: key, Window: wailsupdater.WindowNone,
			}); err != nil {
				t.Fatal(err)
			}
			_, err = updater.Check(t.Context())
			if mode == "unsigned" || mode == "digest-only" {
				if err == nil {
					t.Fatal("unsigned release accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			err = updater.DownloadAndInstall(t.Context())
			if mode != "signed" {
				if err == nil || updater.DownloadedPath() != "" {
					t.Fatal("untrusted artifact staged")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			path := updater.DownloadedPath()
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(payload) {
				t.Fatalf("staged artifact = %q, %v", got, err)
			}
		})
	}
}

func TestSourceUsesSelectedChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		channel := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "1.1.0", "channel": channel,
			"artifacts": []any{map[string]any{"url": "/artifact", "signature": "AQ==", "signatureAlgo": "ed25519ph"}},
		})
	}))
	t.Cleanup(server.Close)
	source, err := newSource(server.URL + "/{{channel}}.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range []domain.UpdateChannel{domain.ChannelStable, domain.ChannelBeta, domain.ChannelStable} {
		if err := source.use(channel); err != nil {
			t.Fatal(err)
		}
		release, err := source.Check(context.Background(), wailsupdater.CheckRequest{CurrentVersion: "1.0.0"})
		if err != nil || release == nil || release.Channel != string(channel) {
			t.Fatalf("channel %q: release = %+v, error = %v", channel, release, err)
		}
	}
	if err := source.use("preview"); err == nil {
		t.Fatal("unknown channel silently accepted")
	}
}
