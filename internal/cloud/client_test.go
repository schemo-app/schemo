package cloud

import (
	"testing"

	commonv1 "github.com/schemo-app/proto/go/common/v1"
	workspacev1 "github.com/schemo-app/proto/go/workspace/v1"

	"schemo/internal/domain"
)

// What the account says about a place in a workspace is kept as the acts the window draws a control
// for, and the one predicate this app acts on is read out of the permission words here, where the
// server's vocabulary is known. A window handed the words would hold a second opinion about who
// may write.
func TestWhatTheAccountSaysBecomesTheWindowsReading(t *testing.T) {
	row := workspacev1.Workspace_builder{
		Role:        commonv1.RoleRef_builder{Name: "editor"}.Build(),
		Permissions: []string{"workspace:read", "workspace:write"},
		Can:         workspacev1.WorkspaceCan_builder{Update: false, Delete: false}.Build(),
	}.Build()

	got := accessOf(row)

	want := domain.WorkspaceAccess{RoleName: "editor", Write: true}
	if got != want {
		t.Errorf("access = %+v, want %+v: the acts the account refused stay refused", got, want)
	}
}

func TestARoleWithoutTheWordThatWritesIsAReaders(t *testing.T) {
	row := workspacev1.Workspace_builder{
		Role: commonv1.RoleRef_builder{Name: "viewer"}.Build(), Permissions: []string{"workspace:read"},
	}.Build()

	if got := accessOf(row); got.Write {
		t.Errorf("access = %+v, want a role without workspace:write to be kept from writing", got)
	}
}

func TestMissingPermissionsGrantNoAccess(t *testing.T) {
	got := accessOf(workspacev1.Workspace_builder{}.Build())
	if got != (domain.WorkspaceAccess{}) {
		t.Errorf("access = %+v, want no permissions", got)
	}
}
