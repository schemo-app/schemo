// Package credentials stores account tokens and publication passwords in the OS credential store.
package credentials

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Store scopes credentials to an installation and its account session.
type Store struct{ installation string }

// New creates a store without accessing the system keyring.
func New(dir platform.DataDir) *Store {
	return &Store{installation: filepath.Clean(string(dir))}
}

// Get reads a token from the OS credential store.
func (s *Store) Get(account domain.Account) (string, error) {
	token, err := keyring.Get(platform.DataDirName, s.key(account))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", domain.ErrNotFound
	}
	return token, err
}

// Set saves a token without a plaintext fallback.
func (s *Store) Set(account domain.Account, token string) error {
	return keyring.Set(platform.DataDirName, s.key(account), token)
}

// Delete removes a session credential, tolerating an absent entry.
func (s *Store) Delete(account domain.Account) error {
	err := keyring.Delete(platform.DataDirName, s.key(account))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Store) key(account domain.Account) string {
	address := account.Server
	if address == "" {
		address = platform.DefaultServer
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.installation+"\x00"+address+"\x00"+account.UserID+"\x00"+account.SessionID)))
}

func (s *Store) publicationKey(account domain.Account, id string) string {
	address := account.Server
	if address == "" {
		address = platform.DefaultServer
	}
	return fmt.Sprintf("publication-%x", sha256.Sum256([]byte(s.installation+"\x00"+address+"\x00"+account.UserID+"\x00"+id)))
}

// PublicationPassword reads a password scoped to this installation, server, account and page.
func (s *Store) PublicationPassword(account domain.Account, id string) (string, error) {
	password, err := keyring.Get(platform.DataDirName, s.publicationKey(account, id))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", domain.ErrNotFound
	}
	return password, err
}

// SetPublicationPassword saves a publication password without a plaintext fallback.
func (s *Store) SetPublicationPassword(account domain.Account, id, password string) error {
	return keyring.Set(platform.DataDirName, s.publicationKey(account, id), password)
}

// DeletePublicationPassword removes a saved password, tolerating an absent entry.
func (s *Store) DeletePublicationPassword(account domain.Account, id string) error {
	err := keyring.Delete(platform.DataDirName, s.publicationKey(account, id))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
