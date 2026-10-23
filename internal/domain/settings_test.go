package domain

import (
	"testing"
	"time"
)

func TestARetentionKnowsHowOldARecordMayGrow(t *testing.T) {
	cases := map[Retention]time.Duration{
		RetainWeek:    7 * 24 * time.Hour,
		RetainMonth:   30 * 24 * time.Hour,
		RetainForever: 0,
		"whatever":    0,
	}
	for retention, want := range cases {
		if got := retention.Age(); got != want {
			t.Errorf("%q.Age() = %v, want %v", retention, got, want)
		}
	}
}

// A combination is kept in one shape and one only: modifiers in the order they are written in, each
// at most once, and one key. What is stored is compared against keyboards, so a shape nobody can
// press is a command that never fires.
func TestACombinationIsWrittenInOneShape(t *testing.T) {
	good := []string{
		"mod+K", "mod+alt+S", "mod+shift+enter", "alt+I", "mod+,", "F5", "mod+F12", "mod+1",
		ShortcutNone,
	}
	for _, combo := range good {
		if !ValidShortcut(combo) {
			t.Errorf("%q was refused", combo)
		}
	}

	bad := []string{
		"", "K", ",", "mod+", "mod+mod+K", "shift+mod+K", "mod+ctrl+K", "mod+unknown", "mod+KK",
		"mod+F40", "mod+nothing+",
	}
	for _, combo := range bad {
		if ValidShortcut(combo) {
			t.Errorf("%q was accepted", combo)
		}
	}
}
