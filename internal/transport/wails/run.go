package wails

import (
	"context"
	"slices"

	"schemo/internal/domain"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/scripting"
)

// collectionSender sends a run's request the way the command line does: through the draft, which
// fills in the `{{tokens}}` and keeps a secret out of what is written down, and then through the
// history. It is why the run feature knows about neither.
type collectionSender struct {
	drafts  *draft.UseCase
	records *record.UseCase
	scripts *scripting.UseCase
}

var _ collection.Sender = collectionSender{}

func (s collectionSender) Send(
	ctx context.Context,
	req collection.RunRequest,
) (domain.Record, error) {
	// The pin travels with the seed, so a request saved against one environment is resolved against
	// it wherever it is sent from — the run is not looking at the tree, and the window may well be on
	// a different environment by the time the twentieth request goes out.
	prepared, err := s.drafts.Prepare(ctx, draft.Seed{
		Method:        req.Method,
		URL:           req.URL,
		Body:          req.Body,
		BodyKind:      req.BodyKind,
		Form:          req.Form,
		BodyFile:      req.BodyFile,
		Headers:       req.Headers,
		Auth:          req.Auth,
		EnvironmentID: req.EnvironmentID,
	}, append(slices.Clone(req.Variables), s.scripts.RunVariables(req.Run)...))
	if err != nil {
		return domain.Record{}, err
	}

	input := recordInput(prepared)
	// The scripts of everything above this request run around it, and they are found by where it came
	// from: the node, and the run whose own variables they share.
	input.Node, input.Run = req.NodeID, req.Run
	// Who is sending travels into the record: an empty name is the person, and a name is the agent
	// history marks the send with.
	input.Agent = req.Agent
	// The space the run resolved travels with each request rather than being read again here: a run
	// of fifty requests must not scatter them across two spaces.
	return s.records.SendAndWait(ctx, req.Workspace, input)
}
