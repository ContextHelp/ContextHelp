# Cross-Package Contracts (dPKMS and ctxt)

How the `dpkms` server and the `ctxt` client divide the work, and where the
contracts between them live.

---

## One Module, Two Binaries

Both binaries are built from one Go module (`github.com/ideacrafterslabs/ctxt`):

| Binary | Entry point | Role |
|--------|-------------|------|
| `dpkms` | `cmd/dpkms` | Server: storage, job queue, pipeline runtime, HTTP and gRPC APIs, federation |
| `ctxt` | `cmd/ctxt` | Client CLI/TUI: capture, search, compose, admin commands |

They share the packages under `internal/`; no import boundary between the two
is enforced. The only public package is `pkg/taxonomy`. Each binary reads its own config file (`dpkms.yaml`, `ctxt.yaml`);
see [ctxt/configuration.md](ctxt/configuration.md).

---

## What dpkms Owns

| Concern | Package |
|---------|---------|
| Storage drivers (`sqlite`, `postgres`) and blob store (`local`, `s3`) | `internal/storage`, `internal/storageutil`, `internal/storage/blob` |
| Job queue (a `jobs` table in the store) and workers | `internal/jobs` |
| Pipelines and steps | `internal/pipeline`, `internal/steps` |
| Search (query parsing, FTS, vectors, ranking) | `internal/search`, `internal/ranking` |
| HTTP API (`/api/v1`, `/healthz`, `/ui`) | `internal/server/http` |
| gRPC API (`AnalyzeService`, `JobService`, `QueryService`, `EntityService`) | `internal/server/grpc`, `api/proto/dpkms.proto` |
| Inbound auth and per-route scopes | `internal/auth` |
| Federation push | `internal/federation` |

---

## How ctxt Reaches dpkms

`internal/dpkmsclient` is ctxt's HTTP client for dpkms. A client talks to one
endpoint (resolved from `--server`, `--instance`, `CTXT_INSTANCE`,
`ctxt instance use`, `server.urls` or `server.url`), sends the endpoint's bearer
token, and maps failures to exit codes (unreachable 70, 401/403 5, 404 3,
409 4, 400/422 2, 429 64, 5xx 6).

[ADR-077](decisions/ADR-077-ctxt-pure-api-client.md) makes this the only path:
ctxt never opens the dpkms store. The migration is not finished; some commands
still open the store directly through `newService()`
(`cmd/ctxt/cmd/helpers.go`). Treat those as legacy.

Endpoint and message reference: [api/api-rest.md](api/api-rest.md),
[api/api-grpc.md](api/api-grpc.md).

---

## Contract Artifacts

| Contract | Location |
|----------|----------|
| gRPC services and messages | `api/proto/dpkms.proto` |
| JSON shape contracts for operator-facing output (`/healthz`, upgrade status, embeddings list, search staleness) | `contracts/*.eva.yaml`, checked by `make eva` (part of `make check`) |
| Node-aware query types (`NodeAwareFilter`, `NodeHit`, `NodeAwareResult`) | `internal/storage/query_contract.go` |
| Contribution catalog entry (`extensions/<slug>/catalog.json`, `skills/<slug>/catalog.json`) | `.github/catalog.schema.json`, [catalog-spec.md](catalog-spec.md) |

---

## Related

- [architecture.md](architecture.md) - component overview
- [dpkms-or-ctxt.md](dpkms-or-ctxt.md) - where a capability belongs
- [scaling.md](scaling.md) - ports, workers, storage sizing
