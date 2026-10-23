-- The whole schema. The app is pre-release: a database with another schema version is moved aside
-- and a fresh one is created (see store.go).

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

-- Everything a workspace holds that is edited and synced as a whole: collections, requests,
-- environments, variables, cookies, cookie-jar domains and drafts. The columns are what is queried
-- and ordered by; the rest of the value is the JSON in `data`.
CREATE TABLE entities (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    parent_id TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    -- A natural key that must be unique among live rows of a kind (a variable's name in its scope,
    -- a cookie's domain/path/name). Empty when the kind has none.
    key TEXT NOT NULL DEFAULT '',
    data TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data)),
    version INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER
);

CREATE INDEX entities_children
    ON entities(workspace_id, kind, parent_id, position)
    WHERE deleted_at IS NULL;

CREATE INDEX entities_parent
    ON entities(parent_id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX entities_key
    ON entities(workspace_id, kind, key)
    WHERE key <> '' AND deleted_at IS NULL;

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE records (
    seq INTEGER PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('manual', 'browser')),
    agent TEXT NOT NULL DEFAULT '',
    method TEXT NOT NULL,
    url TEXT NOT NULL,
    status INTEGER NOT NULL DEFAULT 0,
    status_text TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    cancelled INTEGER NOT NULL DEFAULT 0 CHECK (cancelled IN (0, 1)),
    duration_us INTEGER NOT NULL DEFAULT 0,
    dns_us INTEGER,
    connect_us INTEGER,
    tls_us INTEGER,
    wait_us INTEGER,
    download_us INTEGER,
    request_bytes INTEGER NOT NULL DEFAULT 0,
    response_bytes INTEGER NOT NULL DEFAULT 0,
    request_headers_json TEXT NOT NULL DEFAULT '[]',
    response_headers_json TEXT NOT NULL DEFAULT '[]',
    cookie_writes_json TEXT NOT NULL DEFAULT '[]',
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    node_id TEXT NOT NULL DEFAULT '',
    tab_id INTEGER,
    tab_title TEXT,
    tab_url TEXT,
    favicon_url TEXT,
    updated_at INTEGER NOT NULL DEFAULT 0,
    deleted_at INTEGER,
    remote_seq INTEGER NOT NULL DEFAULT 0 CHECK (remote_seq >= 0),
    is_remote INTEGER NOT NULL DEFAULT 0 CHECK (is_remote IN (0, 1)),
    cached_at INTEGER,
    run_id TEXT
);

CREATE INDEX records_node
    ON records(node_id, started_at DESC)
    WHERE node_id != '' AND deleted_at IS NULL;

CREATE INDEX records_recent
    ON records(workspace_id, started_at DESC, seq DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX records_source
    ON records(workspace_id, source, started_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX records_tab
    ON records(workspace_id, tab_id, started_at DESC)
    WHERE tab_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX records_url
    ON records(workspace_id, url)
    WHERE deleted_at IS NULL;

CREATE TABLE record_bodies (
    record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    side TEXT NOT NULL CHECK (side IN ('request', 'response')),
    content BLOB,
    size INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    PRIMARY KEY (record_id, side)
) WITHOUT ROWID;

CREATE TABLE script_runs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    record_id TEXT REFERENCES records(id) ON DELETE CASCADE,
    node_id TEXT,
    scope TEXT NOT NULL CHECK (scope IN ('pre', 'post')),
    ok INTEGER NOT NULL CHECK (ok IN (0, 1)),
    error TEXT NOT NULL DEFAULT '',
    duration_us INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    logs_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(logs_json) AND json_type(logs_json) = 'array'),
    tests_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tests_json) AND json_type(tests_json) = 'array')
);

CREATE INDEX script_runs_record
    ON script_runs(record_id)
    WHERE record_id IS NOT NULL;

CREATE INDEX script_runs_workspace
    ON script_runs(workspace_id, created_at DESC);

CREATE TABLE collection_runs (
    id TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    passed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    duration_us INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX collection_runs_collection
    ON collection_runs(collection_id, started_at DESC);

CREATE TABLE collection_run_results (
    run_id TEXT NOT NULL REFERENCES collection_runs(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    status INTEGER,
    ok INTEGER NOT NULL CHECK (ok IN (0, 1)),
    duration_us INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    record_id TEXT,
    assertions_passed INTEGER NOT NULL DEFAULT 0,
    assertions_total INTEGER NOT NULL DEFAULT 0,
    failure TEXT NOT NULL DEFAULT '',
    skipped INTEGER NOT NULL DEFAULT 0 CHECK (skipped IN (0, 1)),
    PRIMARY KEY (run_id, position)
) WITHOUT ROWID;

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

