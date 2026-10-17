package publication

import (
	"errors"
	"testing"

	"schemo/internal/domain"
)

type fakePasswords struct {
	value string
	err   error
}

func (f *fakePasswords) PublicationPassword(domain.Account, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.value == "" {
		return "", domain.ErrNotFound
	}
	return f.value, nil
}

func (f *fakePasswords) SetPublicationPassword(_ domain.Account, _, password string) error {
	if f.err != nil {
		return f.err
	}
	f.value = password
	return nil
}

func (f *fakePasswords) DeletePublicationPassword(domain.Account, string) error {
	if f.err != nil {
		return f.err
	}
	f.value = ""
	return nil
}

func TestPublicationPasswordsAreExplicitLocalSecrets(t *testing.T) {
	page := domain.Publication{ID: "page", CollectionID: "collection", CanManage: true,
		Status: domain.PublicationActive, Visibility: domain.VisibilityPassword}
	store := &fakeStore{held: []domain.Publication{page}}
	secrets := &fakePasswords{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.passwords = secrets
	if err := uc.RememberPassword(t.Context(), "collection", "saved-password", ""); err != nil {
		t.Fatal(err)
	}
	if password, err := uc.Password(t.Context(), "collection"); err != nil || password != "saved-password" {
		t.Fatalf("saved password unavailable: %v", err)
	}
	if store.saved != 0 {
		t.Fatal("password was written to the publication database")
	}
	if err := uc.RememberPassword(t.Context(), "collection", "replacement-password", "other-session"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("account switch allowed credential saving: %v", err)
	}
	store.held[0].CanManage = false
	if _, err := uc.Password(t.Context(), "collection"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("reader could reveal the password: %v", err)
	}
	store.held[0].CanManage = true
	store.held[0].Visibility = domain.VisibilityPublic
	if err := uc.RememberPassword(t.Context(), "collection", "", ""); err != nil {
		t.Fatal(err)
	}
	if secrets.value != "" {
		t.Fatal("removing password protection retained the credential")
	}
}

func TestUnavailablePasswordStoreHasNoDatabaseFallback(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{{ID: "page", CollectionID: "collection", CanManage: true,
		Status: domain.PublicationActive, Visibility: domain.VisibilityPassword}}}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.passwords = &fakePasswords{err: errors.New("keyring unavailable")}
	if err := uc.RememberPassword(t.Context(), "collection", "saved-password", ""); err == nil {
		t.Fatal("unavailable keyring reported a saved password")
	}
	if _, err := uc.Password(t.Context(), "collection"); err == nil {
		t.Fatal("unavailable keyring was ignored")
	}
	if store.saved != 0 {
		t.Fatal("password fell back to the database")
	}
}
