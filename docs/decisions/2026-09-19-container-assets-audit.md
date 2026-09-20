# Container Assets Audit

Date: 2026-09-19
Status: Analysis — recommendations only, no files deleted
Scope: `Dockerfile`, `docker-compose.yml`, `docker-compose.dev.yml`,
`docker/Dockerfile.dpkms`, `docker/docker-compose.yml`

The container assets accumulated over six months and were never revisited
together. This note establishes which are canonical, which are dead, and what
should happen to each. It is an audit, not a rewrite: the root `Dockerfile` is
in good shape on its own terms and is not touched.

Docker **was** available for this audit (Engine 29.4.0), so every build claim
below was executed rather than inferred.

---

## Summary

| Asset | Verdict |
|---|---|
| `Dockerfile` | **Canonical.** Builds clean. Keep. |
| `docker-compose.yml` | **Canonical** for dev and prod profiles. Keep. |
| `docker-compose.dev.yml` | **Keep.** Distinct purpose: side-services for the integration tier. |
| `docker/Dockerfile.dpkms` | **Dead — recommend deletion.** Provably unbuildable. |
| `docker/docker-compose.yml` | **Dead — recommend deletion.** Only consumer of the above. |
| `ui-builder` stage | **Recommend removal.** Vestigial; its output is discarded. |

Nothing was deleted in this pass. See [Why nothing was deleted](#why-nothing-was-deleted).

---

## Why are there three compose files?

Because two of them answer real, different questions and the third is a
leftover from before the root-level layout existed.

### `docker-compose.yml` — canonical, dev + prod

The root compose file is the one the Makefile drives (`make docker-dev`,
`make docker-prod`, `make docker-down`, `make docker-logs`, `make docker-ps`).
It carries both environments as profiles:

- `--profile dev` → `dpkms-dev` (air hot-reload) + `ui-dev` (Vite)
- `--profile prod` → `dpkms` (built from the root `Dockerfile`) + `caddy`

### `docker-compose.dev.yml` — canonical, side-services

Despite the name, this is **not** a dev environment. It provides the Postgres
and Redis side-services that the integration test tier needs, and nothing else.
Its header says so explicitly: the default dev flow is SQLite + local queue +
sqlite-vec, and these services are only for validating alternative backends.

Its Postgres pin (`pgvector/pgvector:pg16`) matters and must not drift: the
driver's first migration is `CREATE EXTENSION vector`, which a plain
`postgres:16` image cannot load.

This file is consumed by `Taskfile.yml` (six targets),
`scripts/dev-setup.sh`, and the docs. It is load-bearing.

**The two root files do not overlap** — one runs the application, the other
runs backing services for tests. The confusing part is naming, not structure:
`docker-compose.dev.yml` holds *services*, while the *dev environment* lives in
the `dev` profile of `docker-compose.yml`. A rename to something like
`docker-compose.services.yml` would remove the ambiguity, but it would churn
every referencing call site (six Taskfile targets, `scripts/dev-setup.sh`, the
parity test, and four docs pages) for a cosmetic gain, so it is noted and not
recommended now.

### `docker/docker-compose.yml` — legacy, dead

This is the original March 2026 layout, superseded in May when the root
`docker-compose.yml` landed (`3f2973d`). It has been untouched since
2026-03-13 — the oldest asset in the audit. It is dead for three independent
reasons:

1. All three of its build services point at `docker/Dockerfile.dpkms`, which
   does not build (see below).
2. Nothing references it. No Makefile target, no Taskfile target, no workflow,
   no script. The only mentions in the tree are the parity test's file list and
   the 2026-03-13 plan that predates the migration.
3. Its `dpkms-postgres` service sets `DPKMS_STORAGE_TYPE`, which **zero** Go
   files read (`CH_STORAGE_TYPE` is the name the code actually uses).

Its one service with no root-level equivalent is `docs` (an Astro dev server).
That capability is already served by `make docs-dev`, which runs the same
`pnpm dev` directly.

**Recommendation: delete `docker/docker-compose.yml`.**

---

## `docker/Dockerfile.dpkms` vs the root `Dockerfile`

**Genuinely superseded, not overlapping.** The root `Dockerfile` does
everything this one does and does it correctly.

`docker/Dockerfile.dpkms` does not build. Verified:

```
$ docker build -f docker/Dockerfile.dpkms -t ctxt-audit-dpkms:test .
...
 > [stage-1 6/7] COPY docker/config.yaml /app/config.yaml:
ERROR: failed to build: failed to solve: failed to compute cache key:
  "/docker/config.yaml": not found
```

It copies `docker/config.yaml`, which does not exist — the file was renamed to
`docker/config.docker.yaml` in April (`ad833af`) and this Dockerfile was never
updated. The root `Dockerfile` copies the correct name.

It also carries the CGo bug the root `Dockerfile` was fixed for in September
(`2ec0eb8`): it builds with `CGO_ENABLED=1` but **without** `-tags fts5`, so
even with the config file restored, the resulting binary would fail at the
first FTS5 migration. This exact defect is already documented in
`docs/analysis/2026-08-04-cgo-bifurcation.md`. The root `Dockerfile` passes
`-tags fts5` in all three places it needs to.

For contrast, the root `Dockerfile` builds clean:

```
$ docker build -f Dockerfile --target runtime -t ctxt-audit-root:test .
...
 => naming to docker.io/library/ctxt-audit-root:test    done
```

The CI Docker workflow (`.github/workflows/docker.yml`) builds only
`file: Dockerfile`, so the broken one has no CI coverage and its breakage went
unnoticed for four months.

**Recommendation: delete `docker/Dockerfile.dpkms`**, together with
`docker/docker-compose.yml` (its only consumer). Keep the rest of `docker/` —
`config.docker.yaml`, `air.toml`, and `caddy/Caddyfile` are all live
dependencies of the root `Dockerfile` and `docker-compose.yml`.

---

## The `ui-builder` stage

**Recommendation: remove it.** Its comment is stale and its output is dead.

The comment says "placeholder until web/ui is scaffolded". `web/ui` **has since
been scaffolded** — React 19, Vite 8, TypeScript, with a committed
`pnpm-lock.yaml`. So the premise no longer holds.

But the stage should still go, because the UI is not delivered through it. The
compiled UI is embedded into the binary at compile time:

```go
// internal/ui/embed.go
//go:embed all:dist
var FS embed.FS
```

`internal/ui/dist/` is **committed to the repo** (`index.html`, hashed JS/CSS
assets, icons), and `make build-ui` is what refreshes it. The Go build embeds
that directory; the Dockerfile's `COPY --from=ui-builder /ui/dist
./web/ui/dist` writes to a path nothing reads.

Confirmed against the image built above — the runtime stage has no `web`
directory at all:

```
$ docker run --rm --entrypoint sh ctxt-audit-root:test -c 'ls /app'
config.yaml  ctxt  dpkms
$ docker run --rm --entrypoint sh ctxt-audit-root:test -c 'ls /app/web'
ls: cannot access '/app/web': No such file or directory
```

So the stage pulls `node:22-alpine` on every cold build to produce a
`dist/.keep` file that is copied into the builder and then discarded. Removing
it (and its `COPY --from=ui-builder` line) drops a base image from the build
with no behavior change.

If the UI ever moves from commit-time embedding to build-time compilation, the
stage comes back — but it comes back doing real work (`pnpm install && pnpm
build`), not as a placeholder.

---

## Go version coupling

The root `Dockerfile` hardcodes `golang:1.26-bookworm`, and
`docker-compose.yml` hardcodes the same image for `dpkms-dev`. This is
duplicated with `go.mod`'s `go` directive and with the toolchain pinned in
`mise.toml`.

This coupling is **already guarded**. `test/parity/docker_test.go` parses
`go.mod` and fails if any registered Dockerfile or compose file pins a
different `golang:<major>.<minor>`:

```
$ go test -short -tags fts5 -count=1 ./test/parity/...
ok   github.com/ideacrafterslabs/ctxt/test/parity   0.393s
```

So a Go bump that misses a container file is caught by the unit tier, not
discovered in production. Two consequences worth recording:

- Whoever bumps the toolchain must bump `go.mod`, the Dockerfiles, and the
  compose files in the same change, or the parity test goes red. That is the
  intended behavior.
- `mise.toml` currently requests `go = "latest"` rather than a pin, so it is
  *not* covered by the parity test — the test only compares container files
  against `go.mod`. Pinning mise to the same version closes that gap. **That
  change is out of scope for this audit and is being handled separately; this
  note does not modify `mise.toml`.**

If `docker/Dockerfile.dpkms` and `docker/docker-compose.yml` are deleted, both
must also be removed from `dockerSources()` and `composeFiles()` in
`test/parity/docker_test.go`, or the test will fail trying to read them.

---

## Why nothing was deleted

The deletions recommended here are sound but not self-contained: each requires
a matching edit to `test/parity/docker_test.go`, which reads every registered
file and calls `t.Fatalf` when one is missing. Deleting the files alone turns
the unit tier red.

Since this audit was scoped to producing a decision note, the deletions are
left as a follow-up that can be made and verified in one change:

1. `git rm docker/Dockerfile.dpkms docker/docker-compose.yml`
2. Drop both entries from `dockerSources()` / `composeFiles()` in
   `test/parity/docker_test.go`
3. Remove the `ui-builder` stage and its `COPY --from=ui-builder` line from
   `Dockerfile`
4. Verify: `go test -tags fts5 ./test/parity/...` and
   `docker build -f Dockerfile --target runtime .`

`docs/plans/2026-03-13-P200-docker-environment.md` said to keep
`docker/Dockerfile.dpkms` while updating compose to the new one. That deferral
has outlived its usefulness: the file has been unbuildable since April and has
no consumers left.
