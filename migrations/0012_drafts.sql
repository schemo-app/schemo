CREATE TABLE drafts (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 0,
    method TEXT NOT NULL DEFAULT 'GET',
    url TEXT NOT NULL DEFAULT '',
    params_json TEXT NOT NULL DEFAULT '[]',
    headers_json TEXT NOT NULL DEFAULT '[]',
    auth_json TEXT NOT NULL DEFAULT '{"type":"none"}',
    body TEXT NOT NULL DEFAULT '',
    body_kind TEXT NOT NULL DEFAULT 'raw',
    form_json TEXT NOT NULL DEFAULT '[]',
    body_file TEXT NOT NULL DEFAULT '',
    scripts_json TEXT,
    updated_at INTEGER NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '',
    path_params_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (workspace_id, id)
) WITHOUT ROWID;

CREATE INDEX drafts_workspace_updated
    ON drafts(workspace_id, updated_at DESC);
