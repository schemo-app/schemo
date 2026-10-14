package sqlite

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

// placeAt is the whole of what an index means, so it is tested on its own rather than through a
// database: the level is a few rows, and every one of them is a case.
func TestPlaceAtCountsTheLevelAsItLooks(t *testing.T) {
	at := func(ids ...string) []child {
		out := make([]child, 0, len(ids))
		for _, id := range ids {
			out = append(out, child{id: id})
		}
		return out
	}
	ids := func(level []child) []string {
		out := make([]string, 0, len(level))
		for _, one := range level {
			out = append(out, one.id)
		}
		return out
	}

	tests := []struct {
		name  string
		level []child
		moved string
		at    int64
		want  []string
	}{
		{
			// The row stood first and is dropped after its neighbour: the index counts it, and it must
			// not be counted twice — this is the case that lands one place too far without the discount.
			name: "a row dropped after a row that was behind it", level: at("a", "b", "c"),
			moved: "a", at: 2, want: []string{"b", "a", "c"},
		},
		{
			name: "a row dropped after the row in front of it", level: at("a", "b", "c"),
			moved: "c", at: 1, want: []string{"a", "c", "b"},
		},
		{
			name: "a row dropped before itself changes nothing", level: at("a", "b", "c"),
			moved: "b", at: 1, want: []string{"a", "b", "c"},
		},
		{
			name: "the front of the level", level: at("a", "b", "c"),
			moved: "c", at: 0, want: []string{"c", "a", "b"},
		},
		{
			name: "the end of the level", level: at("a", "b", "c"),
			moved: "a", at: 3, want: []string{"b", "c", "a"},
		},
		{
			name: "past the end lands at the end", level: at("a", "b"),
			moved: "a", at: 9, want: []string{"b", "a"},
		},
		{
			name: "into another level, where the row was not counted", level: at("a", "b", "c"),
			moved: "x", at: 2, want: []string{"a", "b", "x", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(placeAt(tt.level, child{id: tt.moved}, tt.at))
			if len(got) != len(tt.want) {
				t.Fatalf("level = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("level = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestMoveNodeLandsWhereTheIndexPoints is the same rule through the store, where the level is rows
// of two tables and the numbers are written rather than returned.
func TestMoveNodeLandsWhereTheIndexPoints(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	for _, node := range []domain.CollectionNode{
		sampleNode("r-3", "col-1", 2, "Три", "GET", "https://api.example.com/3"),
	} {
		if err := store.SaveNode(ctx, node); err != nil {
			t.Fatalf("SaveNode: %v", err)
		}
	}
	// The level is f-1 (0), r-2 (1), r-3 (2); r-2 is dropped at 3, and the index counts the
	// moving row.
	if err := store.MoveNode(ctx, ws, "r-2", "col-1", 3); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	order := []string{}
	for _, entry := range tree[0].Level() {
		if entry.Collection != nil {
			order = append(order, entry.Collection.ID)
			continue
		}
		order = append(order, entry.Node.ID)
	}
	want := []string{"f-1", "r-3", "r-2"}
	if len(order) != len(want) {
		t.Fatalf("level = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("level = %v, want %v", order, want)
		}
	}
}
