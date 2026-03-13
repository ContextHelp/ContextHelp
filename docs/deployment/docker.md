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
