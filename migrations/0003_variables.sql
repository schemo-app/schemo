CREATE TABLE variables (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('environment', 'globals')),
    scope_id TEXT,
    name TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('text', 'secret')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    position INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    CONSTRAINT variables_scope_check CHECK (
    (scope_kind = 'globals' AND scope_id IS NULL) OR
    (scope_kind = 'environment' AND scope_id IS NOT NULL)
)
);

CREATE INDEX variables_lookup
    ON variables(workspace_id, scope_kind, ifnull(scope_id, ''), enabled, position)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX variables_scope_name_uq
    ON variables(workspace_id, scope_kind, ifnull(scope_id, ''), name)
    WHERE deleted_at IS NULL;
