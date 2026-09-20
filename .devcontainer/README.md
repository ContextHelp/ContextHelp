# Dev container

Opens the repo in a Linux container that can build and test `ctxt` with no
further setup, including the Postgres-backed integration suites.

## What you get

| | |
|---|---|
| Go | matches `GO_VERSION` in `.github/workflows/ci.yml` |
| golangci-lint | matches the version the CI workflow installs |
| SQLite headers | `libsqlite3-dev`, for the `CGO_ENABLED=1` + `-tags fts5` build |
| Postgres | `pgvector/pgvector:pg16`, same image and credentials as CI |
| Redis | `redis:7-alpine`, same image as CI |

## Using it

Open the folder in a devcontainer-aware editor, or from the CLI:

```
devcontainer up --workspace-folder .
```

`postCreateCommand` runs `make build`, so a broken toolchain surfaces at
create time rather than mid-task. After that:

```
make test                        # unit tests
make test-integration-services   # integration suites, needs the services
```

`make test-integration-services` covers all three build-tagged trees — the
suites under `test/integration/` plus the Postgres driver and embeddings
registry suites that live next to the code they cover. `make
test-integration` runs only the first of those and needs no services.

## Notes

- The `POSTGRES_*` / `REDIS_*` env block is set on the container, so a bare
  `go test -tags=integration,fts5 ...` behaves the same as the make target.
- `CGO_ENABLED=1` and `-tags fts5` are baked into the image. Without the tag
  the build fails on a sentinel symbol rather than a clear error, so a plain
  `go build ./...` in a shell here already carries it.
- The services are not published to host ports; only the workspace container
  reaches them. This keeps them from colliding with `docker-compose.dev.yml`
  or a Postgres already running on the host.
- The database is disposable. `docker compose -f .devcontainer/docker-compose.yml down -v`
  resets it.
- Tool versions are hardcoded here to match CI. If the repo later pins them
  in `mise.toml`, this image should read them from there instead.
