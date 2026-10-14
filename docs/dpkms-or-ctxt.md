# dPKMS vs `ctxt`: Who Owns What

One Go module, two binaries. `dpkms` is the substrate: storage, job queue, pipeline runtime and the HTTP/gRPC/MCP API. `ctxt` is the client: it runs on the user's machine, sends work to one dpkms instance over HTTP, and does the local-only parts. Use this page to decide where a capability belongs.

## dpkms: the substrate

dpkms owns everything that has to be durable, shared or authoritative:

- **Storage**: knowledge objects, entities, edges, pages, feeds, inbox, audit log, in SQLite or PostgreSQL; large content in the blob store (local or S3).
- **Job queue**: durable jobs with leases, automatic retry on transient errors (`jobs.max_retries`, default 3), stale-job recovery (`jobs.stale_timeout`, default 30m), and manual `dpkms job retry|cancel`.
- **Pipelines**: the built-in pipelines and steps are compiled into dpkms; detectors pick one per input; `dpkms pipeline create|archive|remove` and `pipelines.overrides` change them. Workers (`dpkms serve --workers`) run them.
- **AI providers**: LLM, vision, transcription, OCR, embeddings are called from dpkms workers, configured in `dpkms.yaml` (`providers.*`) with keys from `dpkms secret` or env.
- **Search**: FTS, vector and hybrid search, RSQL queries, the search graph.
- **API and access control**: REST, gRPC, SSE, MCP (`/api/v1/mcp/`, tools `search` and `schema`), static-token auth with roles and scopes, policy rules.
- **Registries and federation**: registry sync, step registries, push-only federation to peers.
- **Operations**: `serve`, `ps`, `shutdown`, `backup`, `restore`, `housekeeping`, `upgrade`, `healthcheck`, `secret`, `key`.

dpkms does not decide what the user wants to see next or read the user's local machine state.

## ctxt: the client

ctxt owns the user-facing workflow and anything that needs the local machine:

- **Capture and enqueue**: `ctxt analyze`, `ctxt capture <url|file>`, `ctxt import ...`, `ctxt ingest`, `ctxt inbox` — all send content to dpkms (`/api/v1/analyze`, `/api/v1/capture/*`, `/api/v1/inbox`).
- **Local capture**: clipboard and directory watchers (`ctxt watch`), browser tabs and history (`ctxt capture tabs|history`, `ctxt capture schedule install`). These read local state, filter it with `capture.url_filter`, and send it to dpkms. See [ambient.md](ambient.md).
- **Reading and composing**: `ctxt find`, `list`, `show`, `stats`, `log`, `page`, `entity`; `ctxt compose brief|plan|summary|draft` renders compositions locally from objects read over the API.
- **Focus profiles** (`ctxt profile`), hints and mentions attached at capture time.
- **Instance selection**: `ctxt instance use|list|current`, `server.url` / `server.urls` and the token ctxt sends.
- **Interfaces**: `ctxt tui`, `ctxt shell`, `ctxt ui open` (signs the browser into the dpkms web UI).

Every ctxt command talks to exactly one dpkms instance and never falls back to another; nothing is queued locally when it is unreachable.

## Deciding where something goes

| Question | Owner |
|----------|-------|
| Must it survive a client crash, or be shared by several clients? | dpkms |
| Does it run a pipeline step or call an AI provider? | dpkms |
| Does it need an auth scope or belong in the API? | dpkms |
| Does it read the user's clipboard, files, browser or screen? | ctxt |
| Is it presentation or composition of objects already stored? | ctxt |
| Is it a choice the user makes per command (profile, hints, pipeline override)? | ctxt flag, enforced by dpkms |

dpkms is often deployed remote (managed instance, household NAS, federated peer). A remote dpkms cannot read the user's clipboard or browser profile, so local capture stays in ctxt and reaches dpkms over HTTP like any other capture.

## Not implemented

The ambient daemon `ctxd` from [ADR-066](decisions/ADR-066-ambient-capture-substrate.md), work sessions ([ADR-067](decisions/ADR-067-session-workunit.md)), the local MCP server on port 8744 ([ADR-068](decisions/ADR-068-mcp-read-surface.md)) and the meeting recorder ([ADR-069](decisions/ADR-069-meeting-capture-source.md)) are design. If built, they belong on the ctxt side for the reason above. See [ambient.md](ambient.md#not-implemented).

## Related

- [cross-package-contracts.md](cross-package-contracts.md) — packages each binary owns, `dpkmsclient`
- [architecture.md](architecture.md)
