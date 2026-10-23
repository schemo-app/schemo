# Data, sync and protection

## Local data

Everything lives in `~/.schemo` (or `$SCHEMO_DATA_DIR`): the SQLite database `app.db`, the browser
extension socket, and on Windows the native-messaging manifests and the web view profile.

SQLite is **not encrypted**. It holds request content, inline credentials and cookies in plaintext,
with restrictive file permissions where the platform has them; a copied database is readable.

The OS credential store (macOS Keychain, Windows Credential Manager, Linux Secret Service) holds:

- the account refresh token. Access tokens stay in memory. There is no SQLite fallback: without a
  credential store the session lasts until restart.
- publication passwords saved on this device, scoped to installation, server, account and
  publication, never synced.

Secret variable values never appear in ordinary DTOs, logs, search or collection exports. An explicit
request export may include credentials, because it exports the complete request. MCP rules are in
[MCP.md](MCP.md).

## Sync

Linked workspaces sync with the server over `sync/v1` (content) and `history/v1` (records and
collection runs). The client owns local storage and the engine; the server owns authorization,
versions and conflicts.

What syncs: collections and requests, environments and variables, cookies, workspace metadata,
history and runs. What stays: settings, drafts, window session, local retention.

### Engine

- **One registry.** Every synced kind is one entry (`syncKinds` in `db/sync_data.go`):
  its parent kind, whether the parent is required, its natural key, its secret fields. Queueing,
  pushing, applying, versions and tombstones are written once for all kinds.
- **Outbox.** A local write and its `sync_queue` row commit in the same transaction. The queue
  references the entity, not a copy, so the latest row state is what goes out. Pulled changes never
  enter the queue.
- **Versions.** Each entity carries the last server version the device knows. A push succeeds while
  that base still matches; otherwise the server keeps its version and returns it to the losing
  device. Conflicts never become client state.
- **Deletions** are tombstones. Retention pruning is local and never propagates; a person deleting
  synced data does.
- **Live updates.** A linked workspace keeps a stream open that only says newer content or history
  exists. Data still arrives through the normal pull, so there is one apply path; a missed signal is
  caught by the next round.
- **Cursors** advance in the same transaction as the data they cover. Retries are idempotent.

### Secrets and permissions

The wire tells an empty secret from a withheld one, so a device without access cannot overwrite a
value it never saw. The server decides who reads secrets (`secrets:read`) and redacts before
answering; that is access control, not encryption. A device can stop sending secret values and run
results; that affects future uploads only.

Read-only workspaces accept remote updates and push nothing shared. The server enforces roles and
plan limits on every call.

History keeps the server's retention; a device keeps a smaller window and loads older records on
demand without adding them to its synced window.

## Transport and cloud storage

Account and cloud connections require verified HTTPS; plain HTTP only for explicit loopback during
development. This is independent of the request editor's TLS settings, so turning verification off
for a user request never weakens account security.

The server encrypts private content before it reaches PostgreSQL: sync payloads, publications,
history records and bodies, script reports, collection runs. Metadata needed for filtering, sync,
quotas and retention stays readable. The server can decrypt it: this is **encryption at rest**, not
end-to-end. Keys can be rotated.

Not protected against: a compromised running service or its keys, a copied local database, content
already exported. Full local database encryption is not implemented; if it is ever needed it should
be database-level (WAL, temp files, backups, search, every platform), not per-field.
