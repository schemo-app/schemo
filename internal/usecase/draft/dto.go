package draft

import (
	"schemo/internal/command"
	"schemo/internal/domain"
)

// The shapes that exist only for the window: the draft as it stands, the two texts it buffers, and
// what each edit answers with. The row a draft is stored as is domain.Draft, and is not here.

// State is the draft as it stands, with the preview that follows from it. Every answer carries
// both, so the window never has to ask what changed — and never has to work out for itself whether
// what it holds can go out.
type State struct {
	Draft   domain.Draft `json:"draft"`
	Preview Preview      `json:"preview"`
	// Projected is what the authorization puts in the parameter and header lists, which is not part
	// of the draft and is not stored with it: it is what the draft's auth comes to, worked out on
	// every answer so the two can never disagree.
	Projected []domain.ProjectedRow `json:"projected"`
	// Auto is what the app itself puts on the wire — Host, Content-Length and the rest of them —
	// worked out on every answer for the same reason. Whether the window draws them is the
	// appearance's own question (`domain.Requests.ShowAuto`), which is why they are always here.
	Auto []domain.ProjectedRow `json:"auto"`
	// Token is the state of a credential somebody else issues, for the schemes that have one. It is
	// absent for a scheme that carries what it was given: there is nothing to say about a token the
	// user typed, and a block saying so would be noise.
	Token *domain.AuthToken `json:"token,omitempty"`
	// Inherited is what the levels above this draft answer with, for a draft that is a node of a
	// tree: the nearest one that gave a credential, or nothing when none did. It is not a getter the
	// window could have written for itself — «None» on a folder is a level the walk goes past, and
	// that rule belongs with the walk.
	Inherited *domain.Auth `json:"inherited,omitempty"`
}

// TextInput is a buffer flush. Rev is the window's own counter, and it comes back with the answer:
// a reply to a keystroke that has since been typed over is recognised by it and dropped.
type TextInput struct {
	Field TextField `json:"field"`
	Text  string    `json:"text"`
	Rev   int64     `json:"rev"`
}

// TextResult is a buffer's answer. It carries back which buffer it is and the revision the window
// sent with it: a reply to a keystroke that has since been typed over is recognised by those two
// and dropped. The text itself is not in the answer — it is the window's until the window says
// otherwise, and an answer that overwrote it would be a character lost under the caret.
type TextResult struct {
	Field TextField `json:"field"`
	Rev   int64     `json:"rev"`
	State
}

// Added is the draft after a row appeared, with the id it was given: the window addresses a row by
// its id from the first moment, and a list matched against its own copy would be a guess.
type Added struct {
	State
	ID string `json:"id"`
}

// RowPatch is an edit to one row. Each field is optional, because a patch says what changed and
// nothing else: a row sent whole would undo a keystroke that landed between the two.
type RowPatch struct {
	Name        *string `json:"name,omitempty"`
	Value       *string `json:"value,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	Description *string `json:"description,omitempty"`
	// Src and File belong to a form row: the path a file field carries, and whether it is one.
	Src  *string `json:"src,omitempty"`
	File *bool   `json:"file,omitempty"`
}

// Seed is a whole request handed to the draft: what "Open in Request" and a pasted command both
// produce. Headers are pairs because a request can carry the same name twice.
type Seed struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Body   string `json:"body"`
	// A seed carries the format too: a followed link or a pasted command is a whole request, and the
	// Content-Type that follows from the kind is part of what it goes out with.
	BodyKind domain.BodyKind  `json:"bodyKind,omitempty"`
	Form     []domain.FormRow `json:"form,omitempty"`
	BodyFile string           `json:"bodyFile,omitempty"`
	// A seed carries no cookies of its own: a request's cookies are the jar's, read when it is sent.
	// A command pasted into the line puts them in a `Cookie` header, which is where a request keeps
	// the ones that came from outside, and the jar is not asked in that case.
	Headers []domain.HeaderPair `json:"headers,omitempty"`
	// Auth is what the request authorizes itself with. It travels resolved: whoever builds a seed
	// knows where the request came from, and a seed is a request, not a place in a tree.
	Auth *domain.Auth `json:"auth,omitempty"`
	// EnvironmentID is the environment this one request resolves its `{{tokens}}` in, when it is not
	// the window's own — a run sends requests that were saved with a pin, and it is no longer looking
	// at the tree they came from. Empty means the window's, which is what a followed link and a
	// pasted command answer with: neither was ever pinned to anything.
	EnvironmentID string `json:"environmentId,omitempty"`
}

// Paste is a pasted command applied to the draft: what the reading came to, and the draft as it
// stands when there turned out to be something to replace it with. State is absent for the pastes
// that were not commands at all, which is most of them.
type Paste struct {
	Reading command.Reading `json:"reading"`
	State   *State          `json:"state,omitempty"`
}

// Copy is the request written as a command for one tool, and what that notation had to leave out.
// Omitted is not a failure: the command is written all the same, and what is missing is named.
type Copy struct {
	Text    string             `json:"text"`
	Omitted []command.Omission `json:"omitted"`
}

type Preview struct {
	Missing []string `json:"missing"`
}
