package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"schemo/internal/cloud"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// device is the assembled desktop services without a window: a temporary data folder, a migrated
// database, an in-memory keyring and a browser that approves every sign-in. Scenario tests drive
// the same services the window binds.
type device struct {
	*Services
	keyring *memoryKeyring
	events  *published
	t       testing.TB
}

// newDevice starts a device with an empty, migrated database.
func newDevice(t testing.TB) *device {
	t.Helper()
	keyring, events := &memoryKeyring{values: map[string]string{}}, &published{}
	s, err := Assemble(Config{
		Dir:      platform.DataDir(t.TempDir()),
		Info:     platform.NewBuildInfo(),
		Host:     desktop.NewHost(),
		Keyring:  keyring,
		Browser:  browser{},
		Notifier: events,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := s.Store.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Sync.Stop(stop)
		s.Account.Close()
		_ = s.Store.Close()
	})
	if err := s.Workspaces.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return &device{Services: s, keyring: keyring, events: events, t: t}
}

// signIn signs the device in to the cloud as user, through the same device flow a person uses.
func (d *device) signIn(cloud *fakeCloud, user string) {
	d.t.Helper()
	cloud.signInAs(user)
	if _, err := d.Account.Begin(d.t.Context(), cloud.address); err != nil {
		d.t.Fatal(err)
	}
	d.eventually("signed in", func() bool {
		status, err := d.Account.Status(d.t.Context())
		if err != nil {
			d.t.Fatal(err)
		}
		if f := d.events.lastFailure(); f != nil {
			d.t.Fatalf("sign-in failed: %+v", f)
		}
		return status.SignedIn && !status.Waiting
	})
}

// sync runs one full round and fails the test on an error.
func (d *device) sync() {
	d.t.Helper()
	status, err := d.Services.Sync.RunRound(d.t.Context())
	if err != nil {
		d.t.Fatal(err)
	}
	if status.Failure != nil {
		d.t.Fatalf("sync: %+v", status.Failure)
	}
}

// eventually waits for a condition that a background job makes true.
func (d *device) eventually(what string, done func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			d.t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// active is the workspace on screen.
func (d *device) active() string {
	d.t.Helper()
	state, err := d.Workspaces.Snapshot(d.t.Context())
	if err != nil {
		d.t.Fatal(err)
	}
	return state.ActiveID
}

// memoryKeyring is the OS credential store kept in memory.
type memoryKeyring struct {
	mu     sync.Mutex
	values map[string]string
}

func (k *memoryKeyring) get(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

func (k *memoryKeyring) set(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.values[key] = value
	return nil
}

func (k *memoryKeyring) del(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.values, key)
	return nil
}

// len is how many entries the keyring holds.
func (k *memoryKeyring) len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.values)
}

func accountKey(a domain.Account) string { return domain.AccountKey(a.Server, a.UserID) }

func (k *memoryKeyring) Get(a domain.Account) (string, error)     { return k.get(accountKey(a)) }
func (k *memoryKeyring) Set(a domain.Account, token string) error { return k.set(accountKey(a), token) }
func (k *memoryKeyring) Delete(a domain.Account) error            { return k.del(accountKey(a)) }

func (k *memoryKeyring) PublicationPassword(a domain.Account, id string) (string, error) {
	return k.get(accountKey(a) + ":publication:" + id)
}

func (k *memoryKeyring) SetPublicationPassword(a domain.Account, id, password string) error {
	return k.set(accountKey(a)+":publication:"+id, password)
}

func (k *memoryKeyring) DeletePublicationPassword(a domain.Account, id string) error {
	return k.del(accountKey(a) + ":publication:" + id)
}

// browser approves every sign-in: the fake cloud already knows who is signing in.
type browser struct{}

func (browser) OpenURL(string) error { return nil }

var _ cloud.Browser = browser{}

// published records what the services told the windows.
type published struct {
	mu   sync.Mutex
	list []event
}

// event is one published topic and its payload.
type event struct {
	Topic   string
	Payload any
}

func (e *published) Publish(topic string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, event{Topic: topic, Payload: payload})
}

func (e *published) lastFailure() *domain.Failure {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.list) - 1; i >= 0; i-- {
		if status, ok := e.list[i].Payload.(cloud.AccountStatus); ok {
			return status.Failure
		}
	}
	return nil
}

// last is the payload of the latest event of a topic, nil when there was none.
func (e *published) last(topic string) any {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.list) - 1; i >= 0; i-- {
		if e.list[i].Topic == topic {
			return e.list[i].Payload
		}
	}
	return nil
}

// count is how many events of a topic were published.
func (e *published) count(topic string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, one := range e.list {
		if one.Topic == topic {
			n++
		}
	}
	return n
}
