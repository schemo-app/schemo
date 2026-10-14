CREATE TABLE collection_run_results (
    run_id TEXT NOT NULL REFERENCES collection_runs(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    status INTEGER,
    ok INTEGER NOT NULL CHECK (ok IN (0, 1)),
    duration_us INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    record_id TEXT,
    assertions_passed INTEGER NOT NULL DEFAULT 0,
    assertions_total INTEGER NOT NULL DEFAULT 0,
    failure TEXT NOT NULL DEFAULT '',
    skipped INTEGER NOT NULL DEFAULT 0 CHECK (skipped IN (0, 1)),
    PRIMARY KEY (run_id, position)
) WITHOUT ROWID;
