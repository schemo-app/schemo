package request

import (
	"context"
	"errors"
	"schemo/internal/convert/command"
	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/workspace"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Drafts is bound to the window as it is. The interfaces it holds are the boundaries a test
// replaces: the network, the file system, the cookie jar and the values of the variables.
type Drafts struct {
	store       *db.Store
	scope       domain.Scope
	vars        VariableSource
	jar         CookieHeaders
	files       FileSource
	auth        AuthMaterializer
	net         NetDefaults
	ids         platform.IDGen
	collections *workspace.Collections
	records     *History
	host        *desktop.Host
}

// DraftDeps is what a Drafts is made of; the window's composition fills it once.
type DraftDeps struct {
	Store       *db.Store
	Scope       domain.Scope
	Vars        VariableSource
	Jar         CookieHeaders
	Files       FileSource
	Auth        AuthMaterializer
	Net         NetDefaults
	IDs         platform.IDGen
	Collections *workspace.Collections
	History     *History
	Host        *desktop.Host
}

func NewDrafts(d DraftDeps) *Drafts {
	return &Drafts{store: d.Store, scope: d.Scope, vars: d.Vars, jar: d.Jar, files: d.Files,
		auth: d.Auth, net: d.Net, ids: d.IDs, collections: d.Collections, records: d.History,
		host: d.Host}
}

// Preview is what follows from a draft without sending it: the names nothing answers, the rows the
// authorization and the app add, the state of a fetched token, and what the levels above answer
// for "Inherit".
type Preview struct {
	Missing   []string              `json:"missing"`
	Projected []domain.ProjectedRow `json:"projected"`
	Auto      []domain.ProjectedRow `json:"auto"`
	Token     *domain.AuthToken     `json:"token,omitempty"`
	Inherited *domain.Auth          `json:"inherited,omitempty"`
}

// Paste is a pasted text read as a command: what the reading came to and, when it was one, the
// request it holds. Most pastes are not commands, and those carry no request.
type Paste struct {
	Reading command.Reading `json:"reading"`
	Draft   *domain.Draft   `json:"draft,omitempty"`
}

// Copy is the request written as a command for one tool, and what that notation had to leave out.
type Copy struct {
	Text    string             `json:"text"`
	Omitted []command.Omission `json:"omitted"`
}

// NodeEditor is a saved request and the draft its editor opens with.
type NodeEditor struct {
	Node  domain.CollectionNode `json:"node"`
	Draft domain.Draft          `json:"draft"`
}

// Open reads a tab's draft. A tab whose draft was never written starts on an empty GET.
func (s *Drafts) Open(ctx context.Context, id domain.DraftID) (domain.Draft, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Draft{}, err
	}
	stored, err := s.store.Draft(ctx, workspace, id)
	if errors.Is(err, domain.ErrNotFound) {
		fresh := domain.NewDraft()
		fresh.ID = id
		return fresh, nil
	}
	if err != nil {
		return domain.Draft{}, err
	}
	return s.withRowIDs(stored), nil
}

// Save writes a tab's draft down. Only a tab's draft is kept: a saved request's draft becomes the
// request through SaveNode and nothing else.
func (s *Drafts) Save(ctx context.Context, d domain.Draft) error {
	if !domain.IsTabDraft(d.ID) {
		return domain.Refuse(domain.CodeDraftWithoutID, domain.ErrNotAllowed, nil)
	}
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.SaveDraft(ctx, workspace, s.withRowIDs(d))
}

// Discard deletes the draft of a closed tab.
func (s *Drafts) Discard(ctx context.Context, id domain.DraftID) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.DeleteDraft(ctx, workspace, id)
}

// FromRecord writes a history record down as a new tab's draft: the method, the address, the
// headers and the body the request went out with.
func (s *Drafts) FromRecord(ctx context.Context, id domain.DraftID, recordID string) error {
	rec, err := s.records.Record(ctx, recordID)
	if err != nil {
		return err
	}
	body, err := s.records.Body(ctx, recordID, domain.SideRequest)
	if err != nil {
		return err
	}
	seeded := domain.NewDraft()
	seeded.ID = id
	seeded.Method, seeded.URL, seeded.Body = rec.Method, rec.URL, body
	seeded.Headers = s.rowsFromHeaders(rec.RequestHeaders)
	return s.Save(ctx, seeded)
}

// Copy writes a new tab's draft from another tab's or from a saved request, for a duplicated tab.
func (s *Drafts) Copy(ctx context.Context, from, to domain.DraftID) error {
	var source domain.Draft
	if domain.IsTabDraft(from) {
		read, err := s.Open(ctx, from)
		if err != nil {
			return err
		}
		source = read
	} else {
		node, err := s.collections.Node(ctx, string(from))
		if err != nil {
			return err
		}
		source = FromNode(node)
	}
	source.ID = to
	return s.Save(ctx, source)
}

// OpenNode is a saved request and the draft its editor starts from.
func (s *Drafts) OpenNode(ctx context.Context, id string) (NodeEditor, error) {
	node, err := s.collections.Node(ctx, id)
	if err != nil {
		return NodeEditor{}, err
	}
	return NodeEditor{Node: node, Draft: s.withRowIDs(FromNode(node))}, nil
}

// SaveNode writes an edited draft back into the request it was opened from. The node keeps what
// the editor does not own: its name, its position and when it was made.
func (s *Drafts) SaveNode(ctx context.Context, d domain.Draft) (NodeEditor, error) {
	node, err := s.collections.Node(ctx, string(d.ID))
	if err != nil {
		return NodeEditor{}, err
	}
	if err := s.collections.SaveNode(ctx, nodeFromDraft(node, d)); err != nil {
		return NodeEditor{}, err
	}
	return s.OpenNode(ctx, node.ID)
}

// SaveAsNode copies a draft into a collection as a new request; the draft stays as it was.
func (s *Drafts) SaveAsNode(
	ctx context.Context,
	collectionID string,
	name string,
	d domain.Draft,
) (domain.CollectionNode, error) {
	return s.collections.CreateNode(ctx, workspace.RequestDraft{
		CollectionID:  collectionID,
		Name:          name,
		Method:        d.Method,
		URL:           d.URL,
		Params:        d.Params,
		Headers:       d.Headers,
		PathParams:    d.PathParams,
		Body:          d.Body,
		BodyKind:      d.BodyKind,
		Form:          d.Form,
		BodyFile:      d.BodyFile,
		Auth:          &d.Auth,
		EnvironmentID: d.EnvironmentID,
	})
}

// SaveAsNewCollection saves a draft as the first request of a collection made for it. It is one
// call because it is one intention: two would leave an empty collection behind when the second
// failed.
func (s *Drafts) SaveAsNewCollection(
	ctx context.Context,
	collectionName string,
	name string,
	d domain.Draft,
) (domain.CollectionNode, error) {
	made, err := s.collections.CreateCollection(ctx, collectionName, "", "")
	if err != nil {
		return domain.CollectionNode{}, err
	}
	return s.SaveAsNode(ctx, made.ID, name, d)
}

// Preview works out everything a draft shows beside what was typed. A tree that cannot be reached
// answers nothing: the draft is still usable, and only the inherited rows are missing.
func (s *Drafts) Preview(ctx context.Context, d domain.Draft) (Preview, error) {
	above := s.above(ctx, d.ID)
	cookie, err := s.jarCookies(ctx, d)
	if err != nil {
		return Preview{}, err
	}
	missing, err := s.vars.Missing(ctx, above.Variables,
		partsOf(d, above.Auth, cookie).texts(), d.EnvironmentID)
	if err != nil {
		return Preview{}, err
	}
	projected, err := s.projected(d, above.Auth)
	if err != nil {
		return Preview{}, err
	}
	auto, err := s.autoRows(ctx, d, above.Auth, above.Variables, cookie)
	if err != nil {
		return Preview{}, err
	}
	return Preview{
		Missing:   domain.OrEmpty(missing),
		Projected: projected,
		Auto:      auto,
		Token:     s.held(d, above.Auth),
		Inherited: above.Auth,
	}, nil
}

// Send sends a draft with what the levels above it answer, and answers with the send's id; the
// record arrives as an event.
func (s *Drafts) Send(ctx context.Context, d domain.Draft) (string, error) {
	above := s.above(ctx, d.ID)
	prepared, err := s.Prepared(ctx, d, above.Auth, above.Variables)
	if err != nil {
		return "", err
	}
	input := prepared.SendInput()
	input.Node = string(d.ID)
	return s.records.Send(ctx, input)
}

// SendSeed sends a request that is in no tab and no tree (a history row sent again, say), and
// leaves every draft as it is.
func (s *Drafts) SendSeed(ctx context.Context, seed Seed) (string, error) {
	prepared, err := s.Prepare(ctx, seed, nil)
	if err != nil {
		return "", err
	}
	return s.records.Send(ctx, prepared.SendInput())
}

// CopyAs writes a draft out as a command for one tool, with the real values on it. It asks for no
// token and opens no file: what it could not write is named instead.
func (s *Drafts) CopyAs(ctx context.Context, d domain.Draft, format command.Format) (Copy, error) {
	above := s.above(ctx, d.ID)
	cookie, err := s.jarCookies(ctx, d)
	if err != nil {
		return Copy{}, err
	}
	return s.renderCommand(ctx, d, above.Auth, above.Variables, format, cookie, true)
}

// Paste reads a pasted text as a command. The request it holds comes without an id: it replaces
// what the window's draft holds, and the window keeps its own.
func (s *Drafts) Paste(text string) Paste {
	reading := command.Parse(text)
	if reading.Kind != command.KindOK {
		return Paste{Reading: reading}
	}
	d := s.draftOfSeed(seedOf(reading.Request))
	return Paste{Reading: reading, Draft: &d}
}

// ObtainAuth fetches the token the draft's authorization (or the one it inherits) asks for.
func (s *Drafts) ObtainAuth(ctx context.Context, d domain.Draft) error {
	return s.auth.Obtain(ctx, authToApply(d.Auth, s.above(ctx, d.ID).Auth))
}

// ForgetAuth throws away the token that authorization holds.
func (s *Drafts) ForgetAuth(ctx context.Context, d domain.Draft) {
	s.auth.Forget(authToApply(d.Auth, s.above(ctx, d.ID).Auth))
}

// AuthSchemes is every way a request can authorize itself, in the order the window draws them.
func (s *Drafts) AuthSchemes() []domain.Scheme { return domain.AuthSchemes() }

// FormatBody re-indents a JSON or XML body, or answers with the text it was given when the body
// does not parse.
func (s *Drafts) FormatBody(kind domain.BodyKind, text string, indent domain.Indent) string {
	return FormatBody(kind, text, indent)
}

// PickBodyFile asks for the file a body carries and answers with its path, or with nothing when
// the dialog was closed. The filter is deliberately wide: a form field can be any file.
func (s *Drafts) PickBodyFile(title string, kindName string) (string, error) {
	return s.host.OpenFile(title, application.FileFilter{DisplayName: kindName, Pattern: "*"})
}

// above is what the levels around a saved request answer. A tab's draft is in no tree.
func (s *Drafts) above(ctx context.Context, id domain.DraftID) workspace.Above {
	if domain.IsTabDraft(id) || s.collections == nil {
		return workspace.Above{}
	}
	above, err := s.collections.Above(ctx, id)
	if err != nil {
		return workspace.Above{}
	}
	return above
}

// FromNode is a saved request seen as something to edit. A node with no authorization of its own
// inherits, and "Inherit" is how a draft says so.
func FromNode(node domain.CollectionNode) domain.Draft {
	auth := domain.Auth{Type: domain.AuthInherit}
	if node.Auth != nil {
		auth = *node.Auth
	}
	return domain.Draft{
		ID:            domain.DraftID(node.ID),
		Method:        node.Method,
		URL:           node.URL,
		Params:        domain.OrEmpty(node.Params),
		Headers:       domain.OrEmpty(node.Headers),
		PathParams:    domain.OrEmpty(node.PathParams),
		Body:          node.Body,
		BodyKind:      domain.KindOf(node.BodyKind),
		Form:          domain.OrEmpty(node.Form),
		BodyFile:      node.BodyFile,
		Auth:          auth,
		EnvironmentID: node.EnvironmentID,
	}
}

// nodeFromDraft is the other direction. "Inherit" and "None" are stored as the absence of an
// answer, which domain.Auth.Stored decides.
func nodeFromDraft(node domain.CollectionNode, d domain.Draft) domain.CollectionNode {
	node.Method = strings.TrimSpace(d.Method)
	node.URL = d.URL
	node.Params = d.Params
	node.Headers = d.Headers
	node.PathParams = d.PathParams
	node.Body = d.Body
	node.BodyKind = domain.KindOf(d.BodyKind)
	node.Form = d.Form
	node.BodyFile = d.BodyFile
	node.Auth = d.Auth.Stored()
	node.EnvironmentID = d.EnvironmentID
	return node
}

// withRowIDs gives every row an id: the window addresses a row by its id, and a request that came
// from a file or from a record has rows without one.
func (s *Drafts) withRowIDs(d domain.Draft) domain.Draft {
	d.Params = s.rowIDs(domain.OrEmpty(d.Params))
	d.Headers = s.rowIDs(domain.OrEmpty(d.Headers))
	d.PathParams = s.rowIDs(domain.OrEmpty(d.PathParams))
	d.Form = domain.OrEmpty(d.Form)
	for i := range d.Form {
		if d.Form[i].ID == "" {
			d.Form[i].ID = s.ids()
		}
	}
	return d
}

func (s *Drafts) rowIDs(rows []domain.Row) []domain.Row {
	for i := range rows {
		if rows[i].ID == "" {
			rows[i].ID = s.ids()
		}
	}
	return rows
}

// rowsFromHeaders turns header pairs into enabled rows with ids.
func (s *Drafts) rowsFromHeaders(headers []domain.HeaderPair) []domain.Row {
	rows := []domain.Row{}
	for _, header := range headers {
		rows = append(rows,
			domain.Row{ID: s.ids(), Name: header.Name, Value: header.Value, Enabled: true})
	}
	return rows
}

type CookieHeaders interface {
	CookieHeader(ctx context.Context, workspace, rawURL string) (string, error)
}

// NetDefaults is what the engine decides about every request by itself: the address a typed one is
// completed into, the name the app calls itself, and whether the server may compress. The editor
// draws these before a send, so they are read from the engine and not guessed here.
type NetDefaults interface {
	CompleteURL(raw string) string
	WireDefaults() (userAgent string, acceptEncoding bool)
}

type FileSource interface {
	Read(path string) ([]byte, error)
}

type VariableSource interface {
	Missing(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
	SubstituteTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		mask bool,
		envID string,
	) ([]string, error)
	// ReferenceTexts is the same substitution for an export that leaves secret values out: a secret
	// resolves to a reference to itself, its `{{name}}` standing where the value would.
	ReferenceTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
	MaskTexts(
		ctx context.Context,
		above []domain.Variable,
		texts []string,
		envID string,
	) ([]string, error)
}

type AuthMaterializer interface {
	Materialize(ctx context.Context, auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput,
		error)
	Project(auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput, error)
	Obtain(ctx context.Context, auth domain.Auth) error
	Forget(auth domain.Auth)
	Held(auth domain.Auth) domain.AuthToken
}
