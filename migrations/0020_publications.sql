CREATE TABLE publications (
    collection_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL DEFAULT '',
    publication_id TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL DEFAULT '',
    public_url TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT NOT NULL DEFAULT '',
    environment_name TEXT NOT NULL DEFAULT '',
    can_manage INTEGER NOT NULL DEFAULT 0 CHECK (can_manage IN (0, 1)),
    pending_unpublish INTEGER NOT NULL DEFAULT 0 CHECK (pending_unpublish IN (0, 1)),
    updated_at INTEGER NOT NULL,
    outline TEXT NOT NULL DEFAULT '',
    changes TEXT NOT NULL DEFAULT '',
    options TEXT NOT NULL DEFAULT 'null'
);

CREATE INDEX publications_workspace
    ON publications(workspace_id, status);
