# syntax=docker/dockerfile:1

# ─── Stage 1: UI builder ─────────────────────────────────────────────────────
FROM node:22-alpine AS ui-builder

WORKDIR /ui
# placeholder until web/ui is scaffolded
RUN mkdir -p dist && echo '{}' > dist/.keep

# ─── Stage 2: Go builder ─────────────────────────────────────────────────────
FROM golang:1.26-alpine AS go-builder

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

RUN GOWORK=off CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w \
      -X main.Version=${VERSION} \
      -X main.GitCommit=${GIT_COMMIT} \
      -X main.BuildTime=${BUILD_TIME}" \
    -o /out/dpkms ./cmd/dpkms && \
    GOWORK=off CGO_ENABLED=0 GOOS=linux go build \
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
