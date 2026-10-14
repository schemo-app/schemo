CREATE TABLE environments (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT,
    readonly INTEGER NOT NULL DEFAULT 0 CHECK (readonly IN (0, 1)),
    position INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER
);

CREATE INDEX environments_workspace
    ON environments(workspace_id, position)
    WHERE deleted_at IS NULL;
