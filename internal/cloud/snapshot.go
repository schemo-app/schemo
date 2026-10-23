package cloud

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"schemo/internal/domain"
	"slices"
	"strconv"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// Encode writes a document as the bytes a publish carries: one JSON envelope, compressed, with the
// digest of what it says. The digest is of the uncompressed document, and the server recomputes it
// from the bytes it expands.
func Encode(snapshot snapshotv1.Snapshot) ([]byte, string, error) {
	document, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	digest, err := Digest(document)
	if err != nil {
		return nil, "", err
	}

	var packed bytes.Buffer
	sink := gzip.NewWriter(&packed)
	if _, err := sink.Write(document); err != nil {
		return nil, "", err
	}
	if err := sink.Close(); err != nil {
		return nil, "", err
	}
	return packed.Bytes(), digest, nil
}

// Digest is the identity of a document's content, as both this device and the server compute it.
// Two documents that differ only in which build wrote them, or in which language they are read in,
// are the same document: those two fields are dropped before hashing, and everything else is
// written in the one order that does not depend on who wrote it (keys sorted at every level,
// numbers as written). That is what lets a device say a page is behind without publishing to find
// out.
func Digest(document []byte) (string, error) {
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return "", err
	}
	delete(fields, "generator")
	delete(fields, "locale")

	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// snapshotChanges maps publication history to the generated public document model.
func snapshotChanges(releases []domain.PageRelease) []snapshotv1.Release {
	if releases == nil {
		return nil
	}
	out := make([]snapshotv1.Release, len(releases))
	for index, release := range releases {
		var items []snapshotv1.Item
		if release.Items != nil {
			items = make([]snapshotv1.Item, len(release.Items))
		}
		for at, item := range release.Items {
			items[at] = snapshotv1.Item{Sign: item.Sign, Method: item.Method, Name: item.Name, Fields: item.Fields}
		}
		out[index] = snapshotv1.Release{At: release.At, Items: items, Revision: int64(release.Revision)}
	}
	return out
}

// storedChanges keeps persistence history independent of generated wire declarations.
func storedChanges(releases []snapshotv1.Release) []domain.PageRelease {
	if releases == nil {
		return nil
	}
	out := make([]domain.PageRelease, len(releases))
	for index, release := range releases {
		var items []domain.PageItem
		if release.Items != nil {
			items = make([]domain.PageItem, len(release.Items))
		}
		for at, item := range release.Items {
			items[at] = domain.PageItem{Sign: item.Sign, Method: item.Method, Name: item.Name, Fields: item.Fields}
		}
		out[index] = domain.PageRelease{At: release.At, Items: items, Revision: int(release.Revision)}
	}
	return out
}

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
// the page is of, and every entry is already its own: filtering would be a lookup that always
// answers yes and walks both outlines to do it.
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
// history exactly as it was when nothing moved. A release that says nothing is not written, since
// an entry with no requests under it says that it did not.
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
	fields := slices.Sorted(maps.Keys(keys))
	if len(fields) == 0 {
		return []string{"order"}
	}
	return fields
}
