# Architecture

Schemo keeps application logic in Go. Vue renders the UI and owns view state only.

## Layers

```text
main.go → transport → usecase → domain
              ↓           ↓
             infra ←──────┘

pure packages: command, dotenv, postman, lens, pkg
```

| Layer                        | Responsibility                                      |
|------------------------------|-----------------------------------------------------|
| `internal/domain`            | Entities, rules and domain errors                   |
| `internal/usecase/<feature>` | Application behavior and feature ports              |
| `internal/infra/*`           | Database, HTTP, scripts and other infrastructure    |
| `internal/transport/*`       | Wails bindings, desktop integration and composition |
| `internal/platform`          | Platform paths, identity and build metadata         |
| `pkg/*`                      | Reusable packages with no Schemo imports            |

Dependency direction is enforced by `internal/architecture` tests.

### Rules

- `domain` imports only the standard library.
- `usecase` must not depend on `infra`, `transport` or Wails.
- External systems belong in `infra/` or `transport/`.
- Interfaces are declared by the code that consumes them.
- Application composition lives in `transport/wails`.
- `pkg/` must not import anything from `schemo/`.

## Types exposed to the frontend

Anything crossing the Wails boundary is declared in Go and generated for TypeScript.

Place types according to ownership:

- domain entities → `internal/domain`;
- feature requests and responses → `internal/usecase/<feature>`;
- transport-specific shapes → `internal/transport`;
- external format results → beside their parser or reader.

Do not duplicate generated types in TypeScript.

## Repository Structure

```text
main.go
migrations/

internal/
  domain/
  usecase/
  infra/
    sqlite/
    httpx/
    authflow/
    scriptengine/
  transport/
    wails/
    desktop/
    updater/
    extension/
  platform/
  architecture/

pkg/
  migrate/
```

## Naming

Follow standard Go naming.

Project rules:

- no `util`, `common`, `helpers`, `models`, `types` or `constants` packages;
- package names describe what they provide;
- avoid redundant prefixes;
- use cases use `UseCase` and `NewUseCase`;
- ports use role names such as `Store`, `Server` or `Principal`;
- test doubles use `fake<Port>`;
- exported errors are domain errors;
- formatting belongs to `gofmt`.

In the frontend:

- functions are verbs;
- values are nouns;
- booleans use `is`, `has` or `can`;
- handlers use `onX`;
- composables use `useX`;
- display strings end in `Label`;
- shared UI requests use intents rather than watched counters.

Prefer existing libraries over handwritten equivalents.

## Tests

Tests should verify behavior, data integrity and dependency boundaries.

Use simple fakes for ports. If a fake starts reproducing real application logic, test against the real implementation instead.

Avoid source-text scanners for style or naming rules.

## Comments

Comments should:

- document exported declarations;
- explain why, not repeat what the code says;
- be short and technical;
- be written in English.

Use comments for decisions, invariants, compatibility and security assumptions that are not obvious from the code.

## Adding a Feature

1. Add concepts and rules to `domain/` when needed.
2. Add the feature under `usecase/<feature>/`.
3. Declare required ports in the feature.
4. Implement infrastructure in `infra/`.
5. Add migrations and storage when needed.
6. Expose the feature through `transport/wails`.
7. Register composition in `transport/wails/module.go`.
8. Mirror generated data and events in the frontend.

For bound Go API changes, regenerate Wails bindings.

## Vue Boundary

Vue owns presentation and view-only state:

- active tabs;
- popovers;
- selections;
- search text;
- caret and scroll position;
- animations;
- formatting;
- rendering.

Go owns:

- persistence;
- network behavior;
- secrets;
- external formats;
- business rules;
- anything that affects stored or transmitted data.

Pinia should remain a thin mirror of Go state.

## Checks

```bash
task test
task build
task dev
wails3 generate bindings -clean=true -ts -i
cd frontend && npx vue-tsc --noEmit
```

Run binding generation after changing bound Go methods.