# Docker Environment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Single docker-compose setup covering both local dev and production single-machine
self-hosted deployment.

**Architecture:** One compose file with `dev` and `prod` profiles. Dev profile mounts source,
hot-reloads, exposes ports directly. Prod profile builds optimized images, uses Caddy reverse
proxy for TLS, persists all data in named volumes. Shared services (SQLite data dir, blob
storage) are profile-agnostic.

**Tech Stack:** Docker Compose v2, Go multi-stage build, Caddy v2 (reverse proxy + automatic
TLS), Node 22 Alpine (web UI build stage), GitHub Actions for image CI.

---

## Task List

1. Multi-stage Dockerfile
2. docker-compose.yml (base + dev profile)
3. Prod profile + Caddy
4. Health checks + restart policies
5. Makefile targets
6. GitHub Actions CI
7. Documentation

---

### Task 1: Multi-stage Dockerfile

**Goal:** Single `Dockerfile` at repo root replacing `docker/Dockerfile.dpkms`. Three stages:
UI build → Go build → minimal runtime.

**Files:**
- Create: `Dockerfile`
- Remove reference to: `docker/Dockerfile.dpkms` (keep file; update compose to use new one)

**Step 1: Create `Dockerfile` at repo root**

```dockerfile
# syntax=docker/dockerfile:1

# ─── Stage 1: UI builder ─────────────────────────────────────────────────────
FROM node:22-alpine AS ui-builder

WORKDIR /ui

# Install deps first for layer caching
COPY web/ui/package.json web/ui/package-lock.json* web/ui/pnpm-lock.yaml* ./
RUN corepack enable && \
    if [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile; \
    else npm ci; fi

COPY web/ui/ ./
RUN npm run build 2>/dev/null || pnpm build

# ─── Stage 2: Go builder ─────────────────────────────────────────────────────
FROM golang:1.25-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Copy UI dist from ui-builder (embedded into dpkms binary or served from /app/web)
COPY --from=ui-builder /ui/dist ./web/ui/dist

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X main.GitCommit=${GIT_COMMIT} \
      -X main.BuildTime=${BUILD_TIME}" \
    -o /out/dpkms ./cmd/dpkms && \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X main.GitCommit=${GIT_COMMIT} \
      -X main.BuildTime=${BUILD_TIME}" \
    -o /out/ctxt ./cmd/ctxt

# ─── Stage 3: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata sqlite wget

RUN addgroup -S ctxt && adduser -S -G ctxt -u 1000 ctxt

WORKDIR /app

COPY --from=go-builder /out/dpkms /app/dpkms
COPY --from=go-builder /out/ctxt   /app/ctxt
COPY docker/config.yaml            /app/config.yaml

RUN mkdir -p /data/blobs && chown -R ctxt:ctxt /data /app

USER ctxt

ENV DPKMS_DATA_DIR=/data \
    DPKMS_WORKERS=4 \
    CH_PUBLIC=true

EXPOSE 8080 9090

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/dpkms"]
CMD ["serve", "--config", "/app/config.yaml", "--public"]
```

**Note:** `web/ui/` does not yet exist. The `ui-builder` stage is a no-op until the web UI
is created. Gate it with a build arg or an `if [ -d web/ui ]` guard during early development
by replacing the COPY/RUN in `ui-builder` with:

```dockerfile
FROM node:22-alpine AS ui-builder
WORKDIR /ui
# placeholder until web/ui is scaffolded
RUN mkdir -p dist && echo '{}' > dist/.keep
```

Switch to the real build when `web/ui/package.json` lands.

**Step 2: Verify build**

```
docker build --target runtime -t ctxt:test .
```

Expected: image `ctxt:test` created, no errors.

```
docker run --rm ctxt:test version
```

Expected: version string printed (e.g. `dpkms dev`).

**Step 3: Commit**

```
git add Dockerfile
git commit -m "build(docker): add multi-stage Dockerfile (ui-builder, go-builder, runtime)"
```

---

### Task 2: docker-compose.yml (base + dev profile)

**Goal:** Single `docker-compose.yml` at repo root. Base service: `dpkms`. Dev profile adds
hot-reload via `air` and a Vite dev server.

**Files:**
- Create: `docker-compose.yml` (repo root)
- Create: `.env.dev` (template, gitignored except `.env.dev.example`)
- Create: `docker/air.toml` (air config for hot-reload)
- Update: `.gitignore`

**Step 1: Create `docker-compose.yml`**

```yaml
# docker-compose.yml
# Usage:
#   dev:  docker compose --profile dev up
#   prod: docker compose --profile prod up -d

name: ctxt

x-dpkms-common: &dpkms-common
  image: ctxt/dpkms:${TAG:-latest}
  restart: unless-stopped
  environment:
    CH_STORAGE_TYPE: sqlite
    CTXT_DATA_DIR: /data/dpkms.db
    CTXT_BLOB_BACKEND: local
    DPKMS_WORKERS: ${DPKMS_WORKERS:-4}

services:

  # ── Base dpkms (prod profile, built image) ──────────────────────────────
  dpkms:
    <<: *dpkms-common
    build:
      context: .
      dockerfile: Dockerfile
      target: runtime
      args:
        VERSION: ${VERSION:-dev}
        GIT_COMMIT: ${GIT_COMMIT:-unknown}
        BUILD_TIME: ${BUILD_TIME:-unknown}
    container_name: ctxt-dpkms
    volumes:
      - ctxt_data:/data
      - ctxt_blobs:/data/blobs
    networks:
      - ctxt
    profiles:
      - prod

  # ── Dev dpkms (hot-reload via air) ──────────────────────────────────────
  dpkms-dev:
    image: golang:1.25-alpine
    container_name: ctxt-dpkms-dev
    working_dir: /app
    command: >
      sh -c "apk add --no-cache gcc musl-dev sqlite-dev &&
             go install github.com/air-verse/air@latest &&
             air -c docker/air.toml"
    volumes:
      - .:/app
      - go_cache:/root/go/pkg/mod
      - ctxt_dev_data:/data
    ports:
      - "8080:8080"   # HTTP API
      - "9090:9090"   # gRPC
      - "9377:9377"   # WebSocket (future)
    environment:
      CH_STORAGE_TYPE: sqlite
      CTXT_DATA_DIR: /data/dpkms.db
      CTXT_BLOB_BACKEND: local
      DPKMS_WORKERS: ${DPKMS_WORKERS:-2}
      CH_PUBLIC: "true"
      CGO_ENABLED: "1"
      GOOS: linux
    networks:
      - ctxt
    profiles:
      - dev

  # ── Vite dev server ──────────────────────────────────────────────────────
  ui-dev:
    image: node:22-alpine
    container_name: ctxt-ui-dev
    working_dir: /app/web/ui
    command: >
      sh -c "corepack enable &&
             ([ -f pnpm-lock.yaml ] && pnpm install || npm install) &&
             ([ -f pnpm-lock.yaml ] && pnpm dev --host 0.0.0.0 ||
              npm run dev -- --host 0.0.0.0)"
    volumes:
      - ./web/ui:/app/web/ui
      - node_modules:/app/web/ui/node_modules
    ports:
      - "5173:5173"
    environment:
      VITE_API_URL: http://dpkms-dev:8080
    networks:
      - ctxt
    profiles:
      - dev

  # ── Caddy reverse proxy (prod only) ─────────────────────────────────────
  caddy:
    image: caddy:2-alpine
    container_name: ctxt-caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    volumes:
      - ./docker/caddy/Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    environment:
      DOMAIN: ${DOMAIN:-localhost}
    depends_on:
      dpkms:
        condition: service_healthy
    networks:
      - ctxt
    profiles:
      - prod

volumes:
  ctxt_data:
    name: ctxt_data
  ctxt_blobs:
    name: ctxt_blobs
  ctxt_dev_data:
    name: ctxt_dev_data
  caddy_data:
    name: caddy_data
  caddy_config:
    name: caddy_config
  go_cache:
    name: ctxt_go_cache
  node_modules:
    name: ctxt_node_modules

networks:
  ctxt:
    name: ctxt
    driver: bridge
```

**Step 2: Create `docker/air.toml`**

```toml
root = "."
tmp_dir = ".air"

[build]
  cmd = "CGO_ENABLED=1 go build -o ./tmp/dpkms ./cmd/dpkms"
  bin = "./tmp/dpkms serve --config docker/config.yaml --public"
  include_ext = ["go", "yaml", "sql"]
  exclude_dir  = ["vendor", "web", "docs", "tmp", "bin", ".git"]
  delay        = 500

[log]
  time = true

[color]
  main    = "magenta"
  watcher = "cyan"
  build   = "yellow"
  runner  = "green"

[misc]
  clean_on_exit = true
```

**Step 3: Create `.env.dev` template**

Create `docker/.env.dev.example` (copy to `.env.dev` to use):

```bash
# dpkms dev environment
DPKMS_WORKERS=2
CH_STORAGE_TYPE=sqlite
CTXT_DATA_DIR=/data/dpkms.db
CTXT_BLOB_BACKEND=local
CH_PUBLIC=true

# AI providers (optional for dev)
# CH_LLM_BACKEND=ollama
# CH_LLM_ENDPOINT=http://host.docker.internal:11434
# CH_LLM_MODEL=llama3.2
# CH_EMBEDDING_BACKEND=ollama
```

**Step 4: Update `.gitignore`**

Append to `.gitignore`:
```
.env.dev
.env.prod
.air/
tmp/
```

**Step 5: Verify dev stack**

```
docker compose --profile dev up -d dpkms-dev
```

Wait ~15s for air to compile, then:

```
curl -f http://localhost:8080/health
```

Expected output: `{"status":"ok"}` (or similar 200 response).

```
docker compose --profile dev logs dpkms-dev | grep "HTTP server listening"
```

Expected: `HTTP server listening on 0.0.0.0:8080`

**Step 6: Commit**

```
git add docker-compose.yml docker/air.toml docker/.env.dev.example
git commit -m "build(docker): add docker-compose.yml with dev profile and air hot-reload"
```

---

### Task 3: Prod profile + Caddy

**Goal:** Caddy v2 as TLS-terminating reverse proxy in front of `dpkms`. Automatic ACME TLS
for a real domain; self-signed for `localhost`. All state in named volumes.

**Files:**
- Create: `docker/caddy/Caddyfile`
- Create: `docker/.env.prod.example`

**Step 1: Create `docker/caddy/Caddyfile`**

```
{
    # Global options
    email {$ACME_EMAIL:admin@example.com}
}

{$DOMAIN:localhost} {
    # Reverse proxy to dpkms
    reverse_proxy dpkms:8080

    # Compress responses
    encode gzip

    # Security headers
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options    "nosniff"
        X-Frame-Options           "SAMEORIGIN"
        Referrer-Policy           "strict-origin-when-cross-origin"
        -Server
    }

    # Health endpoint passthrough (no auth)
    handle /health {
        reverse_proxy dpkms:8080
    }

    # API routes
    handle /api/* {
        reverse_proxy dpkms:8080
    }

    # UI (static files served by dpkms at /ui)
    handle /* {
        reverse_proxy dpkms:8080
    }

    log {
        output stdout
        format json
        level INFO
    }
}
```

**Note:** When `DOMAIN=localhost`, Caddy uses its internal CA (self-signed). For a real domain
set `DOMAIN=ctxt.example.com` and `ACME_EMAIL=you@example.com` in `.env.prod`.

**Step 2: Create `docker/.env.prod.example`**

```bash
# ── Required ─────────────────────────────────────────────────────────────────
# Domain for Caddy TLS (use real FQDN in production)
DOMAIN=ctxt.example.com

# ACME email for Let's Encrypt notifications
ACME_EMAIL=admin@example.com

# Image tag to deploy
TAG=latest

# ── dpkms ────────────────────────────────────────────────────────────────────
DPKMS_WORKERS=4
CH_STORAGE_TYPE=sqlite
CTXT_DATA_DIR=/data/dpkms.db

# Blob storage: "local" (default) or "s3"
CTXT_BLOB_BACKEND=local

# S3-compatible blob storage (optional)
# CTXT_BLOB_S3_ENDPOINT=https://s3.amazonaws.com
# CTXT_BLOB_S3_REGION=us-east-1
# CTXT_BLOB_S3_BUCKET=ctxt-blobs
# CTXT_BLOB_S3_ACCESS_KEY=
# CTXT_BLOB_S3_SECRET_KEY=

# ── AI providers (optional) ──────────────────────────────────────────────────
# CH_LLM_BACKEND=ollama
# CH_LLM_ENDPOINT=http://host.docker.internal:11434
# CH_LLM_MODEL=llama3.2
# CH_EMBEDDING_BACKEND=ollama

# ── Build metadata (set by CI) ───────────────────────────────────────────────
# VERSION=1.2.3
# GIT_COMMIT=abc1234
# BUILD_TIME=2026-03-13T00:00:00Z
```

**Step 3: Verify prod compose config**

```
docker compose --profile prod config
```

Expected: full merged YAML printed, no errors. Check that `caddy` service shows
`depends_on: dpkms: condition: service_healthy`.

**Step 4: Commit**

```
git add docker/caddy/Caddyfile docker/.env.prod.example
git commit -m "build(docker): add prod profile with Caddy v2 TLS reverse proxy"
```

---

### Task 4: Health checks + restart policies

**Goal:** All prod services have explicit healthchecks and `restart: unless-stopped`. Caddy
only starts after dpkms is healthy.

**Files:**
- Modify: `docker-compose.yml`

**Step 1: Add healthcheck to dpkms service**

In `docker-compose.yml`, the `dpkms` service under the prod profile must include:

```yaml
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:8080/health"]
      interval: 10s
      timeout: 3s
      start_period: 15s
      retries: 3
    restart: unless-stopped
```

The `caddy` service already declares `depends_on: dpkms: condition: service_healthy` in
Task 3's compose. Verify it is present.

**Step 2: Add healthcheck to caddy service**

```yaml
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:80"]
      interval: 15s
      timeout: 5s
      start_period: 20s
      retries: 3
    restart: unless-stopped
```

Note: Caddy serves HTTP on :80 and redirects to HTTPS on :443. The healthcheck uses :80
to avoid TLS cert issues in the check itself.

**Step 3: Verify health in prod mode**

```
docker compose --profile prod up -d
```

Wait 30s, then:

```
docker compose ps
```

Expected: both `ctxt-dpkms` and `ctxt-caddy` show `healthy` in the STATUS column.

```
docker inspect ctxt-dpkms --format '{{.State.Health.Status}}'
```

Expected: `healthy`

**Step 4: Commit**

```
git add docker-compose.yml
git commit -m "build(docker): add healthchecks and restart policies to prod services"
```

---

### Task 5: Makefile targets

**Goal:** Consistent `make docker-*` targets that use the new `docker-compose.yml` at repo
root. Replace the existing targets (which reference `docker/docker-compose.yml`).

**Files:**
- Modify: `Makefile`

**Step 1: Replace existing docker targets in `Makefile`**

Find the block beginning at `## docker-build:` through `## docker-docs:` and replace with:

```makefile
## docker-build: Build production runtime image
docker-build:
	@echo "Building ctxt runtime image..."
	docker compose build dpkms
	@echo "✓ Image built: ctxt/dpkms:latest"

## docker-dev: Start dev environment (hot-reload API + Vite UI)
docker-dev:
	@echo "Starting ctxt dev environment..."
	docker compose --profile dev up

## docker-prod: Start production stack in background (Caddy + dpkms)
docker-prod:
	@echo "Starting ctxt production stack..."
	docker compose --profile prod up -d
	@echo "✓ Running. Check status: make docker-ps"

## docker-down: Stop all ctxt containers
docker-down:
	@echo "Stopping ctxt containers..."
	docker compose --profile dev --profile prod down
	@echo "✓ Stopped"

## docker-logs: Tail logs from all running ctxt containers
docker-logs:
	docker compose logs -f

## docker-ps: Show status of all ctxt containers
docker-ps:
	docker compose ps

## docker-shell: Open a shell in the running dpkms container
docker-shell:
	docker exec -it ctxt-dpkms sh
```

Also update the `.PHONY` line at the top of `Makefile` to include the new targets:
```
docker-build docker-dev docker-prod docker-down docker-logs docker-ps docker-shell
```

**Step 2: Verify help output**

```
make help | grep docker
```

Expected lines (in any order):
```
  docker-build   Build production runtime image
  docker-dev     Start dev environment (hot-reload API + Vite UI)
  docker-prod    Start production stack in background (Caddy + dpkms)
  docker-down    Stop all ctxt containers
  docker-logs    Tail logs from all running ctxt containers
  docker-ps      Show status of all ctxt containers
  docker-shell   Open a shell in the running dpkms container
```

**Step 3: Commit**

```
git add Makefile
git commit -m "build(make): update docker-* targets to use root docker-compose.yml"
```

---

### Task 6: GitHub Actions CI

**Goal:** Build multi-arch Docker image on push to `main` and on PRs touching Docker files.
Push to GitHub Container Registry (GHCR) on `main` only.

**Files:**
- Create: `.github/workflows/docker.yml`

**Step 1: Create `.github/workflows/docker.yml`**

```yaml
name: Docker

on:
  push:
    branches:
      - main
    paths:
      - 'Dockerfile'
      - 'docker-compose.yml'
      - 'docker/**'
      - 'cmd/**'
      - 'internal/**'
      - 'go.mod'
      - 'go.sum'
  pull_request:
    paths:
      - 'Dockerfile'
      - 'docker-compose.yml'
      - 'docker/**'

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository_owner }}/ctxt-dpkms

jobs:
  build:
    name: Build & Push
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=ref,event=branch
            type=ref,event=pr
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=sha,prefix=sha-,format=short
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          file: Dockerfile
          target: runtime
          platforms: linux/amd64,linux/arm64
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          build-args: |
            VERSION=${{ github.ref_name }}
            GIT_COMMIT=${{ github.sha }}
            BUILD_TIME=${{ github.event.head_commit.timestamp }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Image digest
        if: github.event_name != 'pull_request'
        run: echo "Pushed ${{ steps.meta.outputs.tags }}"
```

**Step 2: Lint workflow file**

```
yamllint .github/workflows/docker.yml
```

Expected: no errors (warnings about line length are acceptable).

Alternative if `yamllint` not installed:

```
docker run --rm -v "$(pwd):/data" cytopia/yamllint .github/workflows/docker.yml
```

**Step 3: Verify with act (optional, requires `act` installed)**

```
act push --dry-run -W .github/workflows/docker.yml
```

Expected: workflow steps listed without execution errors.

**Step 4: Commit**

```
git add .github/workflows/docker.yml
git commit -m "ci(docker): add multi-arch image build and GHCR push workflow"
```

---

### Task 7: Documentation

**Goal:** Deployment quick-start for self-hosted users. Dev setup guide. Update root README.

**Files:**
- Create: `docs/deployment/docker.md`
- Create: `docs/deployment/docker-dev.md`
- Modify: `README.md` (Getting Started section)

**Step 1: Create `docs/deployment/docker.md`**

Five-command quick-start for production self-hosting:

```markdown
# Self-Hosted Deployment (Docker)

> Single-machine; Linux/macOS/Windows with Docker Engine + Compose v2.

## Prerequisites

- Docker Engine 24+ + Docker Compose v2
- Domain name pointing at your server (for TLS)
- Ports 80 and 443 open in firewall

## Quick Start

**1. Clone**

    git clone https://github.com/ideacrafterslabs/ctxt.git
    cd ctxt

**2. Configure**

    cp docker/.env.prod.example .env.prod
    $EDITOR .env.prod   # set DOMAIN= and ACME_EMAIL=

**3. Pull image**

    docker compose --profile prod pull

    # Or build locally:
    make docker-build

**4. Start**

    make docker-prod

**5. Verify**

    docker compose ps          # all services: healthy
    curl https://your-domain/health   # {"status":"ok"}

Done. Caddy handles TLS automatically via Let's Encrypt.

## Data persistence

| Volume | Contents |
|--------|----------|
| `ctxt_data` | SQLite database (`/data/dpkms.db`) |
| `ctxt_blobs` | Binary objects (PDFs, audio, images) |
| `caddy_data` | TLS certificates |
| `caddy_config` | Caddy runtime config |

Back up `ctxt_data` and `ctxt_blobs` volumes.

## Upgrade

    docker compose --profile prod pull
    docker compose --profile prod up -d

Migrations run automatically on startup.

## Logs

    make docker-logs

## Stop

    make docker-down
```

**Step 2: Create `docs/deployment/docker-dev.md`**

```markdown
# Local Dev Environment (Docker)

## Prerequisites

- Docker Engine 24+ + Docker Compose v2
- Go 1.25+ (for running tests on host)
- Make

## Start

    make docker-dev

Services started:
- `ctxt-dpkms-dev` — Go server with air hot-reload on :8080
- `ctxt-ui-dev` — Vite dev server on :5173 (when web/ui exists)

## Configure

    cp docker/.env.dev.example .env.dev
    $EDITOR .env.dev   # optional: set AI provider endpoints

## Verify

    curl http://localhost:8080/health
    # expected: {"status":"ok"}

## Hot reload

Edit any `.go` file. Air detects the change, rebuilds in ~3s, restarts.

## Stop

    make docker-down

## Ports

| Port | Service |
|------|---------|
| 8080 | dpkms HTTP API |
| 9090 | dpkms gRPC |
| 5173 | Vite dev server (UI) |
| 9377 | WebSocket (future) |

## Run tests on host (not in container)

    go test ./...

Tests use SQLite in-memory; no container needed.
```

**Step 3: Update `README.md` Getting Started section**

Find the existing "Getting Started" or "Quick Start" section in `README.md` and add Docker
references. If the section doesn't exist, add after the intro paragraph:

```markdown
## Quick Start

**Docker (recommended for production):**

    git clone https://github.com/ideacrafterslabs/ctxt.git && cd ctxt
    cp docker/.env.prod.example .env.prod && $EDITOR .env.prod
    make docker-prod

See [docs/deployment/docker.md](docs/deployment/docker.md) for full details.

**Docker (local dev with hot-reload):**

    make docker-dev

See [docs/deployment/docker-dev.md](docs/deployment/docker-dev.md).

**Build from source:**

    make build
    ./bin/dpkms serve
```

**Step 4: Commit**

```
git add docs/deployment/docker.md docs/deployment/docker-dev.md README.md
git commit -m "docs(docker): add deployment quick-start and dev setup guides"
```

---

## File Summary

| File | Action | Notes |
|------|--------|-------|
| `Dockerfile` | Create | Three-stage: ui-builder, go-builder, runtime |
| `docker-compose.yml` | Create | Root; replaces `docker/docker-compose.yml` usage |
| `docker/air.toml` | Create | Hot-reload config for dev container |
| `docker/caddy/Caddyfile` | Create | Caddy v2 reverse proxy + TLS template |
| `docker/.env.dev.example` | Create | Dev env var template |
| `docker/.env.prod.example` | Create | Prod env var template (gitignored without `.example`) |
| `.github/workflows/docker.yml` | Create | Multi-arch build + GHCR push |
| `Makefile` | Modify | Update `docker-*` targets |
| `docs/deployment/docker.md` | Create | Prod quick-start |
| `docs/deployment/docker-dev.md` | Create | Dev quick-start |
| `README.md` | Modify | Add Docker quick-start to Getting Started |

## Key Design Decisions

- **Root `Dockerfile`** — standard location; avoids `context: ..` hacks in compose.
- **`ui-builder` placeholder** — no-ops until `web/ui/` is scaffolded; same Dockerfile
  works before and after the UI exists.
- **`ctxt` binary CGO_ENABLED=0** — CLI has no SQLite dependency; stays fully static.
- **`dpkms` binary CGO_ENABLED=1** — needs sqlite-vec C extension; links against musl.
- **`air` installed at runtime in dev container** — avoids maintaining a separate dev image.
- **Caddy healthcheck on :80** — avoids TLS cert issues inside the container check.
- **GHCR push only on `main`** — PRs build and verify but don't publish.
- **`type=gha` cache** — GitHub Actions cache for Buildx layers; free speed-up.
