package server

import (
	"testing"

	v1 "github.com/schemo-app/proto/go/workspace/v1"

	"schemo/internal/domain"
)

// What the account says about a place in a space is kept as the acts the window draws a control
// for, and the one predicate this app acts on is read out of the permission words here, where the
// server's vocabulary is known. A window handed the words would hold a second opinion about who
// may write.
func TestWhatTheAccountSaysBecomesTheWindowsReading(t *testing.T) {
	row := v1.Workspace_builder{
		RoleName:    "editor",
		Permissions: []string{"workspace:read", "workspace:write"},
		Can:         v1.WorkspaceCan_builder{Update: false, Delete: false}.Build(),
	}.Build()

	got := accessOf(row)

	want := domain.WorkspaceAccess{RoleName: "editor", Write: true}
	if got != want {
		t.Errorf("access = %+v, want %+v: the acts the account refused stay refused", got, want)
	}
}

func TestARoleWithoutTheWordThatWritesIsAReaders(t *testing.T) {
	row := v1.Workspace_builder{
		RoleName: "viewer", Permissions: []string{"workspace:read"},
	}.Build()

	if got := accessOf(row); got.Write {
		t.Errorf("access = %+v, want a role without workspace:write to be kept from writing", got)
	}
}

// A row that carries no permission words at all is one this build cannot read, and it is read as
// the person's own: every member has a role and every role has permissions, so an empty list is a
// server this app does not understand rather than a person who may do nothing. A device that
// withheld its own work on a reading it could not read would quietly stop syncing.
func TestARowWithoutPermissionWordsIsReadAsTheirOwn(t *testing.T) {
	got := accessOf(v1.Workspace_builder{}.Build())

	if got != domain.OwnAccess() {
		t.Errorf("access = %+v, want a server this build cannot read taken as the person's own", got)
	}
}
