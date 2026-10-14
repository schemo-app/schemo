CREATE TABLE jar_cookies (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '/',
    expires_at INTEGER,
    secure INTEGER NOT NULL DEFAULT 0 CHECK (secure IN (0, 1)),
    http_only INTEGER NOT NULL DEFAULT 0 CHECK (http_only IN (0, 1)),
    same_site TEXT NOT NULL DEFAULT '',
    host_only INTEGER NOT NULL DEFAULT 0 CHECK (host_only IN (0, 1)),
    origin TEXT NOT NULL CHECK (origin IN ('response', 'manual')),
    written_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER
);

CREATE INDEX jar_cookies_domain
    ON jar_cookies(workspace_id, domain, host_only, path)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX jar_cookies_uq
    ON jar_cookies(workspace_id, domain, path, name)
    WHERE deleted_at IS NULL;
