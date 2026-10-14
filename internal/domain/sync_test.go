package domain

import "testing"

// The order of a batch is not a preference: the server refuses a row whose parent has not arrived,
// and a queue that put a request before the collection it is in would never drain. The order is
// made of the depth of each kind, so the one thing to check is that a parent sits above its child —
// a kind added later is added here deliberately.
func TestAParentIsPushedBeforeItsChild(t *testing.T) {
	parents := []struct{ parent, child EntityType }{
		{EntityWorkspace, EntityCollection},
		{EntityCollection, EntityRequest},
		{EntityCollection, EntityVariable},
		{EntityEnvironment, EntityVariable},
	}

	for _, one := range parents {
		if Depth(one.parent) >= Depth(one.child) {
			t.Errorf("%s stands at %d and %s at %d, want the parent pushed first",
				one.parent, Depth(one.parent), one.child, Depth(one.child))
		}
	}
}

// A kind that is not in the list — history's record, which travels through another door — has no
// place in the order, and it says so by sitting below every kind that has one rather than by
// answering whatever a map happens to hold.
func TestAKindThatDoesNotTravelThroughAPushHasNoDepth(t *testing.T) {
	if Depth(EntityRecord) < len(EntityTypes) {
		t.Errorf("Depth(%s) = %d, want it below every kind that is pushed",
			EntityRecord, Depth(EntityRecord))
	}
}
