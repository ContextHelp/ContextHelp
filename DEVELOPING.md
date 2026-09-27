# Developing ContextHelp

How to build, run, and test this repo locally.

This document is the **local development loop**. For how to *propose* a change —
filing issues, ADR process, PR etiquette, review workflow, coding standards —
see [CONTRIBUTING.md](CONTRIBUTING.md).

| You want to... | Read |
|---|---|
| Build the binaries and run the tests | this file |
| Know which test tier needs Docker | this file |
| Propose a feature, file an issue, open a PR | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Install a released binary as a user | [INSTALL.md](INSTALL.md) |

---

## Toolchain

Tool versions are managed by [mise](https://mise.jdx.dev). From the repo root:

```bash
mise install
```

This installs the pinned Go toolchain plus `age` and `fnox`. Verify:

```bash
go version   # must match the `go` directive in go.mod
```

The Go version in `go.mod` is the single source of truth. `test/parity` has a
test (`TestGoVersionParity`) that fails if a Dockerfile or compose file drifts
from it, so bumping Go means bumping those together.

### CGo and the `fts5` build tag are mandatory

Two non-negotiable build settings, both enforced by the code rather than by
convention:

- **`CGO_ENABLED=1`** — the SQLite stack uses `mattn/go-sqlite3` and the
  `sqlite-vec` cgo bindings. With `CGO_ENABLED=0` the build fails with
  `build constraints exclude all Go files in .../sqlite-vec-go-bindings/cgo`.
- **`-tags fts5`** — migrations create FTS5 virtual tables. Without the tag the
  build fails on a deliberate tripwire symbol:
  `internal/storage/sqlite/sqlite3_nofts5.go:13:9: undefined:
  fts5BuildTagRequired_RebuildWith_tags_fts5`.

The Makefile sets both (`export CGO_ENABLED := 1`, `BUILD_TAGS := -tags fts5`),
so `make` targets are always correct. When invoking `go` directly, pass them
yourself:

```bash
CGO_ENABLED=1 go build -tags fts5 ./...
```

---

## Build

```bash
make build        # both binaries into bin/
make build-ctxt   # bin/ctxt  — the "brain" (pipelines, intelligence)
make build-dpkms  # bin/dpkms — the substrate (storage, graph, API, workers)
```

`make install` puts both on `$GOPATH/bin`. `make clean` removes `bin/`.

`make help` lists every target with its one-line description.

---

## Test tiers

Four tiers, with different prerequisites. Only the integration tier needs
Docker.

| Target | Scope | Needs |
|---|---|---|
| `make test-unit` | `./cmd/...`, `./internal/...` | nothing |
| `make test-integration` | `./test/integration/...` | Postgres + Redis |
| `make test-smoke` | `./test/smoke/...` | nothing |
| `make test-e2e` | `./test/e2e/...` against a built binary | Chrome/Chromium for the headless checks (optional) |
| `make test` | everything, with coverage | as above |
| `make test-all` | unit + integration + smoke | as above |

### Unit tests

No services, no network:

```bash
make test-unit
```

#### Tests never touch your real config or server

The `cmd/ctxt/cmd` and `cmd/dpkms/cmd` suites run isolated from your machine,
so it is safe to run them while your own `dpkms serve` listens on
`127.0.0.1:8080`. Their `TestMain` calls `testguard.Main`
(`internal/testguard`), which, before any test runs:

- points `HOME` and the `XDG_*` base dirs at a throwaway directory, which the
  `ctxt` binaries the e2e tests spawn inherit too;
- clears every `CTXT_*`, `CH_*` and `DPKMS_*` variable, such as `CTXT_CONFIG`
  and `CTXT_INSTANCE`;
- writes a user config whose `server.url` is `http://127.0.0.1:1`, a closed
  port, so a test that configures no server gets connection-refused;
- wraps `http.DefaultTransport` to refuse requests to the loopback ports a
  real instance listens on: `:8080` and `:8081` (HTTP), `:9090` and `:9091`
  (gRPC), `:9377` (cookie bridge). Any refusal prints `live-server guard:
  refused request to ... from <TestName>` and fails the run, even when the
  test itself passes.

Every new test package gets the same `TestMain`:

```go
func TestMain(m *testing.M) { os.Exit(testguard.Main(m, nil, "ctxt")) }
```

A helper that builds its own subprocess env must set `server.url` or
`server.urls` itself. It must never drop back to an empty config dir.

#### A dpkms for command tests

`setupTestDB(t)` in `cmd/ctxt/cmd` creates a temporary SQLite database and
starts an in-process dpkms over it (`internal/dpkmstest`). The instance is the
real router and service, built by the same `internal/server/stack.Build` that
`dpkms serve` uses, behind an `httptest` server on an ephemeral loopback port.
Seed through `db.Driver`; a command reaches the same data over the API.

```go
db := setupTestDB(t, dpkmstest.WithStaticTokens()) // protected; admin/writer/reader tokens
db.useRole(t, dpkmstest.RoleReader)                // the client now sends the reader token
out, err := db.exec("status")                      // routed to db.Server.URL
resp := db.Server.Request(t, http.MethodGet, "/api/v1/objects/"+id, dpkmstest.RoleAdmin, nil)
```

- `server.url`, plus `server.token` for a protected instance, goes into the
  user-level config (`db.ClientConfigPath`). `--config` layers over it, so a
  test that writes its own `server` section wins.
- Options: `WithStaticTokens()`, `WithAuthProvider(p)` to plug in another
  `auth.Provider`, `Unreachable()` for a closed port.
- The fixture is hermetic. Every provider backend is the stub, and the
  embedding provider is a stub on a closed port. No worker pool runs, so
  enqueued jobs stay pending.
- Outside `cmd/ctxt/cmd`, call `dpkmstest.Start(t, driver, opts...)`
  directly.
- A hand-built `httptest.Server` is still right for simulating a specific
  response that the real API can't produce on demand.

#### A dpkms binary for e2e tests

`testutil.StartDpkms(t)` (`test/testutil`) builds `./cmd/dpkms` once per test
binary and runs `dpkms serve` for the test:

- in a hermetic HOME and XDG root, with `CTXT_*`, `CH_*`, `DPKMS_*` and
  `BUS_TOKEN` removed from the inherited env;
- with a protected config (mode 0600), static admin, writer and reader tokens,
  and stub providers;
- on free ephemeral HTTP and gRPC ports. It reads the actual ports back from
  the instance's pidfile.

The process leads its own process group. Cleanup, or any failed start, sends
SIGTERM and then SIGKILL to the whole group. `d.WriteCtxtConfig(t, role)`
points a built `ctxt`, run with `d.Env`, at the instance. It is skipped under
`-short`.

A protected instance binds `0.0.0.0`, as `dpkms serve` does for every
non-private access class. Tests reach it on `127.0.0.1`. `PrivateDpkms()`
keeps it on loopback, without auth.

`cmd/ctxt/cmd` currently has a timing-sensitive failure
(`TestE2ECaptureEveryShortLoop`, "expected 2-5 captures in 280ms ... got 0") that
reproduces on a clean tree. It is unrelated to any change you are making; if it
is the only red package, you have not broken anything.

### E2E tests and headless Chrome

`make test-e2e` builds `ctxt` and drives it black-box in a throwaway `HOME`.
The search-graph suite also renders the viewer in headless Chrome when it finds
one; set `CTXT_CHROME` to a Chrome or Chromium binary to pick one, or
`CTXT_CHROME=off` to skip those checks.

Every browser ctxt starts, in the CLI or in tests, goes through
`internal/browser/launch`: it owns the default-browser handlers and the
headless switches (mock keychain, a throwaway profile removed afterwards, no
sync, no extensions), and a test there fails if a headless switch appears
anywhere else. For a manual look at a page with the same rules:

```bash
scripts/headless-chrome.sh dump graph.html                        # print the rendered DOM
scripts/headless-chrome.sh dump http://127.0.0.1:PORT/TOKEN/ -- --virtual-time-budget=1000
scripts/headless-chrome.sh path                                   # which Chrome would run
```

Extra Chrome switches go after `--`; a switch the package sets itself is
refused rather than overridden. A plain dump happens on the load event, before
any fetch the page starts then; `--virtual-time-budget` makes Chrome wait for
those fetches first, which is how the e2e suite checks the served viewer.

### Smoke tests

These boot a real `dpkms serve` through `testutil.StartDpkms`, with its own
bus token and hermetic dirs, then poll its health endpoint and round-trip an
analyze job:

```bash
make test-smoke
```

### Integration tests — the Docker tier

Integration tests are the only tier that needs containers. Bring up the
side-services first:

```bash
docker compose -f docker-compose.dev.yml up -d postgres redis
```

Postgres **must** be the pgvector-bundled image (`pgvector/pgvector:pg16`, which
is what the compose file pins). The Postgres driver's first migration is
`CREATE EXTENSION vector`; a plain `postgres:16` image cannot load it and every
test fails at migration 1.

Then run the tier, pointing it at those services:

```bash
POSTGRES_HOST=localhost POSTGRES_PORT=5432 \
POSTGRES_USER=ctxt POSTGRES_PASSWORD=ctxt POSTGRES_DB=ctxt \
REDIS_HOST=localhost REDIS_PORT=6379 \
go test -tags=integration,fts5 ./test/integration/...
```

Note that CI runs a **wider** set than `make test-integration` does. The
Postgres driver and embeddings-registry conformance suites live next to their
code, carry the same `integration` tag, and read the same `POSTGRES_*` block,
so CI lists them explicitly:

```bash
go test -tags=integration,fts5 \
  ./test/integration/... \
  ./internal/storage/postgres/... \
  ./internal/embeddings/registry/...
```

Run that form before pushing if you touched either area — `make
test-integration` alone will not cover them.

Tear down with `docker compose -f docker-compose.dev.yml down`.

### Running the suite in a container

When local Go does not match `go.mod`, or the CGo extensions misbehave on
macOS, run the whole suite inside the canonical Linux image:

```bash
make test-docker
```

---

## Lint and formatting

CI gates on three things, in this order:

```bash
golangci-lint run --timeout=5m   # pinned to v2.1.6 in CI
gofmt -l .                       # must print nothing
go vet -tags fts5 ./...
```

**Pin your local golangci-lint to the CI version.** Findings differ between
minor releases, so a newer local binary will disagree with CI in both
directions:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6
```

Formatting is fixed with `gofmt -w .` (or `make fmt`).

> **Heads up:** the tree currently carries a large pre-existing lint and
> formatting backlog, so a full-repo `golangci-lint run` or `gofmt -l .` will
> report findings that predate your change. Judge your own diff, not the
> repo-wide total. The pre-commit hooks below are scoped to changed files for
> exactly this reason.

### Pre-commit hooks

`.pre-commit-config.yaml` mirrors the CI lint gate, scoped to the files you are
actually committing. Install [pre-commit](https://pre-commit.com) and wire the
repo hooks:

```bash
make install-hooks
```

You do **not** run `pre-commit install`. That command refuses to run while
`core.hooksPath` is set — and this repo sets it to `.githooks` for the gitleaks
scan below. Instead, `.githooks/pre-commit` chains to `pre-commit hook-impl`
after the secret scan, so both gates run from one hook. If `pre-commit` is not
on your `PATH`, the lint gate is skipped and only the secret scan runs.

The hooks are scoped so the tree's pre-existing backlog does not block you:
`gofmt` and `go vet` see only the staged files, and `golangci-lint` runs with
`--new-from-rev=HEAD`, so only findings *your* diff introduces fail the commit.
Editing a file that already has findings is fine.

Run the hooks manually against your staged diff with `pre-commit run`, or
against everything with `pre-commit run --all-files` (expect a lot of
pre-existing findings).

### Secret scanning

Secret scanning runs locally rather than in CI, because `gitleaks-action`
requires a paid license for organization-owned repos:

```bash
make install-gitleaks   # one-time
make install-hooks      # wires .githooks (pre-commit gitleaks, pre-push chain)
make secret-scan-local  # scan the working tree on demand
```

---

## Other gates

```bash
make gosec        # static security analysis, fails on medium+ severity
make vuln-scan    # govulncheck + nancy
make eva          # JSON-shape contract tests (contracts/*.eva.yaml)
make check        # the pre-merge gate: test + eva
```

`make ben` runs the recall suites, but needs a local `hop.top/ben` checkout
pointed at by `BEN_LOCAL_PATH`; it has no published release yet.

---

## Running locally

```bash
make run-dpkms    # start the substrate: API, gRPC, workers
make run-ctxt     # the CLI
```

Copy `.env.example` to `.env` first. `scripts/dev-setup.sh` will also generate a
starting `.env` if you do not have one; it is not marked executable, so run it
as `bash scripts/dev-setup.sh`.

For a containerized dev environment with hot reload:

```bash
make docker-dev   # air-based hot-reload API + Vite UI
make docker-down  # stop everything
```

See [docs/decisions/2026-09-19-container-assets-audit.md](docs/decisions/2026-09-19-container-assets-audit.md)
for which compose file is canonical for which purpose.

---

## Web UI

The UI lives in `web/ui` (React 19 + Vite + TypeScript, pnpm):

```bash
cd web/ui && pnpm install && pnpm dev
```

`make build-ui` builds it and copies the output into `internal/ui/dist` for
embedding.

---

## Layout

```
cmd/          entry points for the ctxt and dpkms binaries
internal/     private application logic
pkg/          publicly importable packages
api/          REST and gRPC definitions
contracts/    eva JSON-shape contracts
test/         integration, smoke, e2e, and parity suites
web/ui/       React front end
docs/         documentation, ADRs, analyses
plugins/      plugin implementations
```

`test/parity` is worth knowing about: it holds static-analysis tests that catch
drift between `go.mod` and its copies in Dockerfiles, compose files, and CI
workflows. If you add a Dockerfile or compose file, register it there.
