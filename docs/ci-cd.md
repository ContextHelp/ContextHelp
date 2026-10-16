# CI/CD Pipeline

GitHub Actions workflows for `ctxt` and `dpkms`, under `.github/workflows/`.
This page describes what each one runs. For `ci.yml`, the Makefile holds
the commands and the workflow calls them.

**Before you push, run `make ci`.** It runs every `ci.yml` job locally;
see [Local parity](#local-parity).

---

## At a glance

| Workflow | Runs on | What it gates |
|---|---|---|
| `ci.yml` (CI) | push to `main`/`next`, PR into `main`/`next`, manual, and called by `release.yml` | every merge and every release |
| `release.yml` (Release) | push of a `v*.*.*` tag | publishing a release |
| `vuln-scan.yml` | PR into `main`/`next`, nightly 03:00 UTC, manual | govulncheck + nancy |
| `trivy.yml` | PR touching Docker/`go.mod` files, nightly 02:00 UTC, manual | Trivy config, filesystem and published-image scans |
| `docker.yml` | push to `main` touching Docker or Go sources; PRs touching Docker files | multi-arch image build; pushes to GHCR on `main` only |
| `release-notes-check.yml` | PR into `main`/`next`, manual | `Operator-Impact:` trailers have a release-notes row |
| `eva-contracts.yml` | push to `main`/`next`; PRs touching contracts or the code behind the JSON shapes they lock; manual | operator-facing JSON shapes |
| `lateral-eval.yml` | PRs touching `internal/lateral/**`, manual | lateral eval harness regression vs the base branch |
| `ben.yml` | PRs touching retrieval code, manual | recall floor; the job is disabled (`if: false`) until `hop.top/ben` publishes a tag |

---

## Toolchain versions

`mise.toml` pins Go, golangci-lint, Node, gitleaks and GoReleaser. No workflow names
those versions: each workflow's `toolchain` job reads them from `mise.toml`
and the other jobs consume its outputs. Bump a version in `mise.toml` only.

`scripts/check-toolchain-pins.sh` (`make toolchain-check`, and the first
step of CI) fails when anything disagrees:

- Dockerfile, compose and workflow images run the pinned Go and Node, and
  every image names an exact release, never a floating tag;
- `go.mod`'s `go` directive is satisfied by the pin;
- no workflow hardcodes a Go, Node, golangci-lint or GoReleaser version
  (`release.yml` takes GoReleaser's from `ci.yml`'s `goreleaser-version`
  output);
- no workflow uses `@master`, `@main`, `@latest`, a `~>` range or
  `version: latest`;
- the Makefile, Taskfile and `scripts/` install no Go tool at `@latest`
  or a branch, and any golangci-lint they install is the v2 module at the
  pin;
- `release.yml` runs `ci.yml` ahead of every other job (see
  [Release](#release)).

gosec and govulncheck have no `mise.toml` entry. The Makefile pins them
(`GOSEC_VERSION`, `GOVULNCHECK_VERSION`) and runs them with
`go run <pkg>@<version>`, in CI and locally alike.
`scripts/check-ci-parity.sh` fails when a workflow, the Taskfile or a script
names another version. Pinned in place: nancy `v1.2.0`, Trivy action
`v0.36.0`.

### Local-path replaces

Every Go job in `ci.yml` first runs `make ci-drop-replaces`, which drops
any `replace` of the `CI_DROP_REPLACES` modules (`hop.top/kit`,
`hop.top/c12n`) so they resolve through the module proxy. It is a no-op
when `go.mod` carries no such directive. Locally the same target never
edits `go.mod`. It fails instead, because a build with the replace in place
is not the one CI tests.

---

## CI (`ci.yml`)

All jobs except `toolchain` start once `toolchain` has resolved the pins.
`build` also waits for `lint`, `security`, `secret-scan` and `test`.

Every check step is `make <target>`; `make ci-<job>` runs a job's targets
in its step order. The commands, flags, tags and env live in those
targets.

| Job | Steps | Local equivalent |
|---|---|---|
| `toolchain` | read pins from `mise.toml`; `scripts/check-toolchain-pins.sh`; `scripts/check-ci-parity.sh`; `goreleaser check` of `.goreleaser.yaml` at the pin (deprecations are reported, an invalid config fails) | `make ci-toolchain` |
| `secret-scan` | gitleaks release tarball (checksum-verified) over full history, minus `.gitleaksignore` | `make ci-secret-scan` |
| `lint` | golangci-lint on findings new since the PR base (`HEAD~1` on a push); full scan, advisory; `gofmt -l .`; `go vet -tags fts5 ./...` | `make ci-lint` |
| `windows` | `go vet -tags fts5 ./...`, then `go build` of `ctxt` and `dpkms`, for windows/amd64 with CGO and mingw-w64, as GoReleaser builds them | `make ci-windows` (needs mingw-w64) |
| `security` | gosec (medium+ severity and confidence, `-tags fts5`, SARIF upload); govulncheck `-tags fts5`; Trivy filesystem scan (SARIF upload, does not fail the job) | `make ci-security` (no Trivy) |
| `test` | Ubuntu **and** macOS: `go test -v -short -race -tags fts5 ./...` with coverage (uploaded to Codecov from Ubuntu), then the e2e suites: `go test -v -count=1 -tags 'fts5 e2e' ./test/e2e/...` | `make ci-test` (your OS only) |
| `viewer` | search-graph viewer JS tests; embedded viewer bundle is current | `make ci-viewer` |
| `webui` | embedded web UI bundle is current | `make ci-webui` |
| `integration` | Postgres service (`pgvector/pgvector:0.8.6-pg16-bookworm`); integration-tagged suites in `test/integration/`, `internal/storage/postgres/`, `internal/storageutil/`, `internal/embeddings/registry/`; then by name: job-queue claim and re-projection, HTTP handlers, embeddings journey and search-graph e2e, each on Postgres | `make ci-integration` (needs Postgres) |
| `build` | `go build -tags fts5` of `dpkms` and `ctxt`; uploads `bin/` as the `binaries` artifact (7 days) | `make ci-build` |

Every Go build and test uses `-tags fts5` with `CGO_ENABLED=1`: the storage
layer needs SQLite's FTS5 module, and gosec in particular analyses nothing
in 34 packages without the tag.

Every cgo compile also takes `sqlite3.h` from `build/cgo-include`, not the
system. sqlite-vec includes it, and the macOS SDK's, a distro's
`libsqlite3-dev` and mingw-w64 (which has none) each differ from the
SQLite amalgamation `mattn/go-sqlite3` compiles and the binaries link.
`scripts/ci/sqlite3-include.sh` copies that module's header, at the
`go.mod` version, into the directory. The Makefile exports
`CGO_CFLAGS=-O2 -g -I<repo>/build/cgo-include`, and every target that
compiles Go (build, test, vet, golangci-lint, gosec, govulncheck) has the
header as a prerequisite (`CGO_TARGETS`), so it is regenerated when
`go.mod` changes. `.goreleaser.yaml` sets the same flag on every build.
Plain `go build` outside make does not get it.

---

## Local parity

```bash
make ci               # every job; per-job PASS / FAIL / SKIP summary
make ci-<job>         # one job, e.g. make ci-lint, make ci-test
CI_STRICT=1 make ci   # a skipped job fails the run
```

A green `make ci` means a green CI run, except for the gaps below.

**One source of truth.** The workflow does not duplicate the commands
with a checker comparing the copies. It calls the Makefile, so there is
one copy to keep. `scripts/check-ci-parity.sh` (`make ci-parity-check`,
run in the `toolchain` job) fails when:

- a `run:` step is anything but `make <target>`. Provisioning steps that
  only install a tool are exempt (pins read, gitleaks, golangci-lint,
  mingw-w64, module download), and they may not run a check;
- a step or job sets `env:`, `defaults:` or `working-directory:` (env goes
  in the target, where a local run sees it too);
- a CI step passes a make variable other than `LINT_BASE` (the PR base);
- `ci-<job>` is missing, has prerequisites, runs anything but
  `$(CI_STEP) <target>` lines, or differs from the job's steps in order or
  in `continue-on-error` (a `-` prefix);
- `CI_JOBS` is not `ci.yml`'s job list;
- a Makefile, Taskfile, `scripts/` or `.githooks/` line runs golangci-lint
  without `scripts/ci/tools.sh`, or names a gosec/govulncheck version other
  than the Makefile's.

To add a CI check: add a Makefile target, call it from a `ci.yml` step,
then add a `$(CI_STEP) <target>` line to `ci-<job>` in step order.

**Same tools.** `scripts/ci/tools.sh` runs each command with the
`mise.toml` pin (pnpm: `packageManager`), resolved through mise, or through
`PATH` on a runner without mise. A missing tool or another version is an
error that names the fix (`mise trust && mise install`), never a warning.
It also unsets `GOROOT` and sets `GOWORK=off`. The pre-commit hooks,
`make lint` and the Taskfile lint tasks use it too.

**What cannot match, and how the local run handles it:**

| Gap | Local behaviour |
|---|---|
| `test` runs on ubuntu-latest and macos-latest | runs on your OS only; the summary says which |
| `integration` gets a Postgres service container | `scripts/ci/prereq.sh` probes `POSTGRES_HOST:PORT` (defaults: the service's credentials on `localhost:5432`); unreachable means SKIP, with the `docker run` for the service's image |
| `windows` installs mingw-w64 | SKIP without `x86_64-w64-mingw32-gcc`, with the install command |
| headless Chrome on the runners | missing means a PARTIAL notice; the e2e browser checks skip themselves |
| Trivy fs scan, SARIF uploads, Codecov, `binaries` artifact | CI only (`scripts/check-ci-parity.sh --ci-only` lists them); none of them block |
| secret scan on a PR covers the merge commit's history | locally, `HEAD`'s history |
| local-path `replace` in `go.mod` | `make ci-drop-replaces` fails |
| secrets | none: CI uses none |

**Lint.** `ci-lint-full` is the blocking full scan, in CI and in `ci-lint`.
The tree is at zero findings at the pinned version. `lint-new` lints only
your branch's findings against the merge base with `origin/next`
(`LINT_BASE=<commit>` or `LINT_BASE_REF=<branch>` overrides it) for a faster
local loop; CI does not run it.

---

## Release

Pushing a `v*.*.*` tag runs `release.yml`:

1. **`ci`**: all of `ci.yml`, called as a reusable workflow on the tagged
   commit. A release gates on exactly what a merge does, including the
   macOS and e2e runs. No `ci.yml` job may carry a job-level `if:`: in a
   called run the `github` context is the tag push's, so a condition
   written for CI's own triggers could skip a gate on release.
2. **`trivy-release-gate`**: Trivy config scan of the `Dockerfile` and
   filesystem scan, failing on any CRITICAL finding (unfixed CVEs
   ignored). Stricter than CI's Trivy step, which only reports.
3. **`goreleaser`**: GoReleaser at the `mise.toml` pin with `.goreleaser.yaml`:
   CGO builds of `ctxt` and `dpkms` with `-tags fts5` for darwin
   amd64/arm64, linux amd64 and windows amd64 (mingw-w64 cross-compiler),
   checksums, GitHub release and the `ctxt` Homebrew tap formula. Signs
   the checksum file with GPG when the key secret is set; without it the
   job passes `--skip=sign` (OSS GoReleaser has no per-sign condition, so
   `.goreleaser.yaml` cannot express it). A local
   `goreleaser release --snapshot` needs `--skip=sign` too.

`goreleaser` needs both gates. `scripts/check-toolchain-pins.sh` fails if
`release.yml` stops running `ci.yml` or if any of its jobs can start before
the `ci` job passes.

```bash
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

Container images are not built by a release: `docker.yml` pushes to GHCR
from `main`.

---

## Secrets

| Secret | Used by | Required |
|---|---|---|
| `GITHUB_TOKEN` | GoReleaser (release + assets) | provided by Actions |
| `HOMEBREW_TAP_GITHUB_TOKEN` | GoReleaser Homebrew tap push | for the tap |
| `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE` | release signing | optional; skipped when unset |

CI needs no secrets: the integration Postgres credentials are fixed test
values in the workflow, and gitleaks runs as the MIT-licensed CLI rather
than `gitleaks-action` (which needs a licence secret for organisation
repositories).

---

## Debugging a failed run

```bash
gh run list --workflow=ci.yml
gh run view <run-id> --log-failed
```

- **Toolchain check**: the message names the file and the pin it disagrees
  with. Update `mise.toml` first, then the straggler.
- **Any job**: `make ci-<job>` runs the same commands locally.
- **Lint.** The full scan blocks, and the message names the file and line.
  Reproduce with `make ci-lint-full`, or with `make lint-new` to see only
  your branch's findings.
- **Parity check**: the message names the step or target that drifted; see
  [Local parity](#local-parity).
- **Integration**: `make ci-integration` against a local pgvector Postgres
  (the skip message prints the `docker run`).
- **Bundle drift** (`viewer`, `webui`): rebuild with `make build-ui` or
  `make build-searchgraph-viewer` and commit the `dist/` output.

---

## Related

- [DEVELOPING.md](../DEVELOPING.md): local setup, test tiers, hooks
- [development.md](development.md): development workflow and release process
- [release-notes/](release-notes/): operator impact per release
