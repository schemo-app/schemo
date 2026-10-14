package publication

import (
	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

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
			items[at] = snapshotv1.Item{Sign: item.Sign, Method: item.Method, Name: item.Name}
		}
		out[index] = snapshotv1.Release{At: release.At, Items: items}
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
			items[at] = domain.PageItem{Sign: item.Sign, Method: item.Method, Name: item.Name}
		}
		out[index] = domain.PageRelease{At: release.At, Items: items}
	}
	return out
}
