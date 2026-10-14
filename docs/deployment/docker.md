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

**4. Pull image**

    docker compose --env-file docker/.env.prod --profile prod pull

    # Or build locally:
    docker compose --env-file docker/.env.prod build dpkms

**5. Start**

    docker compose --env-file docker/.env.prod --profile prod up -d

**6. Verify**

    docker compose --env-file docker/.env.prod ps          # all services: healthy
    curl https://your-domain/health   # {"status":"ok"}

Done. Caddy handles TLS automatically via Let's Encrypt.

## Secrets

dpkms needs two secrets, neither of which the image or the repo carries.

| Secret | Where | Read by |
|--------|-------|---------|
| Bus token | `DPKMS_BUS_TOKEN` in the env file | the bus hub at `/ws/bus`; serve exits without it |
| API tokens | `docker/dpkms-auth.yaml` (`server.auth`) | every `/api/v1` request and the web UI sign-in |

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

Compose mounts the file as the `dpkms-auth` secret at `/run/secrets/dpkms-auth.yaml`. The image's command loads it as a second `--config` after `/app/config.yaml`:

    serve --config /app/config.yaml --config /run/secrets/dpkms-auth.yaml

Outside swarm, compose bind-mounts a file secret read-only with its host permissions. The container runs as uid 1000, so that user must be able to read the file: `chmod 600` plus `sudo chown 1000 docker/dpkms-auth.yaml`, or `chmod 644` on a host only you use.

Without compose, mount it yourself:

    docker run -d -p 8080:8080 \
      -v "$PWD/docker/dpkms-auth.yaml:/run/secrets/dpkms-auth.yaml:ro" \
      -e DPKMS_BUS_TOKEN=... \
      -v ctxt_data:/data -v ctxt_blobs:/data/blobs \
      ctxt/dpkms

serve exits at config load when `/run/secrets/dpkms-auth.yaml` is missing.

To rotate a token, edit the file and `docker compose --env-file docker/.env.prod --profile prod restart dpkms`.

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
