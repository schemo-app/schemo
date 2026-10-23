# Schemo

Desktop HTTP client: Go, Wails v3, SQLite, goja; Vue 3 window. Read
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before editing and [docs/DATA.md](docs/DATA.md) before
touching storage, sync or secrets.

## Commands

```bash
task dev                  # Go + Vite with hot reload
task test                 # gofmt, vet, staticcheck, Go tests, frontend checks
task generate:bindings    # after changing a bound Go method or type
task web:test             # frontend regressions
task translations:check   # catalogues
```

`task --list` shows the rest. Go builds on Linux need `-tags gtk3`.

## Rules

- Business rules, persistence, networking and secrets stay in Go; Vue renders and edits.
- One package per feature; services are named after what they serve. Add an interface only at a
  real boundary. Names follow the glossary in docs/ARCHITECTURE.md.
- Never hand-edit `frontend/bindings`; regenerate them.
- Tests use temporary SQLite (`dbtest.New`). Never open, reset or migrate the working database to
  verify a change.
- English in code, comments, test names and fixtures; read localized expectations from the
  catalogues.
- Comments say why and what must stay true, not what the next line does.
- Source CSS stays readable: one declaration per line, a blank line between rules.
- The app has not been publicly released: no backward compatibility for the schema or the protocol.

## Secrets

- Refresh tokens live only in the OS credential store.
- Secret variable values never appear in normal DTOs, logs, search or collection exports.
- MCP masks secret values and never exposes account credentials, keyring entries or its own token.

## Design

The design handoff in `../handoff` is the source of truth for layout and dimensions. Reuse the
palette and `ui/` primitives before adding tokens or icons.
