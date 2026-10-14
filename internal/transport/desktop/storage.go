package desktop

import (
	"context"
	"fmt"
	"log"

	"schemo/internal/infra/sqlite"
	"schemo/internal/platform"
	"schemo/migrations"
)

// InitFunc is the part of startup the frontend can retry: open the database and bring the schema
// up to date.
type InitFunc func(ctx context.Context) error

// OpenStorage builds that step and runs it once, here in the graph rather than in a lifecycle
// hook, because the outcome decides which services may be registered — and registration happens
// before the app runs. A failure is recorded rather than returned: returning it from a service's
// start-up aborts the run before any window exists, which is the blank window this avoids.
//
// The returned function is what the failure screen's "Retry" calls; making the folder, opening and
// migrating are all repeatable, so a retry after a locked folder or a full disk is safe.
func OpenStorage(store *sqlite.Store, status *Status, dir platform.DataDir) InitFunc {
	init := func(ctx context.Context) error {
		// The folder first: this is where startup stops on a locked path or one a policy redirected,
		// and until it exists the database cannot even be named. It is also the one refusal with no
		// file to point at and no folder to open, so the screen says less for it and offers less —
		// see the failure screen in App.vue.
		if err := dir.Create(); err != nil {
			status.Fail(FailureDataDir, err.Error())
			return err
		}
		if err := store.Open(ctx); err != nil {
			status.Fail(FailureDatabase, err.Error())
			return err
		}
		res, err := store.Migrate(ctx, migrations.FS)
		if err != nil {
			status.Fail(FailureMigration, err.Error())
			return err
		}
		if len(res.Applied) > 0 {
			applied := make([]string, 0, len(res.Applied))
			for _, m := range res.Applied {
				applied = append(applied, fmt.Sprintf("%04d_%s", m.Version, m.Name))
			}
			log.Printf("[storage] applied %d migration(s): %v", len(applied), applied)
		}
		status.Succeed()
		return nil
	}

	_ = init(context.Background())
	return init
}
