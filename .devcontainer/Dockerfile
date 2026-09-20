# Devcontainer image for ContextHelp (ctxt).
#
# Mirrors the toolchain the CI workflow and the production Dockerfile use so
# a first boot can build and test without further setup:
#   - Go, matching the `go` pin in mise.toml (kept in sync by
#     scripts/check-toolchain-pins.sh).
#   - The SQLite headers CGo needs. Both binaries build with CGO_ENABLED=1
#     and -tags fts5 (mattn/go-sqlite3 + sqlite-vec); without libsqlite3-dev
#     the build fails, and without the fts5 tag it fails on a sentinel
#     symbol. The CI workflow gets these headers for free from the hosted
#     runner image and so carries no install step; a container must be
#     explicit.
#   - The Postgres client, so `psql` is on PATH for poking at the database
#     service by hand.
ARG GO_VERSION=1.26.8
FROM golang:${GO_VERSION}-bookworm

# golangci-lint version is pinned to the one the CI workflow installs.
# Keep the two in step: a local run that passes against a different minor
# is not evidence CI will pass.
ARG GOLANGCI_LINT_VERSION=v2.1.6

ENV DEBIAN_FRONTEND=noninteractive

# gcc/libc6-dev/libsqlite3-dev: the CGo build set, same as the production
# Dockerfile's builder stage. libsqlite3-0 comes in as a dependency.
# postgresql-client: `psql` for manual inspection of the db service.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gcc \
        git \
        libc6-dev \
        libsqlite3-dev \
        make \
        postgresql-client \
        sudo \
        tzdata \
    && rm -rf /var/lib/apt/lists/*

# Non-root user. The golang image has no unprivileged user of its own, so
# create the `vscode` account the devcontainer spec's remoteUser expects.
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID --shell /bin/bash --create-home $USERNAME \
    && echo "$USERNAME ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

RUN GOBIN=/usr/local/bin go install \
        github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}

# Module and build caches both live under /go so a single chown covers them,
# and this must run AFTER every root-run `go install` above: those populate
# /go/pkg/mod/cache/download/... as root, and chowning before them leaves
# root-owned subdirectories that fail later with "permission denied" even
# though /go/pkg/mod itself looks correct.
#
# Ownership also has to be right in the image, not just at runtime, because
# the compose file mounts these two paths as named volumes: Docker seeds a
# fresh named volume from the image's content at that path, ownership
# included. Getting it wrong gives every developer a first-boot build
# failure on a cold volume.
RUN mkdir -p /go/pkg/mod /go/bin /go/cache/go-build \
    && chown -R $USER_UID:$USER_GID /go

# A login shell sources /etc/profile, which overwrites PATH from scratch and
# drops /usr/local/go/bin — so `bash -lc 'go build'` fails with "go: command
# not found" even though the ENV below is correct for non-login shells. VS
# Code terminals and lifecycle hooks commonly use login shells, so the
# toolchain has to be on PATH there too.
RUN printf 'export PATH="/usr/local/go/bin:/go/bin:$PATH"\n' \
        > /etc/profile.d/10-go-path.sh \
    && chmod 0644 /etc/profile.d/10-go-path.sh

USER $USERNAME

# CGo on and the fts5 tag are not optional for this repo; setting them in
# the image means a bare `go test ./...` in a shell behaves like `make test`.
# GOPATH and /go/bin are already set by the golang base image; repeating
# them here would only duplicate the PATH entry.
ENV CGO_ENABLED=1 \
    GOFLAGS=-tags=fts5 \
    GOCACHE=/go/cache/go-build
