CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('personal', 'team')),
    color TEXT NOT NULL DEFAULT '',
    active_environment_id TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    remote_workspace_id TEXT NOT NULL DEFAULT '',
    remote_account TEXT NOT NULL DEFAULT '',
    link_pending INTEGER NOT NULL DEFAULT 0 CHECK (link_pending IN (0, 1)),
    access_json TEXT NOT NULL DEFAULT ''
);

CREATE INDEX workspaces_remote_account
    ON workspaces(remote_account, remote_workspace_id)
    WHERE remote_account <> '' AND remote_workspace_id <> '';

CREATE UNIQUE INDEX workspaces_remote_id_uq
    ON workspaces(remote_workspace_id)
    WHERE remote_workspace_id <> '';

INSERT INTO workspaces (id, name, kind, created_at, updated_at, version)
VALUES ('personal', '', 'personal', 0, 0, 0);
