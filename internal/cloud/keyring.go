package cloud

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Keyring scopes credentials to an installation and its account session.
type Keyring struct{ installation string }

// NewKeyring creates a store without accessing the system keyring.
func NewKeyring(dir platform.DataDir) *Keyring {
	return &Keyring{installation: filepath.Clean(string(dir))}
}

// Get reads a token from the OS credential store.
func (s *Keyring) Get(account domain.Account) (string, error) {
	token, err := keyring.Get(platform.KeyringService, s.key(account))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", domain.ErrNotFound
	}
	return token, err
}

// Set saves a token without a plaintext fallback.
func (s *Keyring) Set(account domain.Account, token string) error {
	return keyring.Set(platform.KeyringService, s.key(account), token)
}

// Delete removes a session credential, tolerating an absent entry.
func (s *Keyring) Delete(account domain.Account) error {
	err := keyring.Delete(platform.KeyringService, s.key(account))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Keyring) key(account domain.Account) string {
	address := account.Server
	if address == "" {
		address = platform.DefaultServer
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.installation+"\x00"+address+"\x00"+account.UserID+"\x00"+account.SessionID)))
}

func (s *Keyring) publicationKey(account domain.Account, id string) string {
	address := account.Server
	if address == "" {
		address = platform.DefaultServer
	}
	return fmt.Sprintf("publication-%x", sha256.Sum256([]byte(s.installation+"\x00"+address+"\x00"+account.UserID+"\x00"+id)))
}

// PublicationPassword reads a password scoped to this installation, server, account and page.
func (s *Keyring) PublicationPassword(account domain.Account, id string) (string, error) {
	password, err := keyring.Get(platform.KeyringService, s.publicationKey(account, id))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", domain.ErrNotFound
	}
	return password, err
}

// SetPublicationPassword saves a publication password without a plaintext fallback.
func (s *Keyring) SetPublicationPassword(account domain.Account, id, password string) error {
	return keyring.Set(platform.KeyringService, s.publicationKey(account, id), password)
}

// DeletePublicationPassword removes a saved password, tolerating an absent entry.
func (s *Keyring) DeletePublicationPassword(account domain.Account, id string) error {
	err := keyring.Delete(platform.KeyringService, s.publicationKey(account, id))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
