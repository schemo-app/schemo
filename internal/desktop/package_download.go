package desktop

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
)

type packageDownload struct {
	source wailsupdater.Provider
	key    []byte
	emit   func(string, ...any)
}

// Wails stages beside the executable for its binary swap. PackageKit does its own installation,
// so Debian packages must instead be downloaded somewhere writable by the current user.
func (d *packageDownload) download(ctx context.Context, release *wailsupdater.Release) (string, error) {
	verification := release.Verification
	if filepath.Ext(release.Artifact.Filename) != ".deb" {
		return "", errors.New("updater: expected a signed Debian package")
	}
	if verification == nil || verification.SignatureAlgo != "ed25519ph" || len(verification.Signature) == 0 {
		return "", errors.New("updater: package requires an ed25519ph signature")
	}
	if len(verification.Digest) > 0 && verification.DigestAlgo != "sha512" {
		return "", errors.New("updater: package requires a sha512 digest")
	}
	key, err := packagePublicKey(d.key)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "schemo-update-*")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "release.deb")
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
			_ = os.Remove(dir)
		}
	}()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()

	d.publish(wailsupdater.EventDownloadStarted, release)
	digest := sha512.New()
	started := time.Now()
	var lastProgress time.Time
	progress := func(written, total int64) {
		if time.Since(lastProgress) < 100*time.Millisecond && written != total {
			return
		}
		lastProgress = time.Now()
		d.publish(wailsupdater.EventDownloadProgress, wailsupdater.Progress{
			Written: written, Total: total, Rate: float64(written) / time.Since(started).Seconds(),
			Provider: release.Provider,
		})
	}
	if err := d.source.Download(ctx, release, io.MultiWriter(file, digest), progress); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	d.publish(wailsupdater.EventDownloadComplete, release)
	d.publish(wailsupdater.EventVerifying, release)
	sum := digest.Sum(nil)
	if len(verification.Digest) > 0 && subtle.ConstantTimeCompare(sum, verification.Digest) != 1 {
		return "", errors.New("updater: package digest mismatch")
	}
	if err := ed25519.VerifyWithOptions(key, sum, verification.Signature, &ed25519.Options{Hash: crypto.SHA512}); err != nil {
		return "", fmt.Errorf("updater: package signature: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	complete = true
	return path, nil
}

func (d *packageDownload) publish(name string, data ...any) {
	if d.emit != nil {
		d.emit(name, data...)
	}
}

func packagePublicKey(raw []byte) (ed25519.PublicKey, error) {
	if len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if block, _ := pem.Decode(raw); block != nil {
		raw = block.Bytes
	}
	value, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("updater: package public key: %w", err)
	}
	key, ok := value.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("updater: package public key must be ed25519")
	}
	return key, nil
}
