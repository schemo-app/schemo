package collection

import (
	"context"
	"fmt"

	"schemo/internal/domain"
)

// Send sends one saved request — not a run of everything under it — the way the window's own send
// does: the levels above it resolved, its environment honored, and every secret left as its mask in
// what is written down. The workspace is the caller's answer and not this package's: the MCP server
// sends requests of spaces the window is not showing, and reading the scope here would file the
// record under whichever one happens to be on screen. The agent is the name the record is marked
// with, and it is empty when the person is the one sending.
func (u *UseCase) Send(
	ctx context.Context,
	workspace, nodeID, agent string,
) (domain.Record, error) {
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return domain.Record{}, err
	}
	node, err := u.store.Node(ctx, nodeID)
	if err != nil {
		return domain.Record{}, err
	}
	// The walk is the run's own, from the root: a request inherits from every level between it and
	// the top, and starting it at its own collection would send it without them.
	items, err := requestsUnder(tree, node.CollectionID, nodeID)
	if err != nil {
		return domain.Record{}, err
	}
	if len(items) != 1 {
		return domain.Record{}, fmt.Errorf("node %s: %w", nodeID, domain.ErrNotFound)
	}
	// The walk answers what the levels above say; the node that goes out is the one read whole —
	// the tree carries the method and nothing else.
	return u.sender.Send(ctx, requestFrom(workspace, node, items[0].auth, items[0].variables, "",
		agent))
}
