# Schemo

Desktop HTTP client built with Wails v3, Go and Vue 3.

Go owns durable state, networking, secrets and domain rules. Vue owns rendering, interaction and view state.

Do not duplicate persistence, request execution or authorization logic in the frontend.

## Before Editing

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

It defines dependency boundaries and the normal feature flow:

```text
domain → usecase → infra / transport → frontend
```

Architecture boundaries are enforced by tests.

## Commands

```bash
task dev
task build
task test
task generate:bindings
task web:typecheck
```

Run `task generate:bindings` after changing Wails-bound Go methods.

Use `task --list` for other commands.

## Project Rules

- Prefer simple ownership over extra layers and interfaces.
- Use standard Go naming and `gofmt`.
- Comments explain non-obvious behavior and constraints.
- Keep business rules in Go and presentation logic in Vue.
- Test observable behavior.
- Use temporary SQLite databases for persistence, migration and transaction tests.
- Keep frontend regressions in `frontend/tests`.

### Design

The current design handoff lives in:

```text
../handoff
```

Use it as the source of truth for layout and dimensions.

Do not introduce new color tokens or handwritten icons where existing primitives already cover the need.

### Secrets

Local application data is stored in SQLite and is not fully encrypted.

Account refresh tokens use the operating system credential store and have no SQLite fallback.

Secrets must not appear in normal DTOs, logs or collection exports. Explicit request export may include request credentials because it exports the complete request.

See [Data Protection](docs/ENCRYPTION.md) and [Sync](docs/SYNC.md).

## Repository Map

| Area | Location |
|---|---|
| Database | `platform.DataDir()` / `app.db` |
| Migrations | `migrations/` |
| Architecture | `docs/ARCHITECTURE.md` |
| Design | `docs/DESIGN.md` |
| Sync | `docs/SYNC.md` |
| MCP | `docs/MCP.md` |
| Data protection | `docs/ENCRYPTION.md` |
| Wails services | `internal/transport/wails/` |
| Desktop integration | `internal/transport/desktop/` |
| Request engine | `internal/infra/httpx/` |
| Script engine | `internal/infra/scriptengine/` |
| Sync engine | `internal/usecase/sync/` |
| Contracts | `github.com/schemo-app/proto` |
| Design handoff | `../handoff` |