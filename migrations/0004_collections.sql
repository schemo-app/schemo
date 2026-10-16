CREATE TABLE collections (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id TEXT REFERENCES collections(id) ON DELETE CASCADE,
    auth_json TEXT,
    scripts_json TEXT,
    variables_json TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER
);

CREATE INDEX collections_workspace
    ON collections(workspace_id, ifnull(parent_id, ''), position)
    WHERE deleted_at IS NULL;
