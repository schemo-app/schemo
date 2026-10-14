CREATE TABLE sync_queue (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('workspace', 'collection', 'request', 'environment', 'variable', 'cookie', 'jar_domain', 'record', 'run')),
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create', 'update', 'delete')),
    operation_id TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'sending', 'failed', 'parked')),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    next_retry_at INTEGER,
    created_at INTEGER NOT NULL,
    offered INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX sync_queue_fetch
    ON sync_queue(workspace_id, status, next_retry_at, created_at);

CREATE UNIQUE INDEX sync_queue_waiting
    ON sync_queue(workspace_id, entity_type, entity_id)
    WHERE status <> 'sending';

INSERT INTO sync_queue (id, workspace_id, entity_type, entity_id, action, operation_id, created_at)
VALUES (lower(hex(randomblob(16))), 'personal', 'workspace', 'personal', 'create', lower(hex(randomblob(16))), 0);
