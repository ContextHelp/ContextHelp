# Local Dev Environment (Docker)

## Prerequisites

- Docker Engine 24+ + Docker Compose v2
- Go 1.24+ (for running tests on host)

## Start

The dev server runs the image's config (`server.access: public`), so it needs the same secrets as prod: the bus token and `docker/dpkms-auth.yaml`. See [Secrets](docker.md#secrets).

    cp docker/dpkms-auth.example.yaml docker/dpkms-auth.yaml
    $EDITOR docker/dpkms-auth.yaml   # replace REPLACE_ME: openssl rand -hex 32
    export DPKMS_BUS_TOKEN=$(openssl rand -hex 32)
    docker compose --profile dev up

Services started:
- `ctxt-dpkms-dev` — Go server with air hot-reload on :8080
- `ctxt-ui-dev` — Vite dev server on :5173 (when web/ui exists)

## Configure

    cp docker/.env.dev.example .env.dev
    $EDITOR .env.dev   # optional: set AI provider endpoints

## Verify

    curl http://localhost:8080/health
    # expected: {"status":"ok"}
    curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/whoami

## Hot reload

Edit any `.go` file. Air detects the change, rebuilds in ~3s, restarts.

## Stop

    docker compose --profile dev down

## Ports

| Port | Service |
|------|---------|
| 8080 | dpkms HTTP API |
| 9090 | dpkms gRPC |
| 5173 | Vite dev server (UI) |

## Run tests on host (not in container)

    go test ./...

Tests use SQLite in-memory; no container needed.
