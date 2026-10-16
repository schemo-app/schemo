package publication

import (
	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"slices"

	"schemo/internal/domain"
)

func applyOptions(snapshot *snapshotv1.Snapshot, options domain.PublicationOptions) {
	snapshot.Options = snapshotv1.Options{Docs: options.Docs, Examples: options.Examples}
	if !options.Docs {
		snapshot.Collection.Description = ""
		snapshot.Prose = snapshotv1.Prose{}
	}
	for groupIndex := range snapshot.Collection.Groups {
		if !options.Docs {
			snapshot.Collection.Groups[groupIndex].Description = ""
		}
		for index := range snapshot.Collection.Groups[groupIndex].Endpoints {
			endpoint := &snapshot.Collection.Groups[groupIndex].Endpoints[index]
			if !options.Docs {
				endpoint.Description = ""
				for parameter := range endpoint.Parameters {
					endpoint.Parameters[parameter].Description = ""
				}
				if endpoint.Body != nil {
					for field := range endpoint.Body.Fields {
						endpoint.Body.Fields[field].Description = ""
					}
				}
			}
			if !options.Examples {
				endpoint.Responses = nil
			}
		}
	}
	snapshot.Collection.Groups = slices.DeleteFunc(snapshot.Collection.Groups, func(group snapshotv1.Group) bool {
		return len(group.Endpoints) == 0 && group.Description == ""
	})
}
