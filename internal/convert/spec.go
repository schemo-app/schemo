package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/convert/merge"
	"schemo/internal/convert/openapi"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Specs updates a collection from an OpenAPI description. What the description said at the last
// update is kept with the collection; where this device reads it stays here.
type Specs struct {
	collections  SpecCollections
	environments SpecEnvironments
	files        Files
	remote       Remote
	ids          platform.IDGen
	host         *desktop.Host
	mu           sync.Mutex
	staged       *stagedSpec
}

type stagedSpec struct {
	token      string
	workspace  string
	collection string
	source     domain.SpecSource
	document   domain.Document
	at         time.Time
}

func NewSpecs(
	collections SpecCollections,
	environments SpecEnvironments,
	files Files,
	remote Remote,
	ids platform.IDGen,
	host *desktop.Host,
) *Specs {
	return &Specs{
		collections: collections, environments: environments, files: files, remote: remote,
		ids: ids, host: host,
	}
}

// SpecReview is what an update would change, under the token that applies it.
type SpecReview struct {
	Token   string            `json:"token"`
	Source  domain.SpecSource `json:"source"`
	Review  merge.Review      `json:"review"`
	Skipped []domain.Skip     `json:"skipped,omitempty"`
}

// SpecSummary is what an update wrote.
type SpecSummary struct {
	Tree     []domain.Collection `json:"tree"`
	EnvState *domain.EnvState    `json:"envState,omitempty"`
	Added    int                 `json:"added"`
	Updated  int                 `json:"updated"`
	Removed  int                 `json:"removed"`
}

// SpecCollections is the tree as an update reads and writes it.
type SpecCollections interface {
	Open(ctx context.Context) (string, error)
	WholeLevel(ctx context.Context, workspace, id string) (domain.Collection, string, error)
	SpecLink(ctx context.Context, workspace, id string) (domain.SpecLink, bool, error)
	SpecSnapshot(ctx context.Context, id string) (json.RawMessage, error)
	WriteUpdate(ctx context.Context, workspace string, update domain.TreeUpdate) ([]domain.Collection, error)
}

// SpecEnvironments is the environments as an update reads and writes them.
type SpecEnvironments interface {
	Held(ctx context.Context, workspace string) ([]domain.Environment, error)
	WriteBlocks(ctx context.Context, workspace, collection string, drafts []domain.EnvironmentDraft) error
	StateOf(ctx context.Context, workspace string) (domain.EnvState, error)
}

// Linked is the source a collection is updated from; nil when it has none.
func (s *Specs) Linked(ctx context.Context, collectionID string) (*domain.SpecSource, error) {
	workspace, err := s.collections.Open(ctx)
	if err != nil {
		return nil, err
	}
	link, linked, err := s.collections.SpecLink(ctx, workspace, collectionID)
	if err != nil || !linked {
		return nil, err
	}
	return &link.Source, nil
}

// PickFile asks for a description and reviews it; nil when the dialog is closed.
func (s *Specs) PickFile(ctx context.Context, collectionID, title string) (*SpecReview, error) {
	kind := kindOf(FormatOpenAPI)
	path, err := s.host.OpenPath(title, false,
		application.FileFilter{DisplayName: kind.name, Pattern: kind.importPattern()})
	if err != nil || path == "" {
		return nil, err
	}
	review, err := s.ReviewFile(ctx, collectionID, path)
	if err != nil {
		return nil, err
	}
	return &review, nil
}

// ReviewFile reviews the description in a file, picked or dropped.
func (s *Specs) ReviewFile(ctx context.Context, collectionID, path string) (SpecReview, error) {
	return s.review(ctx, collectionID, domain.SpecSource{Kind: domain.SpecFile, Location: path})
}

// ReviewURL reviews the description at an address.
func (s *Specs) ReviewURL(ctx context.Context, collectionID, url string) (SpecReview, error) {
	return s.review(ctx, collectionID, domain.SpecSource{Kind: domain.SpecURL, Location: strings.TrimSpace(url)})
}

// ReviewLinked reads the linked source again and reviews it.
func (s *Specs) ReviewLinked(ctx context.Context, collectionID string) (SpecReview, error) {
	source, err := s.Linked(ctx, collectionID)
	if err != nil {
		return SpecReview{}, err
	}
	if source == nil {
		return SpecReview{}, domain.Refuse(domain.CodeSpecNotLinked, domain.ErrNotFound, nil)
	}
	return s.review(ctx, collectionID, *source)
}

func (s *Specs) review(ctx context.Context, collectionID string, source domain.SpecSource) (SpecReview, error) {
	document, err := s.read(ctx, source)
	if err != nil {
		return SpecReview{}, err
	}
	workspace, err := s.collections.Open(ctx)
	if err != nil {
		return SpecReview{}, err
	}
	input, err := s.input(ctx, workspace, collectionID, document)
	if err != nil {
		return SpecReview{}, err
	}
	token := s.ids()
	s.mu.Lock()
	s.staged = &stagedSpec{token: token, workspace: workspace, collection: collectionID, source: source,
		document: document, at: time.Now()}
	s.mu.Unlock()
	return SpecReview{Token: token, Source: source, Review: merge.Compare(input), Skipped: document.Skipped}, nil
}

func (s *Specs) read(ctx context.Context, source domain.SpecSource) (domain.Document, error) {
	var data []byte
	var err error
	if source.Kind == domain.SpecURL {
		data, err = s.remote.Fetch(ctx, source.Location)
	} else {
		data, err = s.files.Read(source.Location)
	}
	if err != nil {
		return domain.Document{}, err
	}
	return openapi.Import(data)
}

// input reads the collection as it is now beside what the last update recorded, on any device.
func (s *Specs) input(ctx context.Context, workspace, collectionID string, document domain.Document) (merge.Input, error) {
	mine, root, err := s.collections.WholeLevel(ctx, workspace, collectionID)
	if err != nil {
		return merge.Input{}, err
	}
	kept, err := s.collections.SpecSnapshot(ctx, collectionID)
	if err != nil {
		return merge.Input{}, err
	}
	var base *merge.Snapshot
	if len(kept) > 0 {
		base = &merge.Snapshot{}
		if err := json.Unmarshal(kept, base); err != nil {
			return merge.Input{}, fmt.Errorf("reading what the description of %s said: %w", collectionID, err)
		}
	}
	held, err := s.environments.Held(ctx, workspace)
	if err != nil {
		return merge.Input{}, err
	}
	return merge.Input{
		Base: base, Mine: mine, Theirs: document.Collections[0],
		Environments: document.Environments, Held: held, Block: root,
	}, nil
}

// Apply writes the choices made on a review, against the collection as it is now.
func (s *Specs) Apply(ctx context.Context, token string, choices merge.Choices) (SpecSummary, error) {
	staged, err := s.claim(token)
	if err != nil {
		return SpecSummary{}, err
	}
	input, err := s.input(ctx, staged.workspace, staged.collection, staged.document)
	if err != nil {
		return SpecSummary{}, err
	}
	result := merge.Apply(input, choices, s.ids)
	if err := record(&result.Update, staged.collection, staged.source, result.Snapshot); err != nil {
		return SpecSummary{}, err
	}
	tree, err := s.collections.WriteUpdate(ctx, staged.workspace, result.Update)
	if err != nil {
		return SpecSummary{}, err
	}
	summary := SpecSummary{Tree: tree, Added: result.Added, Updated: result.Updated, Removed: result.Removed}
	if len(result.Environments) > 0 {
		if err := s.environments.WriteBlocks(ctx, staged.workspace, input.Block, result.Environments); err != nil {
			return SpecSummary{}, err
		}
		state, err := s.environments.StateOf(ctx, staged.workspace)
		if err != nil {
			return SpecSummary{}, err
		}
		summary.EnvState = &state
	}
	return summary, nil
}

// remember keeps what the description of a collection just imported said, and where it can be read
// again if it came from a file or an address.
func (s *Specs) remember(
	ctx context.Context, collectionID string, source domain.SpecSource, document domain.Document,
) error {
	workspace, err := s.collections.Open(ctx)
	if err != nil {
		return err
	}
	input, err := s.input(ctx, workspace, collectionID, document)
	if err != nil {
		return err
	}
	input.Base = nil
	var update domain.TreeUpdate
	if err := record(&update, collectionID, source, merge.Linked(input)); err != nil {
		return err
	}
	_, err = s.collections.WriteUpdate(ctx, workspace, update)
	return err
}

// record puts into an update what the description said and where this device read it.
func record(
	update *domain.TreeUpdate, collectionID string, source domain.SpecSource, snapshot merge.Snapshot,
) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("keeping what the description said: %w", err)
	}
	source.ReadAt = time.Now().UnixMilli()
	update.Snapshot, update.Link = data, domain.SpecLink{CollectionID: collectionID, Source: source}
	return nil
}

// claim is the review a token names, once, as for an import.
func (s *Specs) claim(token string) (stagedSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.staged == nil || s.staged.token != token || time.Since(s.staged.at) > pendingTTL {
		return stagedSpec{}, domain.Refuse(domain.CodeImportExpired, domain.ErrNotFound, nil)
	}
	staged := *s.staged
	s.staged = nil
	return staged, nil
}
