# MCP

Schemo exposes a local MCP server so compatible agents can inspect saved workspace data and, when allowed, send saved requests.

The implementation lives in `internal/transport/mcp`.

## Connection

The server:

- listens on `127.0.0.1` only;
- uses Streamable HTTP at `/mcp`;
- defaults to port `9300`;
- requires an MCP token from Settings → MCP server.

Authentication accepts:

```text id="sm8w2d"
Authorization: Bearer sk-sch-…
```

or `?token=` for clients that cannot set headers.

Reissuing the token invalidates existing MCP sessions.

## What Agents Can See

Agents may read:

- spaces;
- collections;
- saved requests;
- history.

Sensitive values remain protected:

- environment secrets are masked;
- authorization data is not exposed;
- stored history uses the same masking rules as the app;
- large response bodies are truncated.

Agents see saved requests, not raw credentials.

## Permissions

Two access modes are supported:

- read-only;
- read and send.

Read-only mode exposes no send tool.

With send access, `send_request` uses the application's normal request pipeline:

- environment variables are resolved;
- inherited authorization is applied;
- the selected environment is respected;
- the result is recorded in history.

Agent-initiated sends are marked with the agent name.

## Send Approval

When confirmation is enabled, each send requires approval from the Schemo window.

If no approval arrives within two minutes, the request is refused.

## Scope

MCP access can be restricted to selected spaces.

Secret values and authorization credentials never become available to MCP agents.

## Limits

MCP intentionally does not provide:

- remote network access;
- separate long-lived tokens per agent;
- create, edit or delete tools;
- direct credential access;
- credential prompts for agents.

Agents can inspect data and optionally send existing saved requests, but they cannot modify Schemo's stored content.