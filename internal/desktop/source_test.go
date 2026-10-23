package desktop

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPackageUpdatesVerifyBeforeInstallationAndRestartOnlyAfterSuccess(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed Debian package")
	digest := sha512.Sum512(payload)
	signature, err := private.Sign(rand.Reader, digest[:], &ed25519.Options{Hash: crypto.SHA512})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"signed", "tampered", "installation-denied", "wrong-key", "unsigned", "wrong-algorithm", "wrong-digest", "truncated", "archive"} {
		t.Run(mode, func(t *testing.T) {
			staging := t.TempDir()
			for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
				t.Setenv(name, staging)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/artifact" {
					if mode == "truncated" {
						w.Header().Set("Content-Length", "1000")
					}
					if mode == "tampered" {
						_, _ = w.Write([]byte("substituted package"))
					} else {
						_, _ = w.Write(payload)
					}
					return
				}
				artifact := map[string]any{
					"url": "/artifact", "filename": "release.deb",
					"signatureAlgo": "ed25519ph", "signature": base64.StdEncoding.EncodeToString(signature),
					"digestAlgo": "sha512", "digest": base64.StdEncoding.EncodeToString(digest[:]),
				}
				switch mode {
				case "unsigned":
					delete(artifact, "signature")
				case "wrong-algorithm":
					artifact["signatureAlgo"] = "ed25519"
				case "wrong-digest":
					artifact["digest"] = base64.StdEncoding.EncodeToString(make([]byte, sha512.Size))
				case "archive":
					artifact["filename"] = "release.tar.gz"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"version": "1.1.0", "channel": "stable", "artifacts": []any{artifact},
				})
			}))
			t.Cleanup(server.Close)
			source, err := newSource(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			updater := wailsupdater.New(testHost{})
			if err := updater.Init(wailsupdater.Config{
				CurrentVersion: "1.0.0", Providers: []wailsupdater.Provider{source}, PublicKey: public,
			}); err != nil {
				t.Fatal(err)
			}
			key := public
			if mode == "wrong-key" {
				key, _, err = ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
			}
			der, err := x509.MarshalPKIXPublicKey(key)
			if err != nil {
				t.Fatal(err)
			}
			installed, restarted := false, false
			engine := &Updater{updater: updater, source: source,
				packageDownload: &packageDownload{source: source, key: pem.EncodeToMemory(&pem.Block{
					Type: "PUBLIC KEY", Bytes: der,
				})},
				installPackage: func(_ context.Context, path string) error {
					if filepath.Dir(filepath.Dir(path)) != staging {
						t.Errorf("package staged in %q, want user temporary directory %q", path, staging)
					}
					data, err := os.ReadFile(path)
					if err != nil || string(data) != string(payload) {
						t.Errorf("package bytes = %q, %v", data, err)
					}
					installed = true
					if mode == "installation-denied" {
						return errors.New("authorization denied")
					}
					return nil
				},
				restart: func() error { restarted = true; return nil },
			}
			err = engine.Install(t.Context(), domain.ChannelStable, "1.1.0")
			wantInstalled := mode == "signed" || mode == "installation-denied"
			if (err != nil) != (mode != "signed") || installed != wantInstalled || restarted != (mode == "signed") {
				t.Errorf("Install(%s): installed=%v, restarted=%v, error=%v", mode, installed, restarted, err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil || len(entries) != 0 {
				t.Errorf("package staging after Install(%s) = %v, %v; want empty", mode, entries, err)
			}
		})
	}
}

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
