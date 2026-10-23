package app

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/desktop"
)

func TestUnavailableStorageSkipsDataStartup(t *testing.T) {
	status := desktop.NewStatus()
	status.Fail(desktop.FailureDatabase, "storage unavailable")
	start, _ := newStartup(startupDeps{Status: status})
	if err := start(context.Background()); err != nil {
		t.Fatalf("startup with unavailable storage = %v", err)
	}
	if status.Ready() {
		t.Fatal("failed storage became ready")
	}
}

func TestRetryRunsStartupOnceAfterStorageRecovers(t *testing.T) {
	status := desktop.NewStatus()
	status.Fail(desktop.FailureDatabase, "storage unavailable")
	opened, started := 0, 0
	service := System{
		status: status,
		init:   func(context.Context) error { opened++; status.Succeed(); return nil },
		start:  func(context.Context) error { started++; return nil },
	}
	for range 2 {
		current, err := service.RetryInit()
		if err != nil || !current.Ready {
			t.Fatalf("RetryInit = %+v, %v", current, err)
		}
	}
	if opened != 1 || started != 1 {
		t.Fatalf("retry opened storage %d times and started data %d times; want once each", opened, started)
	}
}

func TestUnavailableStorageUsesDefaultPreferences(t *testing.T) {
	status := desktop.NewStatus()
	status.Fail(desktop.FailureDatabase, "storage unavailable")
	current := startupPreferences(context.Background(), status, nil)
	if current.Theme == "" || current.Language == "" || current.Appearance.MonoSize == 0 {
		t.Fatalf("startup preferences without storage = %+v", current)
	}
}

func TestRetryRemainsAvailableAfterDataStartupFails(t *testing.T) {
	status := desktop.NewStatus()
	status.Fail(desktop.FailureDatabase, "storage unavailable")
	want := errors.New("recovery failed")
	attempts := 0
	service := System{
		status: status,
		init:   func(context.Context) error { status.Succeed(); return nil },
		start: func(context.Context) error {
			attempts++
			if attempts == 1 {
				return want
			}
			return nil
		},
	}
	current, err := service.RetryInit()
	if !errors.Is(err, want) || current.Ready {
		t.Fatalf("failed RetryInit = %+v, %v", current, err)
	}
	current, err = service.RetryInit()
	if err != nil || !current.Ready || attempts != 2 {
		t.Fatalf("repeated RetryInit = %+v, %v; startup attempts = %d", current, err, attempts)
	}
}
