package domain

import (
	"context"
	"strings"
	"time"
)

// Principal is who this device is signed in as: the token every call to the cloud carries, and who
// the person is. An account that is gone is the answer, not a question to keep asking.
type Principal interface {
	Caller(ctx context.Context) (Caller, error)
	Me(ctx context.Context) (Account, error)
}

type Account struct {
	Server     string    `json:"server"`
	UserID     string    `json:"userId"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	SessionID  string    `json:"sessionId"`
	SignedInAt time.Time `json:"signedInAt"`
}

func AccountKey(address, email string) string {
	server := strings.ToLower(strings.TrimRight(strings.TrimSpace(address), "/"))
	return server + "|" + strings.ToLower(strings.TrimSpace(email))
}

type Identity struct {
	Key    string
	UserID string
}

func (a Account) SignedIn() bool { return a.UserID != "" }

type Device struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	App        string `json:"app"`
	AppVersion string `json:"appVersion"`
}

type Session struct {
	ID         string    `json:"id"`
	Device     Device    `json:"device"`
	IP         string    `json:"ip,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	IsCurrent  bool      `json:"isCurrent"`
}

type Tokens struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
}

type Challenge struct {
	UserCode  string    `json:"userCode"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}
