# Workflow: Agent-Native MCP Read-Surface

## Goal

Let an AI agent (Claude Code, Cursor, a custom MCP client) search the ctxt knowledge graph through the Model Context Protocol.

## What exists

dpkms serves one MCP endpoint on its HTTP listener, next to the REST API:

| Property | Value |
|---|---|
| URL | `http://<dpkms host>:<server.port>/api/v1/mcp/` (`/api/v1/mcp` also works) |
| Transport | Streamable HTTP, JSON-RPC 2.0, protocol version `2025-03-26`. `POST` only; any other method answers `405`. There is no stdio transport. |
| Server name | `ctxt-graph` |
| Methods | `initialize`, `tools/list`, `tools/call`, `ping` |
| Tools | `search`, `schema` |
| Scope | `read:mcp`, held by every role (`reader`, `writer`, `admin`) |
| Writes | None. Every tool is a read; content goes in through `ctxt analyze` / `ctxt capture`. |

The endpoint is always mounted when `dpkms serve` runs. There is no `mcp:` config key, no MCP-specific flag or env var, and no `ctxt mcp` command: the endpoint uses the dpkms server's own port, bind and inbound auth settings.

Code: `internal/mcp/` (JSON-RPC dispatch, tools), `internal/server/http/routes.go` (mount), `internal/server/http/mcp_search.go` (search wiring).

## Procedure

### Step 1: Start dpkms

```bash
dpkms serve
```

The default port is 8080 (`--port`, `CH_SERVER_PORT` or `server.port`). See [Server settings](../reference/config-and-permissions.md#server-settings).

### Step 2: Register the endpoint in your agent client

Add the URL as an HTTP (streamable HTTP) MCP server in your client's own MCP settings. With Claude Code, for example:

```bash
claude mcp add --transport http ctxt-graph http://127.0.0.1:8080/api/v1/mcp/
```

On a private instance (`server.access: private`, the default) no key is needed. On a `protected` or `public` instance, send an API key whose principal holds any of `reader`, `writer` or `admin`; create one on the dpkms host with `dpkms auth key create`:

```bash
claude mcp add --transport http ctxt-graph https://dpkms.example.com/api/v1/mcp/ \
  --header "Authorization: Bearer <key from dpkms auth key create>"
```

Tokens and roles: [Inbound auth, roles and scopes](../reference/config-and-permissions.md#inbound-auth-roles-and-scopes).

Restart the client or refresh its MCP server list to pick up the tools.

### Step 3: Tool reference

| Tool | Args | Returns | When the agent should use it |
|---|---|---|---|
| `search` | query, top_k=10 (1-100), mode=hybrid\|fts\|vector, profile?, min_score? (hybrid), meta_type?, topic?, person?, source_type?, since?, until? | ranked results with title, snippet, score and (hybrid) score_breakdown; `executed_mode` and `diagnostics.semantic` say when semantic search could not run and full-text answered | Specific keywords (person, project, error msg, file path) or related wording. Same search as `POST /api/v1/find`. Without `profile` it searches every profile. `since`/`until` are `YYYY-MM-DD` dates mentioned in the object |
| `schema` | (no args) | storage taxonomy: object kinds, edge types, pipelines | Agent constructing structured queries |

### Step 4: Server-level instructions

`initialize` returns an `instructions` string that MCP clients show to the model:

> ctxt is the user's personal knowledge graph — captures, decisions, mentions, sessions, projects, people, recent activity. CALL THESE TOOLS FIRST whenever the user asks about themselves, their work, their recent context: "what was I doing this afternoon?" / "what did I decide about X?" / "summarize my last session on Y" / "who is Alice?" — prefer this graph over replying "I don't know."

### Step 5: Profile scoping

Pass `profile` per `search` call:

```
search(query="auth bug", profile="work")
```

Without `profile`, `search` covers every profile.

## Outputs to validate

- Endpoint reachable:

  ```bash
  curl -X POST http://127.0.0.1:8080/api/v1/mcp/ \
    -H 'Content-Type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
  ```

  dpkms answers `415` to a body without `Content-Type: application/json`. Add `-H 'Authorization: Bearer <token>'` on a non-private instance.
- Tool schemas valid (use the [MCP Inspector](https://github.com/modelcontextprotocol/inspector) for interactive testing)

## Common failure modes

### "Agent client can't connect"

- The client must support streamable-HTTP MCP servers; there is no stdio fallback.
- `401`: the instance is not private and the request carries no key, or an unknown, expired or revoked one. Check `server.access` and the key.
- `403 INSUFFICIENT_SCOPE`: the request authenticated with a web UI browser session, which never holds `read:mcp`. Use a key.
- `405`: the client sent `GET`. The endpoint only answers `POST`.
- Connection refused: dpkms is not running, or listens on another port or host (`ctxt status` shows the instance ctxt resolves).

### "search returns fewer results than expected"

- A `profile` argument narrows to that profile; drop it to search every profile.
- `executed_mode: fts_only` (no embedding model) or `fts_fallback` (the semantic leg failed) means only keyword matches came back; `diagnostics.semantic` says why.

## Not implemented

The tools `list`, `get`, `entity`, `recent`, `sessions`, `session`, `compose` and `mentions`, the local `ctxd` MCP server and its tools, `ctxt mcp install|status|uninstall`, MCP bus events and an `mcp:` config block are described in [ADR-068](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-068-mcp-read-surface.md) but do not exist.

## Related references

- [`search-retrieval.md`](search-retrieval.md) and [`semantic-search.md`](semantic-search.md) — the search the `search` tool runs
- [`../reference/config-and-permissions.md`](../reference/config-and-permissions.md) — inbound auth, roles, `read:mcp`
- [`docs/decisions/ADR-068-mcp-read-surface.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-068-mcp-read-surface.md) — design record (includes unbuilt parts)
- [`docs/decisions/ADR-038-supermemory-not-adopted.md`](https://github.com/ContextHelp/ContextHelp/blob/next/docs/decisions/ADR-038-supermemory-not-adopted.md)
- [MCP Specification](https://modelcontextprotocol.io)
