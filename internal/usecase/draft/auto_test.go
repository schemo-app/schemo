package draft

import (
	"context"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// The rows the app adds by itself, in the order the wire carries them: the address's host, the name
// the app calls itself, the compression it asks for, the jar's cookies, and the body's length.
func TestAutoIsTheRowsTheAppAdds(t *testing.T) {
	uc, _, _, _, jar := newWithJar()
	jar.header = "session=abc"
	uc.net = fakeNet{agent: "Schemo/2.4", gzip: true}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "https://api.example.com/users",
		Body: `{"name":"ok"}`, BodyKind: domain.BodyJSON,
	}
	rows, err := uc.Auto(context.Background(), draft, nil, nil)
	if err != nil {
		t.Fatalf("Auto: %v", err)
	}

	want := []domain.ProjectedRow{
		{Target: domain.RowHeaders, Name: "Host", Value: "api.example.com"},
		{Target: domain.RowHeaders, Name: "User-Agent", Value: "Schemo/2.4"},
		{Target: domain.RowHeaders, Name: "Accept-Encoding", Value: "gzip"},
		{Target: domain.RowHeaders, Name: "Cookie", Value: "session=abc"},
		{Target: domain.RowHeaders, Name: "Content-Length", Value: "13"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

// A body only a send can read — a file, or a form carrying one — is left without a length rather
// than given a guess, and nothing is opened to find out.
func TestAutoLeavesOutTheBodyOnlyASendCanRead(t *testing.T) {
	uc, _, files := newUseCaseWithFiles()
	uc.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "/upload", BodyKind: domain.BodyBinary,
		BodyFile: "/tmp/logo.png",
	}
	rows, err := uc.Auto(context.Background(), draft, nil, nil)
	if err != nil {
		t.Fatalf("Auto: %v", err)
	}
	for _, row := range rows {
		if row.Name == "Content-Length" {
			t.Errorf("rows = %+v, want no length for a body only a send can read", rows)
		}
	}
	if files.read != 0 {
		t.Errorf("files read = %d, want nothing opened for a preview", files.read)
	}
}

// A form without a file is text the way any other body is, and its length is counted as a send
// renders it.
func TestAutoCountsAFormBody(t *testing.T) {
	uc, _ := newUseCase()
	uc.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "https://api.example.com/form", BodyKind: domain.BodyForm,
		Form: []domain.FormRow{{Name: "a", Value: "1", Enabled: true}},
	}
	rows, err := uc.Auto(context.Background(), draft, nil, nil)
	if err != nil {
		t.Fatalf("Auto: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Name == "Content-Length" && row.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("rows = %+v, want a length for a form without files", rows)
	}
}

// The address is drawn as it goes out, with a secret resolved to the mask rather than to its value:
// the row says the host is answered by a variable without saying what the variable holds.
func TestAutoMasksWhatTheAddressCarries(t *testing.T) {
	uc, _, _, _, _ := newWithJar()
	uc.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "GET", URL: "https://{{token}}.example.com/x",
	}
	rows, err := uc.Auto(context.Background(), draft, nil, nil)
	if err != nil {
		t.Fatalf("Auto: %v", err)
	}
	for _, row := range rows {
		if strings.Contains(row.Value, "abc123") {
			t.Fatalf("rows = %+v, want the secret not to travel in a value", rows)
		}
		if row.Name == "Host" && row.Value != fakeMask+".example.com" {
			t.Errorf("host = %q, want the masked name", row.Value)
		}
	}
}
