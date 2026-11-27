package merge

import (
	"encoding/json"
	"slices"
	"strings"

	"schemo/internal/domain"
)

// requests merges each operation into its saved request, and offers the ones that have none.
func (r *run) requests(pairs []pair) {
	for _, one := range pairs {
		if one.mine == nil {
			r.add(one)
			continue
		}
		theirs, mine := one.theirs.node, one.mine.node
		item := &Item{
			ID: "request:" + mine.ID, Name: mine.Name, Method: mine.Method,
			Path: domain.AddressPath(theirs.URL), Folder: one.mine.folder, Selected: true,
		}
		merged := r.request(item, one.base(), mine, theirs)
		if len(item.Changes) > 0 {
			r.review.Changed = append(r.review.Changed, *item)
		}

		applied := len(item.Changes) == 0 || r.selected(*item)
		if r.choices != nil && !r.linking && len(item.Changes) > 0 && applied && !same(merged, mine) {
			r.result.Update.Requests = append(r.result.Update.Requests, merged)
			r.result.Updated++
		}
		entry := Request{Key: theirs.Operation, ID: mine.ID, Folder: one.theirs.folder, Node: &theirs}
		if !applied {
			entry.Node = one.base()
			if one.was != nil {
				entry.Folder = one.was.Folder
			}
		}
		r.result.Snapshot.Requests = append(r.result.Snapshot.Requests, entry)
	}
}

// add offers an operation no saved request is. One left out or deleted before starts unchecked.
func (r *run) add(one pair) {
	theirs := one.theirs.node
	declined := r.recorded[theirs.Operation] != nil
	item := Item{
		ID: "add:" + theirs.Operation, Name: theirs.Name, Method: theirs.Method,
		Path: domain.AddressPath(theirs.URL), Folder: one.theirs.folder,
		Selected: !declined, Declined: declined, Changes: []Change{},
	}
	r.review.Added = append(r.review.Added, item)

	entry := Request{Key: theirs.Operation, Folder: one.theirs.folder, Node: &theirs}
	if r.choices != nil && !r.linking && r.selected(item) {
		node := theirs
		node.ID = r.ids()
		node.CollectionID = r.ensure(one.theirs.folder)
		node.Position = r.folders.position(node.CollectionID)
		r.result.Update.Requests = append(r.result.Update.Requests, node)
		r.result.Added++
		entry.ID = node.ID
	}
	r.result.Snapshot.Requests = append(r.result.Snapshot.Requests, entry)
}

// dropped offers to delete the requests the description no longer names. Nothing is deleted
// unless chosen; one that is kept stays on offer.
func (r *run) dropped(gone []Request) {
	for _, was := range gone {
		index := slices.IndexFunc(r.mine.requests, func(one placed) bool { return one.node.ID == was.ID })
		mine := r.mine.requests[index]
		item := Item{
			ID: "remove:" + was.ID, Name: mine.node.Name, Method: mine.node.Method,
			Path: domain.AddressPath(mine.node.URL), Folder: mine.folder,
			Edited: edited(was.Node, mine.node), Changes: []Change{},
		}
		r.review.Removed = append(r.review.Removed, item)
		if r.choices != nil && !r.linking && r.selected(item) {
			r.result.Update.Deleted = append(r.result.Update.Deleted, was.ID)
			r.result.Removed++
			continue
		}
		was.Gone = true
		r.result.Snapshot.Requests = append(r.result.Snapshot.Requests, was)
	}
}

// request merges one operation into the saved request it became. Scripts, protocol settings and
// the body's file are the person's alone and stay as they are.
func (r *run) request(item *Item, base *domain.CollectionNode, mine, theirs domain.CollectionNode) domain.CollectionNode {
	out := mine
	var past domain.CollectionNode
	known := base != nil
	if known {
		past = *base
	}
	field := func(name Field, form, show func(domain.CollectionNode) string, take func()) {
		var before *string
		if known {
			value := form(past)
			before = &value
		}
		change := Change{Field: name, Mine: shorten(show(mine)), Theirs: shorten(show(theirs))}
		if r.weigh(item, change, "", before, form(mine), form(theirs)) {
			take()
		}
	}

	field(FieldName, nameForm, nameForm, func() { out.Name = theirs.Name })
	field(FieldDescription, descriptionForm, descriptionForm, func() { out.Description = theirs.Description })
	address := baseOf(mine)
	field(FieldAddress, baseOf, baseOf, func() { address = baseOf(theirs) })

	out.Params = mergeList(r, item, paramList, past.Params, known, mine.Params, theirs.Params)
	out.URL = composeURL(mine, theirs, address, out.Params)
	out.PathParams = r.pathParams(item, base, mine, theirs, out.URL)
	out.Headers = mergeList(r, item, headerList, past.Headers, known, mine.Headers, theirs.Headers)
	out.Form = mergeList(r, item, formList, past.Form, known, mine.Form, theirs.Form)

	field(FieldBody, bodyForm, func(node domain.CollectionNode) string { return node.Body },
		func() { out.Body, out.BodyKind = theirs.Body, theirs.BodyKind })
	field(FieldAuth, func(node domain.CollectionNode) string { return authForm(node.Auth) },
		func(node domain.CollectionNode) string { return authShown(node.Auth) },
		func() { out.Auth = theirs.Auth })
	out.Responses = mergeList(r, item, exampleList, past.Responses, known, mine.Responses, theirs.Responses)
	return out
}

// pathParams keeps one row per `:name` of the merged address. A parameter the description renamed
// carries its value over to the new name.
func (r *run) pathParams(
	item *Item, base *domain.CollectionNode, mine, theirs domain.CollectionNode, address string,
) []domain.Row {
	names := domain.PathParams(address)
	var past []domain.Row
	held := mine.PathParams
	if base != nil {
		past = base.PathParams
		for from, to := range renamedParams(domain.PathParams(base.URL), domain.PathParams(theirs.URL)) {
			if slices.Contains(names, to) {
				past, held = renameRow(past, from, to), renameRow(held, from, to)
			}
		}
	}
	only := func(rows []domain.Row) []domain.Row {
		return slices.DeleteFunc(slices.Clone(rows), func(row domain.Row) bool { return !slices.Contains(names, row.Name) })
	}

	before := len(item.Changes)
	merged := mergeList(r, item, pathList, only(past), base != nil, only(held), only(theirs.PathParams))
	if len(item.Changes) == before && address == mine.URL {
		return mine.PathParams
	}
	// The editor draws one row per `:name`, in the address's order.
	var out []domain.Row
	for _, name := range names {
		row := domain.Row{Name: name, Enabled: true}
		if at := slices.IndexFunc(merged, func(one domain.Row) bool { return one.Name == name }); at >= 0 {
			row = merged[at]
		}
		out = append(out, row)
	}
	return out
}

// renamedParams pairs names that changed in place: the same position, and neither on the other side.
func renamedParams(before, after []string) map[string]string {
	out := map[string]string{}
	if len(before) != len(after) {
		return out
	}
	for index, name := range before {
		if name != after[index] && !slices.Contains(after, name) && !slices.Contains(before, after[index]) {
			out[name] = after[index]
		}
	}
	return out
}

func renameRow(rows []domain.Row, from, to string) []domain.Row {
	out := slices.Clone(rows)
	for index := range out {
		if out[index].Name == from {
			out[index].Name = to
		}
	}
	return out
}

// edited is whether the person changed a request since it was last read.
func edited(base *domain.CollectionNode, mine domain.CollectionNode) bool {
	if base == nil || !mine.Scripts.Empty() || mine.BodyFile != "" || mine.Protocol != base.Protocol {
		return true
	}
	return describedForm(*base) != describedForm(mine)
}

// describedForm is everything of a request a description can say, as compared.
func describedForm(node domain.CollectionNode) string {
	parts := []string{
		nameForm(node), descriptionForm(node), baseOf(node), bodyForm(node), authForm(node.Auth),
		paramList.form(node.Params), headerList.form(node.Headers), pathList.form(node.PathParams),
		formList.form(node.Form), exampleList.form(node.Responses),
	}
	encoded, _ := json.Marshal(parts)
	return string(encoded)
}

func nameForm(node domain.CollectionNode) string        { return strings.TrimSpace(node.Name) }
func descriptionForm(node domain.CollectionNode) string { return strings.TrimSpace(node.Description) }

func baseOf(node domain.CollectionNode) string {
	base, _ := domain.SplitQuery(node.URL)
	return base
}

func bodyForm(node domain.CollectionNode) string {
	kind := domain.KindOf(node.BodyKind)
	if node.Body == "" && kind == domain.BodyRaw {
		return ""
	}
	return string(kind) + "\n" + canonicalJSON(node.Body)
}

// canonicalJSON is a JSON text with its layout taken out, so reformatting it is no change; other
// text is as written.
func canonicalJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return text
	}
	if !json.Valid([]byte(trimmed)) {
		return text
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return text
	}
	return string(encoded)
}

// same is whether two values would be stored alike.
func same[T any](a, b T) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// sameRows is same for lists, where none and an empty one are alike.
func sameRows[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || same(a, b))
}
