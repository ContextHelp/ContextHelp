# Local Dev Environment (Docker)

## Prerequisites

- Docker Engine 24+ + Docker Compose v2
- Go 1.24+ (for running tests on host)
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
