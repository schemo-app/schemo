package main

import (
	"testing"

	"go.uber.org/fx"

	"schemo/internal/transport/desktop"
	"schemo/internal/transport/wails"
)

// The composition root is a graph of types, and a port nothing supplies is not a compile error: it
// is a program that refuses to start, which nobody finds until they run it. ValidateApp asks if the
// graph is whole without constructing anything — no database, no window, no host — so the question
// belongs here, in the build, where a missing provider is cheap to catch and where asking it cannot
// disturb what the app is keeping.
func TestTheGraphIsWhole(t *testing.T) {
	if err := fx.ValidateApp(fx.NopLogger, wails.Module,
		fx.Supply(desktop.Assets{FS: assets, Icon: appIcon})); err != nil {
		t.Fatalf("the graph does not build: %v", err)
	}
}
