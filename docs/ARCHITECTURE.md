# Architecture

Schemo is one Go binary (Wails v3) with an embedded Vue 3 window. Go owns durable state, networking,
secrets and every rule that changes stored or sent data. Vue owns rendering, the request being edited
and view state.

## Glossary

| Term | Meaning |
|---|---|
| Workspace | Personal or team; everything below belongs to one. Code never shortens it to space |
| Collection | Saved requests; a nested collection is a folder |
| Request | A saved HTTP request (a node of a collection) |
| Draft | The request being edited in a tab, saved or not |
| Environment, variable | Named values for `{{tokens}}`; globals belong to the workspace |
| Record | One sent request with its response, in history |
| Run | A collection or folder sent request by request |
| Account, session | The cloud sign-in and its devices |

The same words name packages, services, files, stores and components on both sides.

## Go

```text
main.go                 starts the app, or the browser extension's native-messaging relay
internal/
  app/                  assembly: builds every service by hand, binds them, startup and shutdown
  desktop/              windows, menu, tray, deep links, dialogs, updates, platform code
  domain/               entities and rules, standard library only
  db/                   SQLite store, schema, sync registry (dbtest for tests)
  platform/             paths, OS identity, build info, file reading
  workspace/            workspaces, collections, environments, search, settings
  request/              drafts, collection runs, HTTP engine, authorization, cookies, history
  script/               sandbox and script API
  cloud/                account, plan, members, publications, sync, gRPC client, keyring
  agent/                MCP server and browser extension bridge
  convert/              import and export; formats in subpackages (har, insomnia, openapi,
                        postman, schemofile, command, dotenv)
  lens/                 response reading
pkg/jsonpointer         no Schemo imports
```

### Rules

- A feature is one package with services named after what they serve (`Collections`, `Drafts`,
  `Runs`, `Sync`), built by `NewX`. Wails binds them; Go-only exported methods carry
  `//wails:ignore`. Edits take a patch (`Collections.Update(id, patch)`) rather than a method per
  field.
- Files are one or two plain words (`run.go`, `sync_queue.go`); the package comment sits at the
  top of the package's main file (no `doc.go`).
- No DI container and no port layer. `internal/app/wire.go` reads top to bottom.
- Interfaces only at real boundaries: the cloud server, the OS keyring, the clock, and where a
  feature needs another feature without importing it.
- Features take `*db.Store` directly. Transactions, outbox entries and the `changed` event are
  written by the store, so a write cannot forget to sync or to tell the window.
- One error vocabulary: `domain` refusal codes with arguments; the window words them from the
  catalogue (`errors.codes.<code>`).
- `domain` imports only the standard library; `pkg/` imports nothing from Schemo.

### Storage

Everything a workspace edits and syncs as a whole (collections, requests, environments, variables,
cookies, drafts) is a row of `entities`: the columns that are queried and ordered by, and the rest of
the value as JSON in `data`. History, runs, publications and the sync queue keep their own tables.
Secret variable values never enter `data`; masking happens once, on read.

Sync is one engine over a registry of kinds (`db/sync_data.go`): each kind names its
parent, its natural key and which fields are secret. See [DATA.md](DATA.md).

## Frontend

```text
frontend/src/
  main.ts, App.vue  the main window; windows/ holds Settings, About and Update with their entries
  components/       .vue only, by area: layout, request, response, collections, environments, …
    ui/             reka-ui based primitives: Button, Dialog, Sheet, Tooltip, …
  composables/      use<Thing>.ts, one composable per file
  stores/           Pinia stores, use<Entities>Store in <entities>.ts; content.ts holds open tabs
  lib/              TS without components: address, variables, JSON, formats, commands
  i18n/             t(), en.json (reference) and translations
```

- Each folder holds one kind of file, so a fix in environments touches
  `components/environments/`, `stores/environments.ts` and `internal/workspace/environment.go`.
- Components are PascalCase `<Entity><Role>.vue` (`CookieDetailPane`, `EnvironmentsSheet`). The
  window's single-instance parts start with `App` (`AppHeader`, `AppSidebar`, `AppStatusBar`);
  only `ui/` primitives keep one-word names. A store instance is named after its store
  (`const environments = useEnvironmentsStore()`). A composable gets its own file only when two
  components use it; otherwise it lives in the component.
- Generated bindings (`frontend/bindings`) are the only types shared with Go. Do not hand-copy Go
  models.
- Go publishes one `changed {kind, workspace}` event after every entity write, whoever made it: the
  window, MCP, sync or the extension. Stores reload on it (`defineResource` for a value shared as is,
  as settings are). `composables/useAppEvents.ts` holds the main window's other Go subscriptions.
- The editor owns the draft while it is typed: edits are local and instant. Go saves the draft,
  previews it (final URL, auth headers, missing variables) and sends it.
- Pinia keeps window context and the few cross-component values; component refs keep forms and
  dialogs.
- i18n is `i18n/index.ts`: `t(key, params)` over nested JSON, `{name}` parameters, `|` plural forms
  picked by `Intl.PluralRules`, English fallback. See [TRANSLATIONS.md](TRANSLATIONS.md).
- Use reka-ui for focus, escape, positioning and ARIA. Icons come from `@lucide/vue` via `Icon.vue`.
  Do not add color tokens where the palette in `style.css` covers the need.

## Requests

A request is resolved and sent only in Go: path parameters, variables, inherited authorization
(Bearer, Basic, API key, JWT, AWS SigV4, Digest, OAuth 2.0), cookies and scripts. Scripts run
collection, then folders, then request, in a sandbox with no network, files or timers. Obtained
tokens stay in Go. Sending, previewing, exporting and collection runs use the same resolved form.

## Tests

- Test behavior through a feature's public methods against a migrated temporary SQLite store
  (`dbtest.New(t)`), and pure packages with table tests. Fakes only stand in for the network,
  scripts and the OS.
- Cross-feature scenarios live in `internal/app`: `newDevice(t)` assembles the real services over a
  temporary store and an in-memory keyring, and `newFakeCloud(t)` is an in-process cloud server.
- Never open, reset or migrate the user's working database to verify a change.
- Frontend regressions live in `frontend/tests` (node test runner). `tests/vite.mjs` loads app
  modules through Vite with the modules a test replaces written as stubs.
