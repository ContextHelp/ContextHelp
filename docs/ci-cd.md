# CI/CD Pipeline

GitHub Actions workflows for `ctxt` and `dpkms`, under `.github/workflows/`.
This page describes what each one runs; the workflow files are the source of
truth.

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

`mise.toml` pins Go, golangci-lint, Node and gitleaks. No workflow names
those versions: each workflow's `toolchain` job reads them from `mise.toml`
and the other jobs consume its outputs. Bump a version in `mise.toml` only.

`scripts/check-toolchain-pins.sh` (`make toolchain-check`, and the first
step of CI) fails when anything disagrees:

- Dockerfile, compose and workflow images run the pinned Go and Node, and
  every image names an exact release, never a floating tag;
- `go.mod`'s `go` directive is satisfied by the pin;
- no workflow hardcodes a Go, Node or golangci-lint version;
- no workflow uses `@master`, `@main`, `@latest`, a `~>` range or
  `version: latest`;
- the Makefile, Taskfile and `scripts/` install no Go tool at `@latest`
  or a branch, and any golangci-lint they install is the v2 module at the
  pin;
- `release.yml` runs `ci.yml` ahead of every other job (see
  [Release](#release)).

Tools without a `mise.toml` entry are pinned in place: gosec `v2.29.0`,
govulncheck `v1.8.0`, nancy `v1.2.0`, Trivy action `v0.36.0`, GoReleaser
`v2.18.2`.

### Local-path replaces

Every Go job in `ci.yml` first runs
`go mod edit -dropreplace=hop.top/kit -dropreplace=hop.top/c12n`, so a
local-path `replace` committed for development resolves through the module
proxy in CI. It is a no-op when `go.mod` carries no such directive.

---

## CI (`ci.yml`)

All jobs except `toolchain` start once `toolchain` has resolved the pins.
`build` also waits for `lint`, `security`, `secret-scan` and `test`.

| Job | Steps | Local equivalent |
|---|---|---|
| `toolchain` | read pins from `mise.toml`; `scripts/check-toolchain-pins.sh` | `make toolchain-check` |
| `secret-scan` | gitleaks release tarball (checksum-verified) over full history, minus `.gitleaksignore` | `make security-scan` |
| `lint` | golangci-lint on findings new since the PR base (`HEAD~1` on a push); full scan, advisory; `gofmt -l .`; `go vet -tags fts5 ./...` | `golangci-lint run --new-from-rev=<base>`, `gofmt -l .`, `go vet -tags fts5 ./...` |
| `security` | gosec (medium+ severity and confidence, `-tags fts5`, SARIF upload); govulncheck `-tags fts5`; Trivy filesystem scan (SARIF upload, does not fail the job) | `make gosec`, `make vuln-scan` |
| `test` | Ubuntu **and** macOS: `go test -v -short -race -tags fts5 ./...` with coverage (uploaded to Codecov from Ubuntu), then the e2e suites: `go test -v -count=1 -tags 'fts5 e2e' ./test/e2e/...` | `make test-unit`, `make test-e2e` |
| `viewer` | search-graph viewer JS tests; embedded viewer bundle is current | `make test-searchgraph-viewer-js`, `make check-searchgraph-viewer-dist` |
| `webui` | embedded web UI bundle is current | `make check-ui-dist` |
| `integration` | Postgres service (`pgvector/pgvector:0.8.6-pg16-bookworm`); integration-tagged suites in `test/integration/`, `internal/storage/postgres/`, `internal/storageutil/`, `internal/embeddings/registry/`; then by name: job-queue claim and re-projection, HTTP handlers, embeddings journey and search-graph e2e, each on Postgres | `make test-integration-services` |
| `build` | `go build -tags fts5` of `dpkms` and `ctxt`; uploads `bin/` as the `binaries` artifact (7 days) | `make build` |

Every Go build and test uses `-tags fts5` with `CGO_ENABLED=1`: the storage
layer needs SQLite's FTS5 module, and gosec in particular analyses nothing
in 34 packages without the tag.

### Integration tests locally

`make test-integration-services` runs the same commands as the
`integration` job, in the same order. It needs a pgvector Postgres and the
`POSTGRES_*` env block; the dev container supplies both, or see
[DEVELOPING.md](../DEVELOPING.md#integration-tests--the-docker-tier).
Keep that target and the job in step when you add a suite.

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
3. **`goreleaser`**: GoReleaser `v2.18.2` with `.goreleaser.yaml`:
   CGO builds of `ctxt` and `dpkms` with `-tags fts5` for darwin
   amd64/arm64, linux amd64 and windows amd64 (mingw-w64 cross-compiler),
   checksums, GitHub release and the `ctxt` Homebrew tap formula. Signs
   the checksum file with GPG when the key secret is set.

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
- **Lint**: only findings your branch introduces fail the job. Run the
  pinned golangci-lint (`mise install`) with `--new-from-rev=<base>`; the
  full-scan step is advisory and reports the standing backlog.
- **Integration**: run `make test-integration-services` against the dev
  container or a local pgvector Postgres.
- **Bundle drift** (`viewer`, `webui`): rebuild with `make build-ui` or
  `make build-searchgraph-viewer` and commit the `dist/` output.

---

## Related

- [DEVELOPING.md](../DEVELOPING.md): local setup, test tiers, hooks
- [development.md](development.md): development workflow and release process
- [release-notes/](release-notes/): operator impact per release
