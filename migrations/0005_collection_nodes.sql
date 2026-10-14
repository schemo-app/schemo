CREATE TABLE collection_nodes (
    id TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    description TEXT,
    auth_json TEXT,
    scripts_json TEXT,
    method TEXT,
    url TEXT,
    params_json TEXT,
    headers_json TEXT,
    body TEXT,
    body_kind TEXT,
    form_json TEXT,
    body_file TEXT,
    environment_id TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    deleted_at INTEGER,
    responses_json TEXT,
    path_params_json TEXT
);

CREATE INDEX collection_nodes_tree
    ON collection_nodes(collection_id, position)
    WHERE deleted_at IS NULL;
