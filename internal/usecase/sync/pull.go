package sync

import (
	"context"
	"log"

	"schemo/internal/domain"
)

// Reading what the other devices did. The cursor is the server's own number and never a clock, and
// the page it comes back with is applied whole, in one transaction, with the cursor moved inside
// it: a batch half applied with the cursor already past it would lose whatever came between.
//
// There is no subscription here. A stream is for a device somebody is watching — it makes another
// device's edit arrive within the second rather than within the interval — and it is the window's
// business, which is why the engine's own round is a pull and nothing more.
//
// The space is named on the wire by the id the server knows it by — RemoteID — and never by the id
// it has here. The two are ids of two different rows, and a call naming the local one asks about a
// space that does not exist there: the server answers "not found", and a device that has ever been
// linked would find nothing but that answer. Every request in this package is named the same way;
// what this device keeps is named the other.

// maxPages bounds one round's reading. A space with a year of history in it is a space whose first
// pull is thousands of rows, and a round that never ended would hold the loop for as long as the
// space is long. What is left over is read by the next round, from the cursor it has reached.
const maxPages = 50

// pull reads what the space missed, until there is nothing more or the round has had enough.
func (u *UseCase) pull(ctx context.Context, caller domain.Caller, space domain.SyncSpace) error {
	cursor := space.Cursor
	started := false

	for page := 0; page < maxPages; page++ {
		answer, err := u.server.Pull(ctx, caller, space.RemoteID, cursor, pullLimit)
		if err != nil {
			return err
		}

		if answer.Resync {
			// The server cannot answer from where this device stands: the space was reset there, or
			// this device is so far behind that the changes it missed are gone. Starting over is the
			// only road left, and it is the same one a person's own resync takes.
			//
			// Once per round: a server that says the same thing from a cursor of zero is not a server
			// this device can get anywhere with, and a loop that cleared its queue every time round
			// would be a loop that emptied the outbox for nothing.
			if started {
				return domain.Refuse(domain.CodeServerRefused, nil,
					domain.Args{"reason": "resync"})
			}
			started = true
			if err := u.store.Reset(ctx, space.ID); err != nil {
				return err
			}
			cursor = 0
			continue
		}

		if len(answer.Changes) > 0 {
			// The page arrives named by the server's own id and lands in this device's own space, so
			// each change is renamed before it is applied: what the store hands on is the space as this
			// device knows it, the same way the live half does it.
			for at := range answer.Changes {
				answer.Changes[at].SpaceID = space.ID
			}
			// A row the store could not write is dropped rather than allowed to stop the page: the
			// contract's own instruction is to skip an entity this build does not understand and keep
			// the cursor moving, and a device that stopped syncing over somebody else's row would be a
			// device whose own work stops too. This is the one thing here that is lost quietly, and it
			// is lost on the side that did not make it.
			skipped, err := u.store.ApplyPage(ctx, space.ID, answer.Changes, answer.Cursor)
			if err != nil {
				return err
			}
			if skipped > 0 {
				log.Printf("[sync] %d changes of %s were not applied", skipped, space.ID)
			}
			u.exchanged(0, len(answer.Changes)-skipped)
			// Something another device did is here now, and the windows have something to redraw.
			u.arrived()
		}

		cursor = answer.Cursor
		if !answer.HasMore {
			break
		}
	}
	return nil
}
