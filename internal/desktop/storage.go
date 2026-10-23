package desktop

import (
	"context"
	"log"

	"schemo/internal/db"
	"schemo/internal/platform"
)

// InitFunc is the part of startup the frontend can retry: open the database and bring the schema
// up to date.
type InitFunc func(ctx context.Context) error

// OpenStorage builds that step and runs it once, here in the graph rather than in a lifecycle hook,
// because the outcome decides which services may be registered, and registration happens before the
// app runs. A failure is recorded rather than returned: returning it from a service's start-up
// aborts the run before any window exists, which is the blank window this avoids.
//
// The returned function is what the failure screen's "Retry" calls; making the folder, opening and
// migrating are all repeatable, so a retry after a locked folder or a full disk is safe.
func OpenStorage(store *db.Store, status *Status, dir platform.DataDir) InitFunc {
	init := func(ctx context.Context) error {
		// The folder first: startup stops here on a locked path or one a policy redirected, and
		// until it exists the database cannot even be named. It is the one refusal with no file to
		// point at and no folder to open, so the failure screen says and offers less for it.
		if err := dir.Create(); err != nil {
			status.Fail(FailureDataDir, err.Error())
			return err
		}
		if err := store.Open(ctx); err != nil {
			status.Fail(FailureDatabase, err.Error())
			return err
		}
		moved, err := store.Migrate(ctx)
		if err != nil {
			status.Fail(FailureMigration, err.Error())
			return err
		}
		if moved != "" {
			log.Printf("[storage] a database of another schema version was moved to %s", moved)
		}
		status.Succeed()
		return nil
	}

	_ = init(context.Background())
	return init
}
