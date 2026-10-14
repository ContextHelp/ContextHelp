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

    cp docker/.env.prod.example docker/.env.prod
    $EDITOR docker/.env.prod   # set DOMAIN=, ACME_EMAIL= and DPKMS_BUS_TOKEN=

Compose does not read `docker/.env.prod` on its own: every `docker compose` command below passes `--env-file docker/.env.prod`. The file only feeds `${VAR}` interpolation in `docker-compose.yml` (image tag, Caddy's domain and ACME email, the bus token); dpkms settings live in its config files. Compose's default `.env` at the repo root is left to the host dev environment (`.env.example`).

**3. Create the auth secret**

The image's config sets `server.access: public`, and a public instance refuses to start without inbound auth. See [Secrets](#secrets).

    openssl rand -hex 32    # bus token: DPKMS_BUS_TOKEN= in the env file
    openssl rand -hex 32    # API token: one per device or person
    cp docker/dpkms-auth.example.yaml docker/dpkms-auth.yaml
    $EDITOR docker/dpkms-auth.yaml   # replace each REPLACE_ME with a token

serve refuses to start on the copied example as is: a `REPLACE_ME` token, or any token under 32 characters, stops it at startup.

**4. Create the settings file**

Blob storage and AI provider settings live in an operator file too. Compose requires it, but the example sets nothing, so an unedited copy keeps blobs on the `ctxt_blobs` volume and providers auto-detected. See [Blob storage and providers](#blob-storage-and-providers).

    cp docker/dpkms-local.example.yaml docker/dpkms-local.yaml

**5. Pull image**

    docker compose --env-file docker/.env.prod --profile prod pull

    # Or build locally:
    docker compose --env-file docker/.env.prod build dpkms

**6. Start**

    docker compose --env-file docker/.env.prod --profile prod up -d

**7. Verify**

    docker compose --env-file docker/.env.prod ps          # all services: healthy
    curl https://your-domain/health   # {"status":"ok"}

Done. Caddy handles TLS automatically via Let's Encrypt.

## Secrets

dpkms needs these secrets, none of which the image or the repo carries.

| Secret | Where | Read by |
|--------|-------|---------|
| Bus token | `DPKMS_BUS_TOKEN` in the env file | the bus hub at `/ws/bus`; serve exits without it |
| API tokens | `docker/dpkms-auth.yaml` (`server.auth`) | every `/api/v1` request and the web UI sign-in |
| S3 credentials | `docker/dpkms-local.yaml` (`storage.blob.s3`) | the blob store, when `storage.blob.backend: s3` |
| Provider API keys | `OPENAI_API_KEY` etc. in the env file | the AI providers; see [Blob storage and providers](#blob-storage-and-providers) |

Generate each token with `openssl rand -hex 32`. Compose refuses to start the stack while `DPKMS_BUS_TOKEN` is unset or empty.

`docker/dpkms-auth.yaml` is gitignored. Copy it from `docker/dpkms-auth.example.yaml`, which holds only `server.auth`:

```yaml
server:
  auth:
    provider: static
    static:
      tokens:
        - token: 3f9c...   # openssl rand -hex 32
          principal: admin
          roles: [admin]
        - token: 8a1d...
          principal: phone
          roles: [writer]   # capture only
```

Each token needs at least one role: `admin`, `writer` or `reader`. Write the token itself: config files do not expand `${VAR}`.

serve refuses to start while any token still contains `REPLACE_ME` (the example's placeholder, public in this repo) or is shorter than 32 characters. The error names the entry, for example `auth: static token [0]: token is the example placeholder (contains REPLACE_ME)`; replace that token with `openssl rand -hex 32` output and start again.

Compose mounts the file as the `dpkms-auth` secret at `/run/secrets/dpkms-auth.yaml`, and `docker/dpkms-local.yaml` as the `dpkms-local` secret at `/run/secrets/dpkms-local.yaml`. The image's command loads them as further `--config` layers after `/app/config.yaml`; later files win:

    serve --config /app/config.yaml --config /run/secrets/dpkms-auth.yaml --config /run/secrets/dpkms-local.yaml

Outside swarm, compose bind-mounts a file secret read-only with its host permissions. The container runs as uid 1000, so that user must be able to read both files: `chmod 600` plus `sudo chown 1000 docker/dpkms-auth.yaml docker/dpkms-local.yaml`, or `chmod 644` on a host only you use.

Without compose, mount them yourself:

    docker run -d -p 8080:8080 \
      -v "$PWD/docker/dpkms-auth.yaml:/run/secrets/dpkms-auth.yaml:ro" \
      -v "$PWD/docker/dpkms-local.yaml:/run/secrets/dpkms-local.yaml:ro" \
      -e DPKMS_BUS_TOKEN=... \
      -v ctxt_data:/data -v ctxt_blobs:/data/blobs \
      ctxt/dpkms

serve exits at config load when either file is missing. Compose also refuses to start without them.

To rotate a token, edit the file and `docker compose --env-file docker/.env.prod --profile prod restart dpkms`.

## Blob storage and providers

`docker/dpkms-local.yaml` is the operator's place for `storage.blob.*` and `providers.*`. It is gitignored; copy it from `docker/dpkms-local.example.yaml`. Every setting in the example is commented out, so the copy changes nothing until you uncomment a block:

```yaml
storage:
  blob:
    backend: s3
    s3:
      endpoint: https://s3.example.com
      region: us-east-1
      bucket: ctxt-blobs
      access_key: ...
      secret_key: ...
providers:
  llm:
    backend: openai
  embedding:
    backend: ollama
    endpoint: http://host.docker.internal:11434
```

Keys set there win over `/app/config.yaml`; the database path and every other image setting stay as they are. Endpoints must be reachable from inside the container. Backends and their keys: [`docs/ctxt/configuration.md`](../ctxt/configuration.md#blob-storage-configuration). The file is only read at start: `docker compose --env-file docker/.env.prod --profile prod restart dpkms` after editing it.

Provider API keys have no config key: providers read them from the environment. Set them in the env file; compose passes `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` and `OPENROUTER_API_KEY` to dpkms, empty when unset, which reads as not set. For an embedding provider, point `providers.embedding.api_key_env` at one of these names.

The S3 settings also have env vars, which win over every config file: `CTXT_BLOB_BACKEND`, `CTXT_BLOB_S3_ENDPOINT`, `CTXT_BLOB_S3_REGION`, `CTXT_BLOB_S3_BUCKET`, `CTXT_BLOB_S3_PREFIX`, `CTXT_BLOB_S3_ACCESS_KEY` and `CTXT_BLOB_S3_SECRET_KEY`. Compose does not pass them, so `docker/dpkms-local.yaml` stays the one place a compose deployment sets blob storage. With `docker run`, `-e CTXT_BLOB_S3_SECRET_KEY=...` keeps the secret key out of the file.

## Data persistence

`docker/config.docker.yaml` (copied into the image as `/app/config.yaml`) is the one place that sets in-container paths. Compose and the image set no storage env var: env overrides the file.

| Volume | Contents |
|--------|----------|
| `ctxt_data` | SQLite database (`storage.path: /data/dpkms.db`) |
| `ctxt_blobs` | Binary objects (PDFs, audio, images) at `storage.blob.local.path: /data/blobs` |
| `caddy_data` | TLS certificates |
| `caddy_config` | Caddy runtime config |

Back up `ctxt_data` and `ctxt_blobs` volumes. When using a remote blob backend (`storage.blob.backend: s3 | garage`) the `ctxt_blobs` volume is unused — back up the bucket out-of-band instead. See [`docs/ctxt/configuration.md`](../ctxt/configuration.md#blob-storage-configuration) for backend options.

Runtime files (pidfiles, upgrade state) stay in the container user's home, outside `/data`, and go with the container.

## Upgrade

    docker compose --env-file docker/.env.prod --profile prod pull
    docker compose --env-file docker/.env.prod --profile prod up -d

Migrations run automatically on startup.

## Logs

    docker compose --env-file docker/.env.prod logs -f

## Stop

    docker compose --env-file docker/.env.prod --profile prod down
