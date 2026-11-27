package merge

import (
	"slices"
	"strings"

	"schemo/internal/domain"
)

// side is a collection walked flat, in display order. folders[0] is the collection itself.
type side struct {
	requests []placed
	folders  []placedFolder
}

type placed struct {
	node   domain.CollectionNode
	folder []string
}

type placedFolder struct {
	collection domain.Collection
	path       []string
}

func flatten(collection domain.Collection) side {
	var out side
	var walk func(level domain.Collection, path []string)
	walk = func(level domain.Collection, path []string) {
		out.folders = append(out.folders, placedFolder{collection: level, path: path})
		for _, entry := range level.Level() {
			if entry.Node != nil {
				out.requests = append(out.requests, placed{node: *entry.Node, folder: path})
				continue
			}
			walk(*entry.Collection, append(slices.Clone(path), entry.Collection.Name))
		}
	}
	walk(collection, nil)
	return out
}

func pathKey(path []string) string { return strings.Join(path, "\x00") }

// pair is an operation, the saved request it is and what the last update recorded of it.
type pair struct {
	theirs placed
	mine   *placed
	was    *Request
}

func (p pair) base() *domain.CollectionNode {
	if p.was == nil {
		return nil
	}
	return p.was.Node
}

// match pairs operations with saved requests: by what the last update recorded, then by method and
// path. gone are recorded requests whose operation the description no longer names.
func (r *run) match() ([]pair, []Request) {
	saved := map[string]*placed{}
	for index := range r.mine.requests {
		saved[r.mine.requests[index].node.ID] = &r.mine.requests[index]
	}
	byKey, byID := map[string]*Request{}, map[string]*Request{}
	if r.in.Base != nil {
		for index := range r.in.Base.Requests {
			was := &r.in.Base.Requests[index]
			if byKey[was.Key] == nil {
				byKey[was.Key] = was
			}
			if was.ID != "" {
				byID[was.ID] = was
			}
		}
	}
	r.recorded = byKey

	pairs := make([]pair, len(r.theirs.requests))
	taken, named := map[string]bool{}, map[string]bool{}
	for index, theirs := range r.theirs.requests {
		pairs[index].theirs = theirs
		key := theirs.node.Operation
		named[key] = true
		if was := byKey[key]; was != nil && saved[was.ID] != nil && !taken[was.ID] {
			pairs[index].mine, pairs[index].was = saved[was.ID], was
			taken[was.ID] = true
		}
	}

	// A request without a record is the operation when method and path say so: first the ones
	// whose name agrees, then the ones that leave no choice.
	address := variableValue(r.in.Theirs.Variables, domain.BaseURLVariable)
	for _, byName := range []bool{true, false} {
		for index := range pairs {
			one := &pairs[index]
			if one.mine != nil {
				continue
			}
			shapes := shapesOf(one.theirs.node, address)
			var candidates []*placed
			for at := range r.mine.requests {
				mine := &r.mine.requests[at]
				if !taken[mine.node.ID] &&
					shapes[domain.OperationKey(mine.node.Method, domain.AddressPath(mine.node.URL))] {
					candidates = append(candidates, mine)
				}
			}
			chosen := choose(candidates, one.theirs, byName)
			if chosen == nil {
				continue
			}
			taken[chosen.node.ID] = true
			one.mine = chosen
			if one.was = byKey[one.theirs.node.Operation]; one.was == nil {
				one.was = byID[chosen.node.ID]
			}
		}
	}

	var gone []Request
	if r.in.Base != nil {
		for _, was := range r.in.Base.Requests {
			if saved[was.ID] != nil && !taken[was.ID] && !named[was.Key] {
				gone = append(gone, was)
			}
		}
	}
	return pairs, gone
}

// shapesOf are the keys a saved request may carry for an operation: the address as the reader
// writes it, and with the base address spelled out.
func shapesOf(node domain.CollectionNode, address string) map[string]bool {
	out := map[string]bool{domain.OperationKey(node.Method, domain.AddressPath(node.URL)): true}
	if address != "" {
		full := strings.ReplaceAll(node.URL, "{{"+domain.BaseURLVariable+"}}", address)
		out[domain.OperationKey(node.Method, domain.AddressPath(full))] = true
	}
	return out
}

func choose(candidates []*placed, theirs placed, byName bool) *placed {
	if !byName {
		if len(candidates) == 1 {
			return candidates[0]
		}
		return nil
	}
	var first *placed
	for _, candidate := range candidates {
		if candidate.node.Name != theirs.node.Name {
			continue
		}
		if slices.Equal(candidate.folder, theirs.folder) {
			return candidate
		}
		if first == nil {
			first = candidate
		}
	}
	return first
}

func variableValue(variables []domain.Variable, name string) string {
	for _, one := range variables {
		if one.Name == name {
			return one.Value
		}
	}
	return ""
}

// folderMap maps a description's folder path to a saved folder id ("" is the collection).
type folderMap struct {
	ids     map[string]string
	saved   map[string]*placedFolder
	renamed map[string]Folder
	next    map[string]int64
}

// mapFolders finds each folder of the description among the saved ones: the recorded one, the one
// whose requests moved with a renamed tag, or one of the same name.
func (r *run) mapFolders(pairs []pair) folderMap {
	out := folderMap{
		ids: map[string]string{"": r.in.Mine.ID}, saved: map[string]*placedFolder{},
		renamed: map[string]Folder{}, next: map[string]int64{},
	}
	for index := range r.mine.folders {
		out.saved[r.mine.folders[index].collection.ID] = &r.mine.folders[index]
	}
	recorded := r.recordedFolders()
	named := map[string]bool{}
	for _, theirs := range r.theirs.folders {
		named[pathKey(theirs.path)] = true
	}

	used := map[string]bool{r.in.Mine.ID: true}
	free := func(id string) bool { return out.saved[id] != nil && !used[id] }
	for _, theirs := range r.theirs.folders[1:] {
		key, id := pathKey(theirs.path), ""
		if was, ok := recorded[key]; ok && free(was.ID) {
			id = was.ID
		}
		if id == "" {
			if was, ok := renamedFrom(pairs, key, recorded, named); ok && free(was.ID) {
				id, out.renamed[key] = was.ID, was
			}
		}
		if id == "" {
			if parent, ok := out.ids[pathKey(theirs.path[:len(theirs.path)-1])]; ok {
				for _, child := range out.saved[parent].collection.Children {
					if child.Name == theirs.collection.Name && !used[child.ID] {
						id = child.ID
						break
					}
				}
			}
		}
		if id != "" {
			out.ids[key], used[id] = id, true
		}
	}
	return out
}

func (r *run) recordedFolders() map[string]Folder {
	out := map[string]Folder{}
	if r.in.Base != nil {
		for _, folder := range r.in.Base.Folders {
			out[pathKey(folder.Path)] = folder
		}
	}
	return out
}

// renamedFrom is the recorded folder that every earlier request of a new folder came from, when the
// description no longer names it.
func renamedFrom(pairs []pair, key string, recorded map[string]Folder, named map[string]bool) (Folder, bool) {
	from, count := "", 0
	for _, one := range pairs {
		if one.was == nil || pathKey(one.theirs.folder) != key {
			continue
		}
		before := pathKey(one.was.Folder)
		if count > 0 && before != from {
			return Folder{}, false
		}
		from = before
		count++
	}
	was, ok := recorded[from]
	return was, ok && count > 0 && from != "" && !named[from]
}

// ensure is the saved folder for a folder of the description, made when there is none.
func (r *run) ensure(path []string) string {
	key := pathKey(path)
	if id, ok := r.folders.ids[key]; ok {
		return id
	}
	parent := r.ensure(path[:len(path)-1])
	made := domain.Collection{
		ID: r.ids(), Name: path[len(path)-1], ParentID: parent, Position: r.folders.position(parent),
		Items: []domain.CollectionNode{}, Children: []domain.Collection{},
	}
	for _, theirs := range r.theirs.folders {
		if pathKey(theirs.path) == key {
			made.Description = theirs.collection.Description
		}
	}
	r.result.Update.Levels = append(r.result.Update.Levels, made)
	r.folders.ids[key] = made.ID
	return made.ID
}

// position is the next free place at the end of a saved level.
func (f *folderMap) position(level string) int64 {
	next, ok := f.next[level]
	if !ok {
		if held := f.saved[level]; held != nil {
			for _, node := range held.collection.Items {
				next = max(next, node.Position+1)
			}
			for _, child := range held.collection.Children {
				next = max(next, child.Position+1)
			}
		}
	}
	f.next[level] = next + 1
	return next
}

// rememberFolders records the description's folders. A description the person did not take stays as
// recorded, so it is offered again.
func (r *run) rememberFolders(applied bool) {
	recorded := r.recordedFolders()
	for _, theirs := range r.theirs.folders[1:] {
		key := pathKey(theirs.path)
		folder := Folder{Path: theirs.path, ID: r.folders.ids[key], Description: theirs.collection.Description}
		if was, ok := recorded[key]; ok && !applied {
			folder.Description = was.Description
		}
		r.result.Snapshot.Folders = append(r.result.Snapshot.Folders, folder)
	}
}
