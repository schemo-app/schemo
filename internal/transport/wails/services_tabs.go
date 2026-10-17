package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/tabstrip"
)

// TabsService is the strip's surface towards the window, and the place the strip meets the drafts
// it points at and the records its request tabs are seeded from. Joining them is this layer's job
// and no feature's: the strip keeps what is open, the draft keeps what is written in it, and a
// closed tab's draft is deleted here because the strip itself never touches the drafts table.
type TabsService struct {
	tabs    *tabstrip.UseCase
	drafts  *draft.UseCase
	records *record.UseCase
	nodes   nodeSource
}

// nodeSource is the collections feature as the strip's copies need it: a tab editing a saved
// request is duplicated from the node, which the window does not hold.
type nodeSource interface {
	Node(ctx context.Context, id string) (domain.CollectionNode, error)
}

func NewTabsService(
	tabs *tabstrip.UseCase,
	drafts *draft.UseCase,
	records *record.UseCase,
	nodes nodeSource,
) *TabsService {
	return &TabsService{tabs: tabs, drafts: drafts, records: records, nodes: nodes}
}

// Strip is what is open, which tab is on screen, and the drafts that fell out of the strip since
// the last answer.
func (s *TabsService) Strip(ctx context.Context) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Strip(ctx)
	return s.settled(ctx, stripped, err)
}

// Open opens a tab for the thing named. A request tab seeded from a history record — one sent by
// hand or taken from the capture — gets its draft here first: laying a record out as a request is
// what opening a record has always meant, and the window is not the side that holds the record.
func (s *TabsService) Open(
	ctx context.Context,
	in tabstrip.OpenInput,
) (tabstrip.Strip, error) {
	if in.Kind == domain.TabKindRequest && in.Record != "" && in.DraftID == "" {
		draftID, err := s.tabs.MintDraftID(ctx)
		if err != nil {
			return tabstrip.Strip{}, err
		}
		if err := s.seedFromRecord(ctx, draftID, in.Record); err != nil {
			return tabstrip.Strip{}, err
		}
		in.DraftID = draftID
	}
	stripped, err := s.tabs.Open(ctx, in)
	return s.settled(ctx, stripped, err)
}

// EditRecord opens an independent editable copy of a history request.
func (s *TabsService) EditRecord(ctx context.Context, recordID string) (tabstrip.Strip, error) {
	draftID, err := s.tabs.MintDraftID(ctx)
	if err != nil {
		return tabstrip.Strip{}, err
	}
	if err := s.seedFromRecord(ctx, draftID, recordID); err != nil {
		return tabstrip.Strip{}, err
	}
	stripped, err := s.tabs.Open(ctx, tabstrip.OpenInput{
		Kind: domain.TabKindRequest, DraftID: draftID, NewTab: true,
	})
	if err != nil {
		_ = s.drafts.Discard(ctx, draftID)
	}
	return s.settled(ctx, stripped, err)
}

// New appends a blank request tab at the end and shows it.
func (s *TabsService) New(ctx context.Context) (tabstrip.Strip, error) {
	stripped, err := s.tabs.New(ctx)
	return s.settled(ctx, stripped, err)
}

// Activate shows a tab.
func (s *TabsService) Activate(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Activate(ctx, id)
	return s.settled(ctx, stripped, err)
}

// Close closes a tab and shows its neighbour; the tab's draft goes with it.
func (s *TabsService) Close(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Close(ctx, id)
	return s.settled(ctx, stripped, err)
}

// CloseOthers closes every tab but the named one and the pinned ones.
func (s *TabsService) CloseOthers(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.CloseOthers(ctx, id)
	return s.settled(ctx, stripped, err)
}

// CloseRight closes every tab after the named one, keeping the pinned ones.
func (s *TabsService) CloseRight(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.CloseRight(ctx, id)
	return s.settled(ctx, stripped, err)
}

// Pin pins or unpins a tab.
func (s *TabsService) Pin(ctx context.Context, id string, pinned bool) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Pin(ctx, id, pinned)
	return s.settled(ctx, stripped, err)
}

// Keep takes a preview out of the preview slot.
func (s *TabsService) Keep(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Keep(ctx, id)
	return s.settled(ctx, stripped, err)
}

// Move drags a tab onto another's side.
func (s *TabsService) Move(ctx context.Context, id string, to string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Move(ctx, id, to)
	return s.settled(ctx, stripped, err)
}

// Duplicate copies a tab right after itself; a request tab's draft is copied here, under a name of
// its own, because a copy that shared the original's draft would be one request in two tabs.
func (s *TabsService) Duplicate(ctx context.Context, id string) (tabstrip.Strip, error) {
	stripped, err := s.tabs.Strip(ctx)
	if err != nil {
		return tabstrip.Strip{}, err
	}
	draftID, err := s.copyDraft(ctx, tabOf(stripped.Tabs, id))
	if err != nil {
		return tabstrip.Strip{}, err
	}
	stripped, err = s.tabs.Duplicate(ctx, id, draftID)
	return s.settled(ctx, stripped, err)
}

// copyDraft is the draft a copy starts from: the source's own while it is a tab's, and the node's
// when the tab edits a saved request — the window never holds that one.
func (s *TabsService) copyDraft(
	ctx context.Context,
	source *domain.Tab,
) (domain.DraftID, error) {
	if source == nil || source.DraftID == "" {
		return "", nil
	}
	var current domain.Draft
	if domain.IsTabDraft(source.DraftID) {
		read, err := s.drafts.Current(ctx, source.DraftID)
		if err != nil {
			return "", err
		}
		current = read
	} else {
		node, err := s.nodes.Node(ctx, string(source.DraftID))
		if err != nil {
			return "", err
		}
		current = draft.FromNode(node)
	}
	copied, err := s.tabs.MintDraftID(ctx)
	if err != nil {
		return "", err
	}
	if _, err := s.drafts.Open(ctx, draftCopy(current, copied)); err != nil {
		return "", err
	}
	return copied, nil
}

// SetRecord points a tab at the history record it shows.
func (s *TabsService) SetRecord(
	ctx context.Context,
	id string,
	recordID string,
) (tabstrip.Strip, error) {
	stripped, err := s.tabs.SetRecord(ctx, id, recordID)
	return s.settled(ctx, stripped, err)
}

// Adopt points a request tab at the node its draft has just been saved as: the tab keeps its place
// and its pin — it is still the request it was — and the draft row it had is gone, because the
// request now lives as the node.
func (s *TabsService) Adopt(
	ctx context.Context,
	id string,
	nodeID string,
) (tabstrip.Strip, error) {
	before, err := s.tabs.Strip(ctx)
	if err != nil {
		return tabstrip.Strip{}, err
	}
	var was domain.DraftID
	if source := tabOf(before.Tabs, id); source != nil {
		was = source.DraftID
	}
	stripped, err := s.tabs.Adopt(ctx, id, nodeID)
	if err != nil {
		return tabstrip.Strip{}, err
	}
	if was != "" && was != domain.DraftID(nodeID) {
		if err := s.drafts.Discard(ctx, was); err != nil {
			return tabstrip.Strip{}, err
		}
	}
	return stripped, nil
}

// settled answers with the strip and deletes the drafts that fell out of it: a closed tab, or a
// preview a plain open replaced, is where a request stops being composed.
func (s *TabsService) settled(
	ctx context.Context,
	stripped tabstrip.Strip,
	err error,
) (tabstrip.Strip, error) {
	if err != nil {
		return tabstrip.Strip{}, err
	}
	for _, id := range stripped.Discarded {
		if err := s.drafts.Discard(ctx, id); err != nil {
			return tabstrip.Strip{}, err
		}
	}
	stripped.Discarded = nil
	return stripped, nil
}

// seedFromRecord lays a history record into a fresh tab draft: the method, the address, the headers
// and the body the request went out with.
func (s *TabsService) seedFromRecord(
	ctx context.Context,
	id domain.DraftID,
	recordID string,
) error {
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
	for _, header := range rec.RequestHeaders {
		seeded.Headers = append(seeded.Headers,
			domain.Row{Name: header.Name, Value: header.Value, Enabled: true})
	}
	_, err = s.drafts.Open(ctx, seeded)
	return err
}

// draftCopy is a draft under a name of its own: the lists are the copy's, so editing one tab can
// never move a row of the other.
func draftCopy(d domain.Draft, id domain.DraftID) domain.Draft {
	out := d
	out.ID = id
	out.Revision = 0
	out.Params = append([]domain.Row{}, d.Params...)
	out.Headers = append([]domain.Row{}, d.Headers...)
	out.PathParams = append([]domain.Row{}, d.PathParams...)
	out.Form = append([]domain.FormRow{}, d.Form...)
	return out
}

func tabOf(tabs []domain.Tab, id string) *domain.Tab {
	for i := range tabs {
		if tabs[i].ID == id {
			return &tabs[i]
		}
	}
	return nil
}
