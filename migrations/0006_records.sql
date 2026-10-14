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
