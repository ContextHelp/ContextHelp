# syntax=docker/dockerfile:1.27.0

# ─── Stage 1: Go builder ─────────────────────────────────────────────────────
FROM golang:1.26.9-bookworm AS go-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates tzdata gcc libc6-dev libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The web UI is the committed internal/ui/dist bundle, brought in by COPY . .
# and embedded via go:embed; `make build-ui` refreshes it. No node stage.

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Canonical build for both binaries: CGo on (mattn/go-sqlite3 + sqlite-vec)
# and -tags fts5 so migrations can create FTS5 virtual tables.
RUN GOWORK=off CGO_ENABLED=1 GOOS=linux go build -tags fts5 \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X main.GitCommit=${GIT_COMMIT} \
      -X main.BuildTime=${BUILD_TIME}" \
    -o /out/dpkms ./cmd/dpkms && \
    GOWORK=off CGO_ENABLED=1 GOOS=linux go build -tags fts5 \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X main.GitCommit=${GIT_COMMIT} \
      -X main.BuildTime=${BUILD_TIME}" \
    -o /out/ctxt ./cmd/ctxt

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM debian:bookworm-20260918-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata libsqlite3-0 wget \
    && rm -rf /var/lib/apt/lists/*

# A real home: runtime files (pidfiles under the XDG data dir) need a
# writable place outside the /data volume, so they go with the container.
RUN groupadd -r ctxt && useradd -r -m -d /home/ctxt -g ctxt -u 1000 ctxt

WORKDIR /app

COPY --from=go-builder /out/dpkms /app/dpkms
COPY --from=go-builder /out/ctxt   /app/ctxt
COPY docker/config.docker.yaml     /app/config.yaml

# Volume mount points; config.yaml puts the database and blobs here.
RUN mkdir -p /data/blobs && chown -R ctxt:ctxt /data /app

USER ctxt

EXPOSE 8080 9090

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/health || exit 1

# config.yaml sets server.access: public, which needs server.auth. That
# comes from the operator's secret, never the image: mount it at
# /run/secrets/dpkms-auth.yaml (docker-compose.yml does; template at
# docker/dpkms-auth.example.yaml) and set DPKMS_BUS_TOKEN. Blob storage
# and provider settings come from /run/secrets/dpkms-local.yaml
# (template: docker/dpkms-local.example.yaml, which changes nothing as
# copied). Without either file serve exits at config load.
ENTRYPOINT ["/app/dpkms"]
CMD ["serve", "--config", "/app/config.yaml", "--config", "/run/secrets/dpkms-auth.yaml", "--config", "/run/secrets/dpkms-local.yaml"]
