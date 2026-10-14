package wails

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
)

// DraftService is the request being composed. What a change means is decided here, and every call
// names the draft it is about — the command line's or the node a card is editing.
type DraftService struct {
	drafts      *draft.UseCase
	collections *collection.UseCase
	host        *desktop.Host
}

func NewDraftService(
	drafts *draft.UseCase,
	collections *collection.UseCase,
	host *desktop.Host,
) *DraftService {
	return &DraftService{drafts: drafts, collections: collections, host: host}
}

// answered completes what the draft cannot work out alone, and every answer to the window goes
// through it: this wrapper is the one place a card's state is finished.
func (s *DraftService) answered(
	ctx context.Context,
	state draft.State,
	err error,
) (draft.State, error) {
	if err != nil {
		return state, err
	}
	return completed(ctx, s.drafts, s.collections, state), nil
}

// completed fills in what a draft cannot work out for itself: what the levels above it answer, and
// the rows and the token that come of it. Walking the tree is not the draft's job.
func completed(
	ctx context.Context,
	drafts *draft.UseCase,
	collections *collection.UseCase,
	state draft.State,
) draft.State {
	// A strip draft has no tree above it, and the walk is skipped rather than run per key.
	var above []domain.Variable
	if !domain.IsTabDraft(state.Draft.ID) {
		// A tree that cannot be reached is a tree that said nothing: the request is still usable, and
		// the rows it would have inherited are the only thing missing.
		if levels, err := collections.Above(ctx, state.Draft.ID); err == nil {
			above = levels.Variables
			state.Inherited = levels.Auth
			// The preview is worked out again now that the levels above are known: a token a collection
			// answers for is not missing, and one the card inherits is.
			if preview, err := drafts.Preview(ctx, state.Draft, levels.Auth, levels.Variables); err == nil {
				state.Preview = preview
			}
		}
	}
	// What the app adds by itself goes out whatever the draft answers for its authorization, so it is
	// worked out here rather than inside either of the branches below.
	if auto, err := drafts.Auto(ctx, state.Draft, state.Inherited, above); err == nil {
		state.Auto = auto
	}
	// A draft that answers for itself has already been projected from its own answer; only the one
	// that inherits had nothing to be projected from until the walk above.
	if state.Draft.Auth.Type != domain.AuthInherit {
		return state
	}
	if projected, err := drafts.Project(ctx, state.Draft, state.Inherited); err == nil {
		state.Projected = projected
		state.Token = drafts.Held(state.Draft, state.Inherited)
	}
	return state
}

// AuthSchemes is every way a request can authorize itself, in the order the window draws them: what
// each scheme asks for, and which of its fields are secrets. The window holds no list of its own.
func (s *DraftService) AuthSchemes() []domain.Scheme { return domain.AuthSchemes() }

// Snapshot is a draft as it stands, which is what a window opens on.
func (s *DraftService) Snapshot(ctx context.Context, id domain.DraftID) (draft.State, error) {
	state, err := s.drafts.Snapshot(ctx, id)
	return s.answered(ctx, state, err)
}

func (s *DraftService) SetMethod(
	ctx context.Context,
	id domain.DraftID,
	method string,
) (draft.State, error) {
	state, err := s.drafts.SetMethod(ctx, id, method)
	return s.answered(ctx, state, err)
}

func (s *DraftService) SetAuth(
	ctx context.Context,
	id domain.DraftID,
	auth domain.Auth,
) (draft.State, error) {
	state, err := s.drafts.SetAuth(ctx, id, auth)
	return s.answered(ctx, state, err)
}

// SetBodyKind picks the format the Body popover composes in. It is a call of its own rather than a
// row patch because it is a property of the request and not of any one row.
func (s *DraftService) SetBodyKind(
	ctx context.Context,
	id domain.DraftID,
	kind domain.BodyKind,
) (draft.State, error) {
	state, err := s.drafts.SetBodyKind(ctx, id, kind)
	return s.answered(ctx, state, err)
}

// SetBodyFile is the path a Binary body will be read from. The dialog that produced it is
// PickBodyFile below, and it is the window that decides which row the answer belongs to.
func (s *DraftService) SetBodyFile(
	ctx context.Context,
	id domain.DraftID,
	path string,
) (draft.State, error) {
	state, err := s.drafts.SetBodyFile(ctx, id, path)
	return s.answered(ctx, state, err)
}

// SetEnvironmentOverride pins this request to an environment of its own, apart from the window's;
// an empty id clears the pin. The bar draws it beside the address, the last check before send.
func (s *DraftService) SetEnvironmentOverride(
	ctx context.Context,
	id domain.DraftID,
	environmentID string,
) (draft.State, error) {
	state, err := s.drafts.SetEnvironmentOverride(ctx, id, environmentID)
	return s.answered(ctx, state, err)
}

// SetText is a buffer flush: the URL and the body are the window's while they are being typed, and
// this is how they reach this side. The revision travels back so the window can match a reply.
func (s *DraftService) SetText(
	ctx context.Context,
	id domain.DraftID,
	in draft.TextInput,
) (draft.TextResult, error) {
	result, err := s.drafts.SetText(ctx, id, in)
	if err != nil {
		return result, err
	}
	result.State, err = s.answered(ctx, result.State, nil)
	return result, err
}

func (s *DraftService) AddRow(
	ctx context.Context,
	id domain.DraftID,
	kind domain.RowKind,
) (draft.Added, error) {
	added, err := s.drafts.AddRow(ctx, id, kind)
	if err != nil {
		return added, err
	}
	added.State, err = s.answered(ctx, added.State, nil)
	return added, err
}

// Preview is the missing-name mark on its own: a window refreshing the mark takes the names rather
// than the whole state.
func (s *DraftService) Preview(ctx context.Context, id domain.DraftID) (draft.Preview, error) {
	state, err := s.drafts.Snapshot(ctx, id)
	if err != nil {
		return draft.Preview{}, err
	}
	return completed(ctx, s.drafts, s.collections, state).Preview, nil
}

// RemoveRow and PatchRow address a row by id and not by position: a click that lands after the list
// changed under it must not delete the row that took its place.
func (s *DraftService) RemoveRow(
	ctx context.Context,
	draftID domain.DraftID,
	kind domain.RowKind,
	id string,
) (draft.State, error) {
	state, err := s.drafts.RemoveRow(ctx, draftID, kind, id)
	return s.answered(ctx, state, err)
}

func (s *DraftService) PatchRow(
	ctx context.Context,
	draftID domain.DraftID,
	kind domain.RowKind,
	id string,
	patch draft.RowPatch,
) (draft.State, error) {
	state, err := s.drafts.PatchRow(ctx, draftID, kind, id, patch)
	return s.answered(ctx, state, err)
}

// MoveRow is a drag of a row: the list it belongs to and the place it was let go at.
func (s *DraftService) MoveRow(
	ctx context.Context,
	draftID domain.DraftID,
	kind domain.RowKind,
	id string,
	position int64,
) (draft.State, error) {
	state, err := s.drafts.MoveRow(ctx, draftID, kind, id, position)
	return s.answered(ctx, state, err)
}

// ObtainAuth and ForgetAuth are the two buttons the design gives a scheme that fetches: ask for a
// token, or throw the one there is away. What went wrong is reported rather than swallowed.
func (s *DraftService) ObtainAuth(
	ctx context.Context,
	draftID domain.DraftID,
) (draft.State, error) {
	state, err := s.drafts.ObtainAuth(ctx, draftID)
	return s.answered(ctx, state, err)
}

func (s *DraftService) ForgetAuth(
	ctx context.Context,
	draftID domain.DraftID,
) (draft.State, error) {
	state, err := s.drafts.ForgetAuth(ctx, draftID)
	return s.answered(ctx, state, err)
}

// Replace hands a draft a whole request: "Open in Request" on a record, or a command pasted into
// the command line.
func (s *DraftService) Replace(
	ctx context.Context,
	id domain.DraftID,
	seed draft.Seed,
) (draft.State, error) {
	state, err := s.drafts.Replace(ctx, id, seed)
	return s.answered(ctx, state, err)
}

// PasteCommand reads a pasted command and, when it is one, replaces the draft with it. The reading
// travels back: the window words a command it could not read and puts the text in the field itself.
func (s *DraftService) PasteCommand(
	ctx context.Context,
	id domain.DraftID,
	text string,
) (draft.Paste, error) {
	pasted, err := s.drafts.PasteCommand(ctx, id, text)
	if err != nil || pasted.State == nil {
		return pasted, err
	}
	state, err := s.answered(ctx, *pasted.State, nil)
	if err != nil {
		return pasted, err
	}
	pasted.State = &state
	return pasted, nil
}

// bodyFileFilter is deliberately wide: a form field can be any file, and offering "*.json" would
// tell the user what they are about to send. The display name is the window's, being a word.
func bodyFileFilter(kindName string) []application.FileFilter {
	return []application.FileFilter{{DisplayName: kindName, Pattern: "*"}}
}

// FormatBody re-indents a JSON or XML body with the indentation the editors are set to, and answers
// with the text it was given when the body does not parse. It touches no draft: the window decides
// whether the result replaces what is in the box.
func (s *DraftService) FormatBody(
	kind domain.BodyKind,
	text string,
	indent domain.Indent,
) string {
	return draft.FormatBody(kind, text, indent)
}

// PickBodyFile asks for the file a body carries and answers with its path, or with nothing when the
// dialog was closed. It writes nothing into the draft — the window patches the row itself.
func (s *DraftService) PickBodyFile(
	ctx context.Context,
	title string,
	kindName string,
) (string, error) {
	return s.host.OpenFile(title, bodyFileFilter(kindName)...)
}
