# Developing ContextHelp

How to build, run, and test this repo locally.

This document is the **local development loop**. For how to *propose* a change —
filing issues, ADR process, PR etiquette, review workflow, coding standards —
see [CONTRIBUTING.md](CONTRIBUTING.md).

| You want to... | Read |
|---|---|
| Know what to run before you push | [Before you push](#before-you-push) |
| Build the binaries and run the tests | this file |
| Know which test tier needs Docker | this file |
| Propose a feature, file an issue, open a PR | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Install a released binary as a user | [INSTALL.md](INSTALL.md) |

---

## Before you push

```bash
make ci
```

`make ci` runs every job of `.github/workflows/ci.yml` on your machine:
the same make targets each CI step calls, in the same order, with the
`mise.toml` tool versions. Jobs keep going after a failure, as in CI, and a
summary lists each job as PASS, FAIL or SKIP. A green `make ci` means CI
goes green, apart from the platform gaps listed below.

One job at a time: `make ci-<job>`, for example `make ci-lint` or
`make ci-test`. The job names are the ones in `ci.yml`.

A job that needs something a CI runner provides and your machine lacks is
SKIPPED, with the command that provides it. Skips never count as a pass:
the run ends `INCOMPLETE`, and `CI_STRICT=1 make ci` exits non-zero on
one. What cannot match CI locally, and what the runner does about it:

| Gap | Local run |
|---|---|
| `test` runs on Linux **and** macOS runners | runs once, on your OS |
| `integration` needs the job's Postgres service | SKIP unless Postgres is reachable; the skip prints the `docker run` for CI's image and credentials |
| `windows` cross-compiles with mingw-w64 | SKIP without `x86_64-w64-mingw32-gcc` (`brew install mingw-w64`) |
| e2e headless viewer checks need Chrome | run when Chrome is found, else a PARTIAL notice (the tests skip themselves) |
| Trivy scan, SARIF and Codecov uploads, build artifact | CI only; they report and never block |
| `go.mod` local-path `replace` for `hop.top/kit` or `hop.top/c12n` | CI drops it; `make ci` fails until you remove it |
| a local `go.work` | ignored (`GOWORK=off`), as CI has none |
| secrets | none needed: CI uses none |

`make lint-new` lints only the findings your branch adds, against the merge
base with `origin/next` (`LINT_BASE=<commit>` or `LINT_BASE_REF=<branch>` to
override). It is a quick local check. The gate CI runs is the full scan.

---

## Toolchain

Tool versions are managed by [mise](https://mise.jdx.dev). From the repo root:

```bash
mise install
```

This installs every tool `mise.toml` pins: Go, golangci-lint, gitleaks, and
the Node version that builds the embedded web bundles (`web/ui`,
`web/searchgraph`). Verify:

```bash
go version   # must match the `go` pin in mise.toml
```

Every `make ci-*`, lint and pre-commit path runs its tools through
`scripts/ci/tools.sh`, which picks mise's install of the pin and fails when
the tool is missing or another version is first on `PATH`. It never falls
back to whatever is installed. In a fresh worktree, run `mise trust` once.

`mise.toml` is the single source of truth for tool versions. CI reads its
`go`, `golangci-lint` and `node` pins at run time, and `make toolchain-check`
fails when anything else disagrees: a Dockerfile, compose file or workflow
image, a hardcoded `go-version`/`node-version`, `.nvmrc`/`.node-version`,
`package.json` `engines.node`, or a workflow action or tool on a floating ref
(`@master`, `@latest`, `~> v2`). The `go` directive in `go.mod` is a minimum
the pin must satisfy, not the version you build with. Bump a tool in
`mise.toml` first, then run `make toolchain-check` to find the stragglers.

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
| `make test-integration` | `./test/integration/...` without the `integration` tag | nothing |
| `make test-integration-services` | CI's integration job (alias of `make ci-integration`) | Postgres |
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

A test that spawns `dpkms serve` passes free ports for `--port` and
`--grpc-port` and `--cookie-bridge-port 0`, so the instance never binds a
default port (`test/testutil.StartDpkms` does all three).

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
docker compose -f docker-compose.dev.yml up -d postgres
```

Postgres **must** be the pgvector-bundled image
(`pgvector/pgvector:0.8.6-pg16-bookworm`, which is what the compose file pins). The Postgres driver's first migration is
`CREATE EXTENSION vector`; a plain `postgres:16` image cannot load it and every
test fails at migration 1.

Then run the tier, pointing it at those services:

```bash
POSTGRES_HOST=localhost POSTGRES_PORT=5432 \
POSTGRES_USER=ctxt POSTGRES_PASSWORD=ctxt POSTGRES_DB=ctxt \
go test -tags=integration,fts5 ./test/integration/...
```

That is only the first tree CI covers. The Postgres driver, storage factory
and embeddings-registry suites live next to their code, carry the same
`integration` tag and read the same `POSTGRES_*` block, and the job-queue,
HTTP handler, embeddings journey and search-graph Postgres tests run by name.
`make ci-integration` runs exactly the commands of CI's integration job.
Its `POSTGRES_*` defaults are the CI service's (`contexthelp` /
`test_password` / `contexthelp_test` on `localhost:5432`); export the block
above to use the compose Postgres instead. Run it before pushing if you
touched storage, jobs, the HTTP server or embeddings.

Tear down with `docker compose -f docker-compose.dev.yml down`.

### Running the suite in a container

When local Go does not match `go.mod`, or the CGo extensions misbehave on
macOS, run the whole suite inside the canonical Linux image:

```bash
make test-docker
```

---

## Lint and formatting

```bash
make ci-lint
```

That is CI's lint job: golangci-lint on findings new since the merge base
(blocking), the full-repo scan (advisory, as in CI), `gofmt -l .` and
`go vet -tags fts5 ./...`. golangci-lint is the `mise.toml` pin;
findings differ between releases, so no other version is accepted.

Formatting is fixed with `gofmt -w .` (or `make fmt`).

The full scan (`make ci-lint-full`, `make lint`) is the blocking gate, in CI
and in `make ci-lint`. The tree is at zero findings at the pinned version, so
any finding the scan reports is yours to fix. `make lint-new` narrows the run
to your branch when you want a faster loop.

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
on your `PATH`, the lint gate does not run and every commit prints a warning
saying so.

The hooks are scoped for speed: `gofmt` and `go vet` see only the staged
files, and `golangci-lint` runs with `--new-from-rev=HEAD` over the staged
packages, so only findings your commit introduces fail it. The full scan in
CI catches anything that scope misses. All three use the pinned
tools: a missing or different golangci-lint fails the commit instead of
being skipped.

Run the hooks manually against your staged diff with `pre-commit run`, or
against everything with `pre-commit run --all-files` (expect a lot of
pre-existing findings).

### Secret scanning

The pre-commit hook scans staged changes; CI's `secret-scan` job scans
the full history with the same pinned gitleaks:

```bash
make install-gitleaks   # one-time; the version mise.toml pins
make install-hooks      # wires .githooks (pre-commit gitleaks, pre-push chain)
make secret-scan-local  # scan the working tree (tracked + untracked) on demand
make security-scan      # scan the history of HEAD, minus the .gitleaksignore baseline
```

See [docs/git-hooks.md](docs/git-hooks.md#history-scan-and-baseline) for
the baseline.

---

## Other gates

```bash
make gosec        # static security analysis, fails on medium+ severity (CI security job)
make govulncheck  # known vulnerabilities in reachable code (CI security job)
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

Nothing loads `.env` automatically. To use one, copy `.env.example` to `.env`
and export it into your shell first: `set -a; . ./.env; set +a`.

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

`make build-ui` rebuilds it straight into `internal/ui/dist` (Vite's
`outDir`), the committed bundle `go:embed` serves at `/ui`. Commit the result
alongside any `web/ui` source change.

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
extensions/   integrations outside ctxt (Raycast), each with a catalog.json
skills/       agent skills, each with a catalog.json
```

`test/parity` is worth knowing about: it holds static-analysis tests that catch
drift between `go.mod` and its copies in Dockerfiles, compose files, and CI
workflows. If you add a Dockerfile or compose file, register it there.
