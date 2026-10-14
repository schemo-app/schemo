# Design

This document describes how Schemo works and why.

## Core Model

Go owns application state and behavior. Vue mirrors that state and handles presentation.

Major features live under `usecase/`:

- environments and variables;
- request history;
- drafts;
- collections;
- cookies;
- scripts;
- import and export;
- search;
- inspection;
- publication;
- workspaces.

Infrastructure such as SQLite, HTTP, authentication and script execution lives under `infra/`.

Desktop integration, Wails bindings, browser extension integration and composition live under `transport/`.

## Requests

A request is prepared and sent entirely in Go.

Preparation includes:

- path parameters;
- environment variables;
- authentication;
- cookies;
- scripts.

The frontend receives the resulting record and never resolves secrets or request credentials itself.

Addresses without a scheme default to:

- `https` for normal hosts;
- `http` for localhost addresses.

## Authentication

Supported schemes include:

- Bearer;
- Basic;
- API key;
- JWT;
- AWS SigV4;
- Digest;
- OAuth 2.0.

Authentication is modeled as data rather than branches in request code.

Inherited authorization is resolved from the nearest collection level that defines it.

Obtained tokens remain in Go and are not exposed to the frontend.

## Variables and Path Parameters

Environment variables use:

```text id="ym7gg1"
{{name}}
```

Path parameters use:

```text id="khs0wt"
/users/:id
```

Variables come from environments and collection scope.

Path parameters belong to the request itself and are derived from its URL.

All request consumers use the same resolved form for sending, previewing, exporting and collection runs.

## Collections

Collections contain request trees and support sequential execution.

Import and export are handled through format-specific readers and writers.

Supported formats include:

- Schemo;
- OpenAPI;
- Postman;
- Insomnia;
- HAR.

Format packages convert to and from the shared domain representation.

## Commands

Schemo can parse pasted cURL commands.

Requests can be exported as:

- cURL;
- fetch;
- wget;
- HTTPie;
- PowerShell.

Command parsing and generation live in Go.

## Search

Global search spans collections, environments, history and commands.

Search is implemented as a read projection over SQLite rather than by calling each feature independently.

Secret values are never searchable or returned in search results.

## Inspector

The inspector reads structured responses and exposes them as navigable data.

Supported formats include:

- JSON;
- XML;
- HTML;
- CSV.

It can resolve values by position or path and identify common error formats and references.

Parsing and interpretation live in Go; the frontend renders the result.

## Scripts

Scripts run around a request in this order:

1. collection;
2. parent folders;
3. request.

The sandbox has no direct network, filesystem or timer access.

Response captures can store selected values into variables before post-request scripts run.

## Frontend Mirror

Vue receives state through generated Wails bindings.

Stores call Go services directly and replace their state from returned values.

Events are subscribed to through composables.

The frontend may own temporary view state such as:

- selection;
- active tabs;
- open popovers;
- search text;
- caret and scroll position.

Business state is written through Go calls rather than edited locally.

## Input Buffers

The request URL and body are temporarily owned by the frontend while the user is typing so cursor state is preserved.

Changes are synchronized back to Go using revisions. Stale responses are ignored.

Discrete fields such as method, headers and authentication are written immediately.

## Secrets

Secrets remain on the Go side whenever possible.

They are not exposed through:

- search;
- publication;
- request history representations intended for display;
- generated public documents.

Explicit request export may include resolved credentials because it represents the complete request.

## Publications

A publication is a versioned document generated from a collection and uploaded to the server.

Only explicitly allowed fields are included. Secret values are never copied into the publication document.

The server controls access and subscription enforcement.

## Workspaces

A workspace owns:

- history;
- collections;
- environments;
- drafts.

Application preferences such as theme and language remain global.

Workspace ownership is represented by `workspace_id` in local storage.

The active workspace is resolved once per operation so switching workspaces cannot move an in-progress operation to another workspace.

## Window Appearance

Window material is platform-controlled.

Where supported, Schemo uses native translucency. Otherwise the frontend renders an opaque background.

The frontend does not emulate platform window materials.

## Deliberate Constraints

Schemo intentionally avoids:

- duplicate frontend business logic;
- handwritten TypeScript copies of Go models;
- repository interfaces without a real abstraction need;
- a separate DTO layer for every call;
- frontend mocks that duplicate backend behavior;
- exposing secrets merely for presentation convenience.

Keep additional abstractions only when they remove real duplication or isolate an external dependency.