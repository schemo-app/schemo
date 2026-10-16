package publication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
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

// outlineOf fingerprints collection metadata, folders and requests independently.
func outlineOf(snapshot snapshotv1.Snapshot) ([]domain.OutlineItem, error) {
	var out []domain.OutlineItem
	appendItem := func(id, method, name string, content any) error {
		held, err := json.Marshal(content)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(held)
		parts, err := partsOf(held)
		if err != nil {
			return err
		}
		out = append(out, domain.OutlineItem{
			ID: id, Method: method, Name: name, Fingerprint: hex.EncodeToString(sum[:])[:12], Parts: parts,
		})
		return nil
	}
	collection := snapshot.Collection
	collection.Groups = nil
	metadata := map[string]any{"name": collection.Name, "description": collection.Description,
		"variables": collection.Variables, "environment": snapshot.Environment, "options": snapshot.Options}
	if err := appendItem(collection.ID, "", collection.Name, metadata); err != nil {
		return nil, err
	}
	for _, group := range snapshot.Collection.Groups {
		if group.ID != collection.ID {
			metadata := group
			metadata.Endpoints = nil
			if err := appendItem(group.ID, "", group.Name, metadata); err != nil {
				return nil, err
			}
		}
		for _, endpoint := range group.Endpoints {
			if err := appendItem(endpoint.ID, endpoint.Method, endpoint.Name, endpoint); err != nil {
				return nil, err
			}
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
			fields := changedParts(was.Parts, item.Parts)
			out = append(out, domain.PageItem{Sign: signChanged, Method: item.Method, Name: item.Name, Fields: fields})
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

// eachItem includes folders as well as their requests, so inherited changes include descriptions.
func eachItem(level domain.Collection, seen func(id string)) {
	seen(level.ID)
	for _, node := range level.Items {
		seen(node.ID)
	}
	for _, child := range level.Children {
		eachItem(child, seen)
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
	revision int,
) []domain.PageRelease {
	if len(items) == 0 {
		return history
	}
	out := append([]domain.PageRelease{{At: at, Revision: revision, Items: items}}, history...)
	if len(out) > maxReleases {
		out = out[:maxReleases]
	}
	return out
}

func partsOf(content []byte) (map[string]string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return nil, err
	}
	parts := map[string]string{}
	add := func(key string, value []byte) {
		sum := sha256.Sum256(value)
		parts[key] = hex.EncodeToString(sum[:])[:12]
	}
	for key, value := range fields {
		switch key {
		case "id", "endpoints", "groups":
			continue
		case "parameters":
			var parameters []snapshotv1.Parameter
			if err := json.Unmarshal(value, &parameters); err != nil {
				return nil, err
			}
			for _, parameter := range parameters {
				encoded, err := json.Marshal(parameter)
				if err != nil {
					return nil, err
				}
				add("parameter:"+parameter.In+":"+parameter.Name, encoded)
			}
		case "responses":
			var responses []snapshotv1.Response
			if err := json.Unmarshal(value, &responses); err != nil {
				return nil, err
			}
			for _, response := range responses {
				encoded, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				add("response:"+strconv.FormatInt(response.Status, 10)+":"+response.Name, encoded)
			}
		default:
			add(key, value)
		}
	}
	return parts, nil
}

func changedParts(before, after map[string]string) []string {
	keys := map[string]bool{}
	for key, fingerprint := range before {
		if after[key] != fingerprint {
			keys[key] = true
		}
	}
	for key, fingerprint := range after {
		if before[key] != fingerprint {
			keys[key] = true
		}
	}
	fields := make([]string, 0, len(keys))
	for key := range keys {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return []string{"order"}
	}
	return fields
}
