CREATE TABLE collection_runs (
    id TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    passed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    duration_us INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX collection_runs_collection
    ON collection_runs(collection_id, started_at DESC);
