# MCP

Schemo exposes its desktop data and request pipeline to MCP clients through a local Streamable HTTP server.

## Connect

Enable the server in **Settings → MCP server** and copy the agent configuration. The default endpoint is `http://127.0.0.1:9300/mcp`.

Every request requires `Authorization: Bearer sk-sch-…`. Clients without header support can use `?token=`. Reissuing the token invalidates existing sessions. The server listens only on loopback.

## Permissions

- Reading is always available for the workspaces selected in settings.
- Sending is optional and independent of editing. When enabled, `send_request` uses the normal request pipeline, environment, inherited authorization and history. Sends are attributed to the agent.
- **Edit saved data** is off by default. Enabling it allows creation, modification, movement and deletion in the selected workspaces.
- **Ask before sending** requires confirmation in the application; unanswered sends expire after two minutes. This setting does not confirm edits.

Permissions are checked on each call. Disabling an operation withdraws its tools. Workspace roles are still checked by the normal storage transactions, and the cloud enforces its own permissions and plan limits.

“All workspaces” means local workspaces and cloud workspaces visible to the currently signed-in account. Cached ids from another account do not grant access. Pending send approvals recheck the account scope and permissions before execution.

The MCP token belongs to this desktop installation, not to an individual cloud account. Switching accounts does not rotate it: connected agents follow the current account and the selected workspace policy. Reissue the token or disable MCP to disconnect them.

## Tools

There are 20 tools with both sending and editing enabled, 7 with sending alone, and 6 with neither.

| Tools | Purpose |
| --- | --- |
| `workspaces` | Discover workspace ids and names |
| `collections`, `get_collection` | Read collections and folders, including descriptions, saved authorization and request lists |
| `requests` | List saved requests or read one request, including saved authorization, description, query parameters and environment pin |
| `environments` | Read a workspace's environments, globals, variables and active environment |
| `history` | Read recent history or a saved response |
| `send_request` | Execute a saved request through the application |
| `create_collection`, `update_collection`, `delete_collection`, `move_collection` | Manage collections and nested folders |
| `create_request`, `update_request`, `delete_request`, `move_request` | Manage saved HTTP requests without executing them |
| `create_environment`, `update_environment`, `delete_environment` | Manage environments; creation selects the new environment in that workspace |
| `set_variable`, `delete_variable` | Set a variable by name or delete it by id, in an environment or workspace globals |

Personal and team workspaces use the same tools. New workspace operations require an explicit `workspace` id and do not switch the desktop's workspace. Source and destination ids must belong to that workspace.

Request edits are partial: omit a field to keep it unchanged. Scripts, files and disabled headers are preserved unless an editable field explicitly replaces their associated data. The `auth` field replaces saved authorization; omit it to preserve the current configuration, or use `type: "inherit"` to inherit from the parent collection. Collection authorization can be changed with `update_collection`. Request bodies can be edited as raw text, JSON or XML; local file reads and uploads are not exposed.

`set_variable` preserves an existing variable's secret kind. Set `secret: true` to create a secret; its value is not returned.

## Data protection

- Secret environment values are withheld; known secret values are masked in request and history text.
- Saved inline API credentials are readable, including request and collection authorization and stored header values. MCP access to a workspace therefore grants access to inline credentials stored there. Use secret variables when their values must remain hidden from the agent.
- Authorization fields preserve `{{variable}}` references; reading a request does not resolve them. Known secret-variable values are masked even when embedded in saved authorization or headers.
- Masked values cannot be written back as request content or variable values.
- Response bodies are masked before truncation to 256 KiB.
- Mutation results contain ids, not private request payloads.

The desktop refreshes affected workspace data after MCP edits. Normal storage writes enqueue changes for synchronization.

Application account credentials, OS credential-store entries and the MCP access token are not exposed through tools. No token-reveal tools, local filesystem access, arbitrary script execution or remote MCP listener are provided.

## Compatibility approach

Saved API data is readable by an authorized agent, while explicitly protected variables remain hidden. This follows the distinction documented by [Hoppscotch MCP](https://github.com/hoppscotch/hoppscotch-mcp-server#security). [Postman MCP](https://github.com/postmanlabs/postman-mcp-server/blob/main/src/tools/getCollection/getCollection.ts) returns collection API data, while [Postman Vault](https://learning.postman.com/docs/use/postman-vault/use-vault-secrets) separates protected secrets from saved request data. Schemo secret variables are not a Postman Vault implementation.
