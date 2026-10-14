package server

import (
	"testing"

	"github.com/schemo-app/proto/go/subscription/v1"

	"schemo/internal/domain"
)

// The caps the server sends and what it counts against them become the bars a window draws, in the
// kinds this app knows — and only where something is capped: a quota the server leaves unlimited
// is a number without a meaning, and one nothing is counted against has no bar to stand on.
func TestTheCapsAndWhatStandsAgainstThemBecomeMeters(t *testing.T) {
	quotas := map[string]int{
		"workspace.collections":  20,
		"org.members":            1,
		"org.storage_mb":         100,
		"history.retention_days": 7,
		"org.workspaces":         -1,
	}
	used := subscription.Usage_builder{
		Collections:  18,
		Members:      1,
		Workspaces:   3,
		StorageBytes: 12 * megabyte,
	}.Build()

	meters := metersOf(quotas, used)

	want := []domain.Meter{
		{Kind: "collections", Used: 18, Limit: 20},
		{Kind: "members", Used: 1, Limit: 1},
		{Kind: "storage", Used: 12, Limit: 100},
	}
	if len(meters) != len(want) {
		t.Fatalf("meters = %+v, want %+v", meters, want)
	}
	for at, one := range meters {
		if one != want[at] {
			t.Errorf("meter %d = %+v, want %+v", at, one, want[at])
		}
	}
}

// A server that says nothing about what stands against the caps has no meters to draw, and neither
// has a plan with no caps in it at all: a bar needs both halves.
func TestMetersNeedBothTheCapAndTheCount(t *testing.T) {
	usage := subscription.Usage_builder{Collections: 18}.Build()

	if meters := metersOf(nil, usage); len(meters) != 0 {
		t.Errorf("meters = %+v, want nothing drawn from caps nobody sent", meters)
	}
	if meters := metersOf(map[string]int{"workspace.collections": 20}, nil); len(meters) != 0 {
		t.Errorf("meters = %+v, want nothing drawn from a count nobody sent", meters)
	}
}
