package publication

// What a page says has moved at it since it last went up.
//
// The page is told this: it draws the entries and numbers them from its own revision. What this
// device keeps for itself is the outline the last publish put up, and it is the only thing the next
// publish can be compared against — nothing else here remembers what the page looked like before.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

// The three words this device says about a request, which the page colours without translating.
const (
	signAdded   = "+"
	signChanged = "~"
	signGone    = "-"
)

// outlineOf is what a document puts up, request by request, in the order the page draws them, with
// the fingerprint of what each one says. An id is the page's own and stays the same for as long as
// the request is, which is what lets a rename read as one request changed rather than as two.
func outlineOf(snapshot snapshotv1.Snapshot) ([]domain.OutlineItem, error) {
	var out []domain.OutlineItem
	for _, group := range snapshot.Collection.Groups {
		for _, endpoint := range group.Endpoints {
			held, err := json.Marshal(endpoint)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(held)
			out = append(out, domain.OutlineItem{
				ID:          endpoint.ID,
				Method:      endpoint.Method,
				Name:        endpoint.Name,
				Fingerprint: hex.EncodeToString(sum[:])[:12],
			})
		}
	}
	return out, nil
}

// pending is what the collection has that the page does not: the document built now, read against
// the outline the last publish put up. It is the entry a publish would add to the history, and the
// same comparison the page's own record is made of.
func pending(
	held domain.Publication, snapshot snapshotv1.Snapshot,
) ([]domain.PageItem, bool, error) {
	now, err := outlineOf(snapshot)
	if err != nil {
		return nil, false, err
	}
	items, cut := moved(held.Outline, now)
	return items, cut, nil
}

// moved is what the page did not have, has otherwise and no longer has: three signs off the
// outlines. Gone requests are said last, after the page's own order, because a request that is not
// there has no place among the ones that are.
func moved(before, now []domain.OutlineItem) ([]domain.PageItem, bool) {
	had := make(map[string]domain.OutlineItem, len(before))
	for _, item := range before {
		had[item.ID] = item
	}

	there := make(map[string]bool, len(now))
	out := make([]domain.PageItem, 0, len(now))
	for _, item := range now {
		there[item.ID] = true
		was, known := had[item.ID]
		switch {
		case !known:
			out = append(out, domain.PageItem{Sign: signAdded, Method: item.Method, Name: item.Name})
		case was.Fingerprint != item.Fingerprint:
			out = append(out, domain.PageItem{Sign: signChanged, Method: item.Method, Name: item.Name})
		}
	}
	for _, item := range before {
		if !there[item.ID] {
			out = append(out, domain.PageItem{Sign: signGone, Method: item.Method, Name: item.Name})
		}
	}

	cut := len(out) > maxReleaseItems
	if cut {
		out = out[:maxReleaseItems]
	}
	return out, cut
}

// eachNode walks a level and calls back with the id of every request it holds, the folders under it
// included: a folder's own list is every request a reader of it would see, and not the rows of the
// level alone.
func eachNode(level domain.Collection, seen func(id string)) {
	for _, node := range level.Items {
		seen(node.ID)
	}
	for _, child := range level.Children {
		eachNode(child, seen)
	}
}

// within keeps the outline entries one folder holds. A folder with no set of its own is the folder
// the page is of, and every entry is already its own: filtering that would be a lookup that always
// answers yes, and one that would walk both outlines to do it.
func within(items []domain.OutlineItem, held map[string]bool) []domain.OutlineItem {
	if held == nil {
		return items
	}
	out := make([]domain.OutlineItem, 0, len(items))
	for _, item := range items {
		if held[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// appended is the history a publish leaves behind: this release in front of the others, or the
// history exactly as it was when nothing moved. A release that says nothing is not written — the
// page draws what changed, and an entry with no requests under it is one that says it did not.
func appended(
	history []domain.PageRelease,
	items []domain.PageItem,
	at time.Time,
) []domain.PageRelease {
	if len(items) == 0 {
		return history
	}
	out := append([]domain.PageRelease{{At: at, Items: items}}, history...)
	if len(out) > maxReleases {
		out = out[:maxReleases]
	}
	return out
}
