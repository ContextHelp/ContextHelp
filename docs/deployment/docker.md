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
    docker compose build dpkms

**4. Start**

    docker compose --profile prod up -d

**5. Verify**

    docker compose ps          # all services: healthy
    curl https://your-domain/health   # {"status":"ok"}

Done. Caddy handles TLS automatically via Let's Encrypt.

## Data persistence

| Volume | Contents |
|--------|----------|
| `ctxt_data` | SQLite database (`/data/dpkms.db`) |
| `ctxt_blobs` | Binary objects (PDFs, audio, images) when `blob.backend: local` |
| `caddy_data` | TLS certificates |
| `caddy_config` | Caddy runtime config |

Back up `ctxt_data` and `ctxt_blobs` volumes. When using a remote blob backend (`backend: s3 | garage`) the `ctxt_blobs` volume is unused — back up the bucket out-of-band instead. See [`docs/ctxt/configuration.md`](../ctxt/configuration.md#blob-storage-configuration) for backend options.

## Upgrade

    docker compose --profile prod pull
    docker compose --profile prod up -d

Migrations run automatically on startup.

## Logs

    docker compose logs -f

## Stop

    docker compose --profile prod down
