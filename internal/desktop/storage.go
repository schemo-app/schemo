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

// OpenStorage opens the store before services register, since the outcome decides which may. A
// failure is recorded, not returned, to avoid a blank window; the returned func is the safe Retry.
func OpenStorage(store *db.Store, status *Status, dir platform.DataDir) InitFunc {
	init := func(ctx context.Context) error {
		// Without the folder the database cannot be named; the failure screen has no file to point at.
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
