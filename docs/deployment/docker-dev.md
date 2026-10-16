# Local Dev Environment (Docker)

## Prerequisites

- Docker Engine 24+ + Docker Compose v2
- Go 1.24+ (for running tests on host)

## Start

The dev server runs the image's config (`server.access: public`), so it needs the same secrets as prod: the bus token, `docker/dpkms-auth.yaml` and `docker/dpkms-local.yaml`. See [Secrets](docker.md#secrets).

    cp docker/dpkms-auth.example.yaml docker/dpkms-auth.yaml
    $EDITOR docker/dpkms-auth.yaml   # replace REPLACE_ME: openssl rand -hex 32
    cp docker/dpkms-local.example.yaml docker/dpkms-local.yaml   # sets nothing until edited
    cp docker/.env.dev.example docker/.env.dev
    $EDITOR docker/.env.dev          # DPKMS_BUS_TOKEN=: openssl rand -hex 32
    docker compose --env-file docker/.env.dev --profile dev up

Compose does not read `docker/.env.dev` on its own: pass `--env-file docker/.env.dev` to every `docker compose` command. It only feeds `${VAR}` interpolation in `docker-compose.yml`; no container sees its other keys.

Services started:
- `ctxt-dpkms-dev` — Go server with air hot-reload on :8080
- `ctxt-ui-dev` — Vite dev server on :5173 (when web/ui exists)

## Configure

dpkms settings (storage, workers) are config keys in `docker/config.docker.yaml`, which air serves with; edits reload like code. Blob storage and provider settings for your own setup go in `docker/dpkms-local.yaml`, provider API keys and optionally the non-secret S3 settings in `docker/.env.dev` ([Blob storage and providers](docker.md#blob-storage-and-providers)); air does not watch the secret, so restart `dpkms-dev` after editing either.

## Verify

    curl http://localhost:8080/health
    # expected: {"status":"ok"}
    curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/whoami

## Hot reload

Edit any `.go` file. Air detects the change, rebuilds in ~3s, restarts.

## Stop

    docker compose --env-file docker/.env.dev --profile dev down

## Ports

| Port | Service |
|------|---------|
| 8080 | dpkms HTTP API |
| 9090 | dpkms gRPC |
| 5173 | Vite dev server (UI) |

## Run tests on host (not in container)

    go test ./...

Tests use SQLite in-memory; no container needed.
