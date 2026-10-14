CREATE TABLE tabs (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    kind TEXT NOT NULL,
    ref TEXT NOT NULL DEFAULT '',
    draft_id TEXT NOT NULL DEFAULT '',
    record_id TEXT NOT NULL DEFAULT '',
    pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    preview INTEGER NOT NULL DEFAULT 0 CHECK (preview IN (0, 1)),
    position INTEGER NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
    PRIMARY KEY (workspace_id, id)
) WITHOUT ROWID;

CREATE INDEX tabs_workspace_position
    ON tabs(workspace_id, position);

CREATE UNIQUE INDEX tabs_active ON tabs(workspace_id) WHERE active = 1;
CREATE UNIQUE INDEX tabs_preview ON tabs(workspace_id) WHERE preview = 1;
