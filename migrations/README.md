# Pre-release table baseline

The client schema has one numbered SQL migration per application table. Each file
creates the table, its indexes and any required seed rows. The migration runner
owns `schema_migrations`; it is not an application table.

The baseline replaces the previous development migration history. It is intended
for a fresh database, not an automatic upgrade of an existing SQLite file. Startup
rejects a mismatched ledger before applying pending schema changes. Back up existing
data and prepare an explicit conversion before changing a development database.
Deleting the ledger does not convert a schema and is not a supported workaround.

The `personal` workspace starts with sync version zero, and `sync_queue` contains
its initial create operation. Both are required for the first offline-to-cloud sync.

Tests use temporary databases. They verify the per-table baseline, repeat startup,
foreign keys, outbox behavior and refusal of an older ledger without modifying data.
Do not open, reset or migrate the user's working database to verify a code change.
