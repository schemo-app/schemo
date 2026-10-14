CREATE TABLE jar_domains (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    blocked INTEGER NOT NULL DEFAULT 0 CHECK (blocked IN (0, 1)),
    written_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER
);

CREATE UNIQUE INDEX jar_domains_workspace_domain_uq
    ON jar_domains(workspace_id, domain)
    WHERE deleted_at IS NULL;
