# Compose Cleanup: Redis and Qdrant Are Advertised, Never Used

Date: 2026-08-04
Status: Analysis — proposals only, no edits applied
Context: [Storage architecture POV](2026-08-04-storage-architecture-pov.md), which flagged the compose entries as "misleading documentation."

## Evidence

### 1. Zero Go references

Searched `internal/`, `pkg/`, `cmd/`, `plugins/`, `test/` for `redis`/`qdrant` (word-boundary, case-insensitive):

- **No client imports.** `go.mod`/`go.sum` contain no redis or qdrant modules.
- **No env-var reads.** `REDIS_*` and `QDRANT_*` appear in zero `.go` files. No config struct parses them.
- **All hits are fixture text.** e.g. `internal/search/query_decompose_test.go` uses `"Redis"` as a sample search query; `test/integration/us0010_decision_extraction_test.go` uses `"Use Redis for caching"` as sample content. `internal/search/query_mode.go:52` even lists `"redis": false // not a tech indicator on its own`.
- Job queue is SQLite-backed (`internal/jobs/queue.go`); vectors are sqlite-vec. Postgres, by contrast, **is** a real backend: `test/integration` consumes `POSTGRES_*` (DSN builder plumbed recently) and CI exercises it.

### 2. Non-Go references to the phantom services

| Artifact | What it claims | Reality |
|---|---|---|
| `docker-compose.dev.yml` | `redis:7-alpine` ("distributed job queue and caching"), `qdrant/qdrant:v1.7.0` ("vector search database") | Header already says "NOT required… only when validating… Redis, or Qdrant integrations" — but no such integrations exist to validate |
| `.env.example:35-47` | Commented `REDIS_ENABLED`, `REDIS_HOST/PORT/PASSWORD/DB`, `QDRANT_ENABLED/HOST/PORT/API_KEY/COLLECTION` | No code reads any of them |
| `docs/environment-variables/services.md` | Full reference doc: types, defaults, "Enable Redis for distributed job queue and caching" | **Most misleading artifact** — documents a config surface that does not exist |
| `.github/workflows/ci.yml` (integration job) | Spins up `redis:7-alpine` service container; exports `REDIS_HOST`/`REDIS_PORT` to tests | Dead weight — nothing consumes it; pure CI spin-up cost |
| `.github/workflows/release.yml` (integration gate) | Same redis service container + env | Same dead weight |
| `scripts/dev-setup.sh:245-250, 325-327` | Writes commented `REDIS_*`/`QDRANT_*` into generated `.env`; advertises "To start Postgres, Redis, and Qdrant" | Propagates the phantom config |
| `docs/developer-quickstart.md`, `docs/development-infrastructure.md`, `docs/ci-cd.md` | Present-tense: "Redis - Distributed job queue (`:6379`)", "Qdrant 1.7 (vector search)" | Present-tense framing of nonexistent integrations |
| `docs/scaling.md`, `docs/dpkms/{queue,caching,embeddings}.md`, `docs/dependencies.md`, `docs/architecture.md` | Future/plugin framing ("Plugin-defined stores", "Plugin interface for distributed queues (Redis, NATS)") | Correctly aspirational — fine as-is |
| `plugins/`, `extensions/`, `schemas/`, `skills/`, `Makefile`, `docker-compose.yml` | — | No references at all; no plugin manifest mentions either service |

### 3. Is Qdrant a planned backend?

Yes — twice, explicitly:

- `docs/decisions/ADR-022-vector-semantic-search.md` sketches a `vector_backend: type: plugin, plugin: qdrant-backend` config (url `:6333`, collection) as the "Custom Plugin" escape hatch alongside sqlite-vec (default), pgvector, and LEANN.
- `docs/ROADMAP.md` Skeleton 6: `- [ ] Qdrant backend (external vector DB option)`.

Redis has weaker standing: `docs/dependencies.md` explicitly rules it out ("Network dependency (violates local-first)") and defers to a future "Plugin interface for distributed queues (Redis, NATS)". No ADR commits to Redis specifically.

## Recommendation

The aspiration is fully preserved by ADR-022 and the ROADMAP; the compose/env/CI artifacts add nothing until code exists, and the `qdrant/qdrant:v1.7.0` pin is already stale. **Prune, don't annotate** — with one exception: leave a one-line breadcrumb in the compose header so the re-add path is obvious when the ROADMAP item lands.

Per entry:

| Entry | Verdict | Rationale |
|---|---|---|
| compose `postgres` | **Keep** | Real alternative backend; CI + integration tests use it |
| compose `redis` + `redis_data` volume | **Prune** | No code, no ADR commitment, local-first doc rules it out |
| compose `qdrant` + `qdrant_data` volume | **Prune** (breadcrumb in header) | Planned (ADR-022/ROADMAP) but zero code; stale pin; re-add in the implementing change |
| `.env.example` Redis/Qdrant blocks | **Prune** | Example env must mirror the real config surface |
| `docs/environment-variables/services.md` | **Rewrite as planned-status stub** | Deleting breaks the env-vars README index; a stub prevents the vars from being re-invented differently later |
| CI/release redis service + `REDIS_*` env | **Prune** | Unconsumed; saves container startup on every integration run |
| `scripts/dev-setup.sh` | **Prune** the two commented blocks + trim the closing echo | Keeps generated `.env` honest |
| `docs/developer-quickstart.md`, `development-infrastructure.md`, `ci-cd.md` | **Reword** present-tense mentions to match | Follow-on docs sweep; low urgency |
| scale-out/plugin docs (`scaling.md`, `dpkms/*`, `dependencies.md`, `architecture.md`) | **Keep as-is** | Already framed as future options |

## Proposed edits (not applied)

### docker-compose.dev.yml

```diff
-# Optional development side-services for testing non-default backends.
-# Default ctxt dev flow uses SQLite + local job queue + sqlite-vec — these
-# services are NOT required. Spin them up only when validating Postgres,
-# Redis, or Qdrant integrations.
+# Optional development side-service for testing the non-default Postgres
+# backend. Default ctxt dev flow uses SQLite + local job queue + sqlite-vec
+# — this service is NOT required.
+#
+# A Qdrant vector backend is planned (ADR-022, ROADMAP Skeleton 6); its
+# service entry will be reintroduced alongside the code that consumes it.
 #
 # Usage:
 #   docker compose -f docker-compose.dev.yml up postgres
-#   docker compose -f docker-compose.dev.yml up redis qdrant
@@
-  # Redis — distributed job queue and caching
-  redis:
-    image: redis:7-alpine
-    container_name: ctxt-redis-dev
-    ... (entire service block)
-
-  # Qdrant — vector search database
-  qdrant:
-    image: qdrant/qdrant:v1.7.0
-    container_name: ctxt-qdrant-dev
-    ... (entire service block)
@@
 volumes:
   postgres_data:
     driver: local
-  redis_data:
-    driver: local
-  qdrant_data:
-    driver: local
```

### .env.example

```diff
-# Redis (distributed job queue, caching)
-# REDIS_ENABLED=false
-# REDIS_HOST=localhost
-# REDIS_PORT=6379
-# REDIS_PASSWORD=
-# REDIS_DB=0
-
-# Qdrant (vector search)
-# QDRANT_ENABLED=false
-# QDRANT_HOST=localhost
-# QDRANT_PORT=6333
-# QDRANT_API_KEY=
-# QDRANT_COLLECTION=contexthelp
```

### .github/workflows/ci.yml and release.yml (integration jobs)

```diff
-      redis:
-        image: redis:7-alpine
-        options: >-
-          --health-cmd "redis-cli ping"
-          --health-interval 10s
-          --health-timeout 5s
-          --health-retries 5
-        ports:
-          - 6379:6379
@@
           POSTGRES_DB: contexthelp_test
-          REDIS_HOST: localhost
-          REDIS_PORT: 6379
```

### docs/environment-variables/services.md

```diff
-# Optional Services Environment Variables
-
-Configuration for Redis and Qdrant vector search.
-... (full REDIS_*/QDRANT_* reference)
+# Optional Services Environment Variables
+
+> **Status: planned — not implemented.** No released binary reads any
+> variable on this page. The job queue is SQLite-backed and vector search
+> uses sqlite-vec. A Qdrant vector backend is planned via the pluggable
+> vector-backend interface (ADR-022, ROADMAP Skeleton 6); a distributed
+> queue plugin interface may follow. Variable names below are reserved and
+> will be finalized when the backends land.
```

### scripts/dev-setup.sh

```diff
-# Redis (optional, for distributed jobs)
-# REDIS_HOST=localhost
-# REDIS_PORT=6379
-
-# Qdrant (optional, for vector search)
-# QDRANT_HOST=localhost
-# QDRANT_PORT=6333
@@
 echo "=== Optional Services ==="
-echo "To start Postgres, Redis, and Qdrant:"
+echo "To start Postgres (alternative storage backend):"
 echo "  docker-compose -f docker-compose.dev.yml up -d"
```

## Follow-on (separate, low urgency)

Docs sweep to reword present-tense Redis/Qdrant mentions in `docs/developer-quickstart.md` (prereqs + optional-services section), `docs/development-infrastructure.md`, and `docs/ci-cd.md` to match the pruned reality.
