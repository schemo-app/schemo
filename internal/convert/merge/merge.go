// Package merge brings a newer API description into a collection, keeping the person's edits: a
// three-way comparison where Compare and Apply are one walk. It reads and writes nothing.
package merge

import (
	"slices"

	"schemo/internal/domain"
)

// Snapshot is what the description said at the last update; a nil Root is not known.
type Snapshot struct {
	Root         *Level                    `json:"root,omitempty"`
	Folders      []Folder                  `json:"folders,omitempty"`
	Requests     []Request                 `json:"requests"`
	Environments []domain.EnvironmentDraft `json:"environments,omitempty"`
}

// Level is what the description says of the collection itself.
type Level struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Auth        *domain.Auth      `json:"auth,omitempty"`
	Variables   []domain.Variable `json:"variables,omitempty"`
}

// Folder is a folder the description names and the saved folder it became.
type Folder struct {
	Path        []string `json:"path"`
	ID          string   `json:"id,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Request is an operation and its saved request. No ID: the person did not take it. No Node: what it
// said is unknown. Gone: no longer described but kept.
type Request struct {
	Key    string                 `json:"key"`
	ID     string                 `json:"id,omitempty"`
	Folder []string               `json:"folder,omitempty"`
	Node   *domain.CollectionNode `json:"node,omitempty"`
	Gone   bool                   `json:"gone,omitempty"`
}

// Input is the three sides: Base (nil before the first update), Mine read whole, and Theirs as the
// reader reads it. Block is the collection whose environment values the description fills.
type Input struct {
	Base         *Snapshot
	Mine         domain.Collection
	Theirs       domain.Collection
	Environments []domain.EnvironmentDraft
	Held         []domain.Environment
	Block        string
}

// Field is the part of a request, a collection or an environment a change is in.
type Field string

const (
	FieldName        Field = "name"
	FieldDescription Field = "description"
	FieldAddress     Field = "address"
	FieldParam       Field = "param"
	FieldHeader      Field = "header"
	FieldPathParam   Field = "pathParam"
	FieldFormField   Field = "formField"
	FieldBody        Field = "body"
	FieldAuth        Field = "auth"
	FieldExample     Field = "example"
	FieldVariable    Field = "variable"
	FieldFolder      Field = "folder"
)

// Attribute is the part of a row or a folder that changed.
type Attribute string

const (
	AttributeName        Attribute = "name"
	AttributeValue       Attribute = "value"
	AttributeDescription Attribute = "description"
	AttributeEnabled     Attribute = "enabled"
	AttributeFile        Attribute = "file"
)

// ChangeKind says whether something came, went or moved on.
type ChangeKind string

const (
	ChangeModified ChangeKind = "modified"
	ChangeAdded    ChangeKind = "added"
	ChangeRemoved  ChangeKind = "removed"
)

// Change is one difference. A Conflict keeps the person's side unless the description's is chosen.
type Change struct {
	ID        string     `json:"id"`
	Field     Field      `json:"field"`
	Name      string     `json:"name,omitempty"`
	Attribute Attribute  `json:"attribute,omitempty"`
	Kind      ChangeKind `json:"kind"`
	Conflict  bool       `json:"conflict,omitempty"`
	Mine      string     `json:"mine,omitempty"`
	Theirs    string     `json:"theirs,omitempty"`
}

// Item is a row of the review. Declined: left out or deleted before. Edited: a dropped request the
// person changed.
type Item struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Method   string   `json:"method,omitempty"`
	Path     string   `json:"path,omitempty"`
	Folder   []string `json:"folder,omitempty"`
	Selected bool     `json:"selected"`
	Declined bool     `json:"declined,omitempty"`
	Edited   bool     `json:"edited,omitempty"`
	Changes  []Change `json:"changes"`
}

// Review is what an update would change.
type Review struct {
	Added        []Item `json:"added"`
	Changed      []Item `json:"changed"`
	Removed      []Item `json:"removed"`
	Settings     *Item  `json:"settings,omitempty"`
	Environments []Item `json:"environments"`
}

// Choices are the items to apply and the conflicts the description wins.
type Choices struct {
	Selected []string `json:"selected"`
	Theirs   []string `json:"theirs"`
}

// Result is what to write; each of Environments is a whole block of one environment.
type Result struct {
	Update                  domain.TreeUpdate
	Environments            []domain.EnvironmentDraft
	Snapshot                Snapshot
	Added, Updated, Removed int
}

// Compare answers what an update would change.
func Compare(in Input) Review {
	r := newRun(in, nil)
	r.do()
	return r.review
}

// Apply is the update the choices make. New levels and requests take ids from ids.
func Apply(in Input, choices Choices, ids func() string) Result {
	r := newRun(in, &choices)
	r.ids = ids
	r.do()
	return r.result
}

// Linked is the snapshot of a collection just imported from the description: every saved row is
// the part it was read from.
func Linked(in Input) Snapshot {
	r := newRun(in, &Choices{})
	r.linking = true
	r.ids = func() string { return "" }
	r.do()
	return r.result.Snapshot
}

type run struct {
	in      Input
	choices *Choices
	linking bool
	ids     func() string
	review  Review
	result  Result

	mine     side
	theirs   side
	folders  folderMap
	recorded map[string]*Request
}

func newRun(in Input, choices *Choices) *run {
	return &run{
		in:      in,
		choices: choices,
		review:  Review{Added: []Item{}, Changed: []Item{}, Removed: []Item{}, Environments: []Item{}},
		result:  Result{Snapshot: Snapshot{Requests: []Request{}}},
		mine:    flatten(in.Mine),
		theirs:  flatten(in.Theirs),
	}
}

func (r *run) do() {
	pairs, gone := r.match()
	r.folders = r.mapFolders(pairs)
	applied := r.settings()
	r.requests(pairs)
	r.dropped(gone)
	r.environments()
	r.rememberFolders(applied)
}

// selected is whether an item is applied. While comparing it is the item's default.
func (r *run) selected(item Item) bool {
	switch {
	case r.linking:
		return true
	case r.choices == nil:
		return item.Selected
	}
	return slices.Contains(r.choices.Selected, item.ID)
}

// offer records a change and answers whether the description's side is taken.
func (r *run) offer(item *Item, change Change, key string) bool {
	change.ID = item.ID + "|" + string(change.Field) + "|" + key + "|" + string(change.Attribute)
	item.Changes = append(item.Changes, change)
	if r.choices == nil {
		return !change.Conflict
	}
	return r.selected(*item) && (!change.Conflict || slices.Contains(r.choices.Theirs, change.ID))
}

// unsaid is whether the description leaves a field empty. With no history that is no difference.
func unsaid(field Field, form string) bool {
	return form == "" || field == FieldAuth && form == string(domain.AuthInherit)
}

// weigh compares one field of the three sides, where base is nil when the past is not known, and
// answers whether the description's side is taken.
func (r *run) weigh(item *Item, change Change, key string, base *string, mine, theirs string) bool {
	switch {
	case mine == theirs:
		return false
	case base == nil:
		if unsaid(change.Field, theirs) {
			return false
		}
		// Filling what is empty loses nothing; anything else may be the person's own.
		change.Conflict = mine != ""
	case mine == *base:
	case theirs == *base:
		// Only the person moved on, so their edit stays.
		return false
	default:
		change.Conflict = true
	}
	if change.Kind == "" {
		change.Kind = ChangeModified
	}
	return r.offer(item, change, key)
}
