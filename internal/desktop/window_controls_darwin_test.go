//go:build darwin

package desktop

import (
	"math"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppKit must run on the process's main thread, so exercise the native layout in its own process.
func TestWindowControlsStayCenteredAfterNativeLayout(t *testing.T) {
	t.Parallel()
	binary := filepath.Join(t.TempDir(), "window-controls")
	build := exec.CommandContext(t.Context(), "clang", "-x", "objective-c", "-framework", "Cocoa",
		"-mmacosx-version-min=12.0", "-I.", "testdata/window_controls.m", "window_controls_darwin.m", "-o", binary)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building native window-controls test: %v\n%s", err, output)
	}
	output, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
	if err != nil {
		t.Fatalf("native window-controls regression: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}

func TestHeaderHeightBeforeRuntimeReadyDoesNotDispatchNativeLayout(t *testing.T) {
	host := NewHost()
	host.SetMainWindow(&application.WebviewWindow{})
	if err := host.SetHeaderHeight(43.75); err == nil {
		t.Fatal("SetHeaderHeight before runtime ready accepted a native layout update")
	}
}

func TestHeaderHeightRejectsInvalidMeasurements(t *testing.T) {
	t.Parallel()
	host := NewHost()
	for _, height := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := host.SetHeaderHeight(height); err == nil {
			t.Errorf("SetHeaderHeight(%v) accepted an invalid measurement", height)
		}
	}
}
