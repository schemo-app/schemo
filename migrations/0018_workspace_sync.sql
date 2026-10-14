CREATE TABLE workspace_sync (
    workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'local'
    CHECK (state IN ('local', 'synced', 'paused')),
    last_sync_seq INTEGER NOT NULL DEFAULT 0 CHECK (last_sync_seq >= 0),
    last_history_seq INTEGER NOT NULL DEFAULT 0 CHECK (last_history_seq >= 0),
    last_synced_at INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0)
) WITHOUT ROWID;
