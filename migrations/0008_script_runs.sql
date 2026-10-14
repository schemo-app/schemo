CREATE TABLE script_runs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    record_id TEXT REFERENCES records(id) ON DELETE CASCADE,
    node_id TEXT,
    scope TEXT NOT NULL CHECK (scope IN ('pre', 'post')),
    ok INTEGER NOT NULL CHECK (ok IN (0, 1)),
    error TEXT NOT NULL DEFAULT '',
    duration_us INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    logs_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(logs_json) AND json_type(logs_json) = 'array'),
    tests_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tests_json) AND json_type(tests_json) = 'array')
);

CREATE INDEX script_runs_record
    ON script_runs(record_id)
    WHERE record_id IS NOT NULL;

CREATE INDEX script_runs_workspace
    ON script_runs(workspace_id, created_at DESC);
