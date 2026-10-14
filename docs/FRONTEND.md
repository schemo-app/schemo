# Frontend

The frontend is a Vue 3 application that mirrors state owned by Go.

Vue owns presentation and view state. Application state is changed through Go calls rather than local mutation.

## Structure

Components are grouped by feature:

```text id="3g47mr"
request/
response/
json/
inspector/
history/
collections/
environments/
workspaces/
browser/
search/
update/
layout/
ui/
```

Shared UI primitives live in `ui/`.

Pure frontend helpers live in `lib/`.

Reactive reusable behavior lives in `composables/`.

Application state mirrors live in `stores/`.

## UI Primitives

Use `reka-ui` for accessible behavior such as:

- focus management;
- Escape handling;
- outside clicks;
- positioning;
- ARIA behavior.

Project styling is applied on top.

When the same UI pattern is needed more than once, move it into `ui/`.

Common primitives include:

- buttons;
- inputs;
- switches;
- dialogs;
- popovers;
- menus;
- tabs;
- checkboxes;
- tooltips;
- sheets;
- alerts.

Use `Icon.vue` for icons. Icons come from `@lucide/vue`; do not add handwritten SVG icons.

## Feature Components

Feature-specific components stay inside their feature folder.

Do not move a component into `ui/` until it is genuinely shared.

Large screens may expose their state machines as composables instead of pushing all behavior into components.

## `lib/`

`lib/` contains pure functions only:

- no Vue;
- no reactive state;
- no calls to Go.

Typical responsibilities include:

- formatting;
- clipboard helpers;
- highlighting;
- JSON and JSON:API presentation;
- token markup;
- collection tree helpers;
- request display helpers.

Parsing or business rules that affect persisted or transmitted data belong in Go.

## `composables/`

Composables contain reusable reactive behavior and event subscriptions.

Examples:

- theme handling;
- drag and resize behavior;
- keyboard shortcuts;
- deferred writes;
- Go event subscriptions;
- screen-level view models.

Keep one event subscription composable per feature area.

Composables should read current values through getters or stores rather than capturing stale prop snapshots.

## `stores/`

Pinia stores are thin mirrors of Go state.

They:

- call Go services;
- store returned values;
- apply Go events.

They should not duplicate business logic already implemented in Go.

There is no separate IPC abstraction layer between stores and generated Wails services.

## Generated Data

Types crossing the Wails boundary are generated from Go.

Do not create handwritten TypeScript copies of Go models.

Constants that must match Go are generated where possible rather than duplicated manually.

## Theme

Themes are applied through classes on `<html>`.

Palette variables live in `style.css`.

Theme initialization must happen before first paint to avoid flashing the wrong palette.

Theme transitions are presentation behavior and remain entirely in the frontend.

## Rules

- Keep business logic in Go.
- Keep view state in Vue.
- Group code by feature.
- Reuse primitives instead of duplicating UI behavior.
- Prefer existing libraries to handwritten replacements.
- Keep stores thin.
- Keep `lib/` pure.
- Generate shared types instead of copying them.