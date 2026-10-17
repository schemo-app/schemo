# Data Protection

Schemo protects account transport, refresh tokens and private cloud content.

Local application databases are **not encrypted in full**.

## Current State

### Local Device

SQLite stores application content, request credentials and cookies in plaintext.

Database files use restrictive filesystem permissions where supported, but a copied database remains readable.

Account refresh tokens are not stored in SQLite.

Publication passwords saved on this device also use the operating system credential store.
They are scoped to the installation, server, account and publication, and are not synchronized.
The server keeps only password hashes; passwords created elsewhere or before local saving cannot be recovered.
There is no plaintext fallback if the credential store is unavailable.

They are stored through the operating system credential store:

- macOS Keychain;
- Windows Credential Manager;
- Linux Secret Service.

Access tokens remain in memory.

If secure credential storage is unavailable, the session remains usable until restart and requires sign-in again afterward.

## Transport

Account and cloud connections require verified HTTPS.

Plain HTTP is allowed only for explicit loopback addresses used for local development.

OAuth and discovery endpoints follow the same transport policy.

Account transport is independent from request-editor TLS settings, so disabling certificate verification for a user request cannot weaken Schemo account security.

## Cloud Storage

Private cloud content is encrypted before it is written to PostgreSQL.

Encrypted data includes:

- synchronization payloads;
- publication documents;
- history records and bodies;
- script reports;
- collection runs.

Operational metadata required for filtering, synchronization, quotas and retention remains readable.

The server can decrypt stored content, so this is **encryption at rest**, not end-to-end encryption.

## Secrets

Secret values may be withheld from synchronization.

The server also enforces `secrets:read` and redacts protected values before returning them.

This is access control, not encryption.

Credentials embedded in ordinary URLs, headers, bodies, scripts or examples are not automatically secret merely because they resemble credentials.

## What This Protects

Current protection covers:

- network transport to Schemo account and cloud services;
- persisted account refresh tokens;
- private content in cloud database storage.

It does not protect against:

- compromise of the running cloud service;
- compromise of its encryption keys;
- a copied local SQLite database;
- content already exported or copied elsewhere.

## Local Database Encryption

Full local database encryption is not implemented.

If it becomes a product requirement, it should be evaluated as a database-level solution rather than adding ad hoc encryption to individual repositories or fields.

That evaluation must include:

- WAL and temporary files;
- backups and restore;
- search;
- all supported desktop platforms.

## Status

Implemented:

- verified account/cloud transport;
- OS credential storage for refresh tokens;
- encrypted sync payloads;
- encrypted publications and history;
- encryption-key rotation.

Remaining:

- backup-restore verification;
- decision on full local database encryption.
