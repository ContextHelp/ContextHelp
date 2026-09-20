# syntax=docker/dockerfile:1

# ─── Stage 1: UI builder ─────────────────────────────────────────────────────
FROM node:22-alpine AS ui-builder

WORKDIR /ui
# placeholder until web/ui is scaffolded
RUN mkdir -p dist && echo '{}' > dist/.keep

# ─── Stage 2: Go builder ─────────────────────────────────────────────────────
FROM golang:1.26.8-bookworm AS go-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates tzdata gcc libc6-dev libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy UI dist from ui-builder (embedded into dpkms binary or served from /app/web)
COPY --from=ui-builder /ui/dist ./web/ui/dist

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

# ─── Stage 3: Runtime ────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata libsqlite3-0 wget \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd -r ctxt && useradd -r -g ctxt -u 1000 ctxt

WORKDIR /app

COPY --from=go-builder /out/dpkms /app/dpkms
COPY --from=go-builder /out/ctxt   /app/ctxt
COPY docker/config.docker.yaml     /app/config.yaml

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
