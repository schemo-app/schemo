CREATE TABLE plan (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    tier TEXT NOT NULL DEFAULT '',
    seats INTEGER NOT NULL DEFAULT 0 CHECK (seats >= 0),
    plan_until INTEGER NOT NULL DEFAULT 0 CHECK (plan_until >= 0),
    features TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(features)),
    quotas TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(quotas)),
    meters TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(meters))
) WITHOUT ROWID;
