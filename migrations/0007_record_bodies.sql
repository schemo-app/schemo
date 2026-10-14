CREATE TABLE record_bodies (
    record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    side TEXT NOT NULL CHECK (side IN ('request', 'response')),
    content BLOB,
    size INTEGER NOT NULL DEFAULT 0,
    truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    PRIMARY KEY (record_id, side)
) WITHOUT ROWID;
