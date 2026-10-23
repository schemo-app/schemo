package desktop

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const testTransaction dbus.ObjectPath = "/org/freedesktop/PackageKit/transactions/test"

type fakePackageKit struct{}

func (fakePackageKit) CreateTransaction() (dbus.ObjectPath, *dbus.Error) {
	return testTransaction, nil
}

type fakePackageTransaction struct {
	conn     *dbus.Conn
	failure  bool
	hold     bool
	hints    []string
	content  string
	started  chan struct{}
	canceled chan struct{}
}

func (f *fakePackageTransaction) SetHints(hints []string) *dbus.Error {
	f.hints = hints
	return nil
}

func (f *fakePackageTransaction) Cancel() *dbus.Error {
	close(f.canceled)
	return nil
}

func (f *fakePackageTransaction) InstallFiles(flags uint64, files []string) *dbus.Error {
	if flags != 0 || len(files) != 1 {
		return dbus.NewError("test.invalidArguments", nil)
	}
	file, err := os.Open(files[0])
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	f.content = string(data)
	close(f.started)
	if f.hold {
		return nil
	}
	code := uint32(1)
	if f.failure {
		code = 2
		_ = f.conn.Emit(testTransaction, packageKitTransaction+".ErrorCode", uint32(2), "Authorization denied")
	}
	if err := f.conn.Emit(testTransaction, packageKitTransaction+".Finished", code, uint32(0)); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func packageBus(t *testing.T) *fakePackageTransaction {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for PackageKit integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, daemon, "--session", "--nofork", "--print-address=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	address = strings.TrimSpace(address)
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", address)
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.RequestName(packageKitService, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(fakePackageKit{}, "/org/freedesktop/PackageKit", packageKitService); err != nil {
		t.Fatal(err)
	}
	transaction := &fakePackageTransaction{conn: conn, started: make(chan struct{}), canceled: make(chan struct{})}
	if err := conn.Export(transaction, testTransaction, packageKitTransaction); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func TestPackageKitInstallsWithAuthorizationAndWaitsForResult(t *testing.T) {
	for _, mode := range []string{"paths", "denied"} {
		t.Run(mode, func(t *testing.T) {
			transaction := packageBus(t)
			transaction.failure = mode == "denied"
			path := filepath.Join(t.TempDir(), "release.deb")
			if err := os.WriteFile(path, []byte("verified package"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := installDebPackage(ctx, path)
			if mode == "denied" {
				if err == nil || !strings.Contains(err.Error(), "Authorization denied") {
					t.Fatalf("installDebPackage error = %v, want authorization denial", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			<-transaction.started
			if transaction.content != "verified package" || !slices.Contains(transaction.hints, "interactive=true") {
				t.Errorf("installation content = %q, hints = %v", transaction.content, transaction.hints)
			}
		})
	}
}

func TestPackageKitCancelsAnUnfinishedTransaction(t *testing.T) {
	transaction := packageBus(t)
	transaction.hold = true
	path := filepath.Join(t.TempDir(), "release.deb")
	if err := os.WriteFile(path, []byte("verified package"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- installDebPackage(ctx, path) }()
	select {
	case <-transaction.started:
	case <-time.After(5 * time.Second):
		t.Fatal("package installation did not start")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("installDebPackage error = %v, want context cancellation", err)
	}
	select {
	case <-transaction.canceled:
	default:
		t.Error("PackageKit transaction was not canceled")
	}
}

func TestPackageKitRejectsNonPackageArtifacts(t *testing.T) {
	if err := installDebPackage(t.Context(), "release.tar.gz"); err == nil {
		t.Fatal("installDebPackage accepted a non-package artifact")
	}
}
