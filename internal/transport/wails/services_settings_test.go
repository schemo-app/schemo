package wails

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/infra/files"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/settings"
)

// dialog is the file dialog as a test answers it: a path, or nothing for a dialog that was closed.
type dialog struct {
	path string
	err  error
}

func (d dialog) PickRootCertificate(string) (string, error) { return d.path, d.err }

// nothing is the settings' own side of a change: this service writes rows and hands the network to
// the engine, and neither is what these tests are about.
type nothing struct{}

func (nothing) Apply(domain.Network) {}

func settingsService(t *testing.T, picked dialog) (*SettingsService, context.Context) {
	t.Helper()

	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	ctx := context.Background()
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatalf("desktop.OpenStorage: %v", err)
	}

	return &SettingsService{
		settings:     settings.NewUseCase(store, &heard{}, nothing{}),
		files:        files.NewReader(),
		certificates: picked,
	}, ctx
}

// A dialog that was closed is not a change. The window takes the answer whole — it draws what came
// back — so an empty one would be painted over every field the pane holds, which is what happened
// before this test existed: cancelling the file dialog wiped the settings on screen.
func TestAClosedCertificateDialogChangesNothing(t *testing.T) {
	svc, ctx := settingsService(t, dialog{})

	before, err := svc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	after, err := svc.AddRootCertificate(ctx, "Roots")
	if err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("settings = %+v, want the ones that were already there: %+v", after, before)
	}
}

// A file that is not a certificate is refused, and the refusal travels: a trust meant to be added
// and missing is worst of both, so the window has to be able to say so.
func TestAFileThatIsNotACertificateIsRefused(t *testing.T) {
	svc, ctx := settingsService(t, dialog{})

	path := filepath.Join(t.TempDir(), "notes.pem")
	if err := os.WriteFile(path, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	svc.certificates = dialog{path: path}

	_, err := svc.AddRootCertificate(ctx, "Roots")
	if domain.CodeOf(err) != domain.CodeBadCertificate {
		t.Errorf("AddRootCertificate = %v, want a refusal the window can word", err)
	}
}
