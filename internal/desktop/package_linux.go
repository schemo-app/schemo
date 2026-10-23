package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"schemo/internal/platform"
)

const (
	packageKitService     = "org.freedesktop.PackageKit"
	packageKitTransaction = packageKitService + ".Transaction"
)

func packageInstalled(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "--search", target)
	query.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	output, err := query.Output()
	return err == nil && strings.TrimSpace(string(output)) == platform.Slug+": "+target
}

func installDebPackage(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if filepath.Ext(path) != ".deb" {
		return errors.New("updater: expected a signed Debian package")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("updater: connect to PackageKit: %w", err)
	}
	defer conn.Close()
	var transaction dbus.ObjectPath
	if err := conn.Object(packageKitService, "/org/freedesktop/PackageKit").CallWithContext(
		ctx, packageKitService+".CreateTransaction", 0,
	).Store(&transaction); err != nil {
		return fmt.Errorf("updater: create package transaction: %w", err)
	}
	var owner string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, packageKitService).Store(&owner); err != nil {
		return err
	}
	match := []dbus.MatchOption{
		dbus.WithMatchSender(owner),
		dbus.WithMatchObjectPath(transaction),
		dbus.WithMatchInterface(packageKitTransaction),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer conn.RemoveMatchSignal(match...)
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	object := conn.Object(packageKitService, transaction)
	if err := object.CallWithContext(ctx, packageKitTransaction+".SetHints", 0,
		[]string{"interactive=true", "background=false"}).Err; err != nil {
		return err
	}
	defer func() {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = object.CallWithContext(cancelCtx, packageKitTransaction+".Cancel", 0).Err
		}
	}()
	err = object.CallWithContext(ctx, packageKitTransaction+".InstallFiles", dbus.FlagAllowInteractiveAuthorization,
		uint64(0), []dbus.UnixFD{dbus.UnixFD(file.Fd())}).Err
	var busError dbus.Error
	if errors.As(err, &busError) && busError.Name == "org.freedesktop.DBus.Error.InvalidArgs" {
		err = object.CallWithContext(ctx, packageKitTransaction+".InstallFiles", dbus.FlagAllowInteractiveAuthorization,
			uint64(0), []string{path}).Err
	}
	if err != nil {
		return fmt.Errorf("updater: install package: %w", err)
	}
	var failure error
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return errors.New("updater: PackageKit disconnected before installation completed")
			}
			if signal.Sender != owner || signal.Path != transaction {
				continue
			}
			switch signal.Name {
			case packageKitTransaction + ".ErrorCode":
				var code uint32
				var message string
				if err := dbus.Store(signal.Body, &code, &message); err != nil {
					return err
				}
				failure = fmt.Errorf("updater: PackageKit error %d: %s", code, message)
			case packageKitTransaction + ".Finished":
				var code, duration uint32
				if err := dbus.Store(signal.Body, &code, &duration); err != nil {
					return err
				}
				if failure != nil {
					return failure
				}
				if code != 1 {
					return fmt.Errorf("updater: package installation finished with status %d", code)
				}
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
