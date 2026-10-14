# Sync

Schemo synchronizes workspace data with the server through the shared `sync/v1` and `history/v1` contracts.

The client owns local storage and the sync engine. The server owns authorization, versions and conflict resolution.

## Model

The sync engine:

- pushes local changes;
- pulls remote changes;
- listens for live update signals;
- runs only for linked workspaces.

Each workspace is either local or linked to an account.

Local workspaces are created remotely when needed. Remote workspaces are mirrored locally.

## What Syncs

Workspace content includes:

- collections and requests;
- environments and variables;
- cookies;
- workspace metadata;
- history and collection runs.

Some data remains local:

- application settings;
- drafts;
- local session state;
- UI state;
- local retention operations.

## Outbox

Local changes are recorded in `sync_queue`.

The queue stores references to changed entities rather than copies of their payloads. When an entity changes again, the latest row state is sent.

The data change and its queue entry are written in the same transaction.

Pulled changes never enter the outbox.

## Versions and Conflicts

Synced entities carry the last server version known by the device.

A push succeeds when its base version still matches the server.

If another device changed the entity first, the server keeps its current version and returns it to the losing device.

Conflicts are therefore resolved by the server rather than becoming persistent client state.

## Deletions

A synchronized deletion is represented as a tombstone rather than immediately removing the row.

Local maintenance operations such as retention pruning do not propagate as remote deletions.

Explicit user deletion of synchronized data does.

## Secrets

Secret values may sync when enabled.

The wire distinguishes:

- an empty value;
- a value intentionally withheld.

This prevents a device without access to a secret from accidentally overwriting it.

The server decides whether a caller may read secret values.

The client can disable sending secret values while continuing to synchronize their metadata.

## History

History uses its own `history/v1` contract.

Records and collection runs can synchronize independently from workspace entities.

The server retains history according to its policy, while each device may keep a smaller local window.

Older history can be loaded on demand without becoming part of the normal synchronized window.

## Live Updates

Linked workspaces keep a live connection to the server.

The stream carries signals that newer content or history exists. Actual data still arrives through the normal pull path.

This keeps one consistent path for applying changes, versions and cursors.

If live delivery is missed, the next regular sync round catches up.

## Permissions

Workspace permissions come from the server.

Read-only workspaces:

- accept remote updates;
- do not push local shared-content changes;
- may still keep device-local state such as drafts and settings.

Authorization is always enforced again by the server.

## Sync Settings

Users can control whether the device sends:

- secret values;
- run results.

These settings affect future uploads. They do not remove data that was already synchronized.

## Guarantees

The sync engine is designed around a few invariants:

- local writes and outbox entries are atomic;
- pulled changes are never queued back for upload;
- cursors advance in the same transaction as applied data;
- retries are idempotent;
- live updates use the same pull/apply path as periodic sync;
- server authorization remains authoritative.