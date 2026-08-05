# CGO Bifurcation — Driver/Build-Tag Audit and Canonical-Build Proposal

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md) (risk item "CGO bifurcation")

## Summary

The feared bifurcation — production behavior switching between `mattn/go-sqlite3`
(CGO) and `modernc.org/sqlite` (pure Go) — turns out to be a ghost: **no
production code path ever uses modernc**. The real bifurcation is narrower and
nastier: a single CGO driver whose capabilities depend on whether `-tags fts5`
was present at compile time, with **no compile-time enforcement and no runtime
capability check**. Today the Makefile and CI test/build paths pass the tag,
while **the GoReleaser release pipeline and the Dockerfile do not** — meaning
the artifacts we actually ship are the degraded build, and they fail at first
migration with `no such module: fts5`.

## 1. Which driver does each build path use?

### Production code: mattn only

- `internal/storage/sqlite/driver.go:8-9` imports
  `github.com/asg017/sqlite-vec-go-bindings/cgo` and blank-imports
  `github.com/mattn/go-sqlite3`; `driver.go:55` opens with driver name
  `"sqlite3"` (mattn's registration).
- `internal/storageutil/snapshot.go:14` — backup/snapshot path also opens
  `"sqlite3"`.
- `modernc.org/sqlite` appears in exactly **one** file:
  `test/integration/us0034_export_backup_test.go:16` (blank import) and `:84`
  (`sql.Open("sqlite", path)`) — a test fixture helper creating a throwaway DB.
  Nothing under `cmd/`, `internal/`, or `pkg/` imports it.
- `docs/dependencies.md:27` ("Selected: `modernc.org/sqlite` (CGo-free)") is
  **stale** — it documents the pre-switch decision that
  `docs/plans/P-010-sqlite-driver-vector-storage-design.md` and
  `docs/plans/P-020-sqlite-driver-vector-storage.md` explicitly reversed
  (modernc → mattn) to gain FTS5 + sqlite-vec.

So `modernc.org/sqlite` in `go.mod:44` is a test-only leftover, not a fallback
driver. There is no pure-Go build of ctxt/dpkms today; there is only "CGO build
with FTS5" and "CGO build silently missing FTS5" (plus one accidental
"CGO-disabled binary that cannot open storage at all" — see below).

### Build-path matrix

| Path | CGO | `-tags fts5` | Result |
|---|---|---|---|
| `make build` / `make install` — `Makefile:10-11` (`CGO_ENABLED := 1`, `BUILD_TAGS := -tags fts5`), used at `:42,:49,:55-56` | on | yes | full |
| `make test` / `test-unit` / `test-integration` — `Makefile:68,:73,:78` | on | yes | full |
| `make test-cover` — `Makefile:91` (`go test -race -coverprofile=coverage.out ./...`) | on | **no** | `no such module: fts5` in any test that runs migrations (e.g. `internal/jobs/race_test.go:27` → `sqlite.New`) |
| `make test-gate` — `Makefile:97` | on | **no** | same failure class; this is the known jobs-package symptom |
| CI unit tests — `.github/workflows/ci.yml:170-171` (`CGO_ENABLED: '1'`, `-tags fts5`) | on | yes | full |
| CI integration — `ci.yml:228-230` (`-tags=integration,fts5`) | on | yes | full |
| CI build smoke — `ci.yml:261-264` | on | yes | full |
| Lateral eval — `.github/workflows/lateral-eval.yml:56,:63` | on | yes | full |
| **GoReleaser** — `.goreleaser.yaml:18-19,:52-53` sets `CGO_ENABLED=1` for both builds; **no `flags:`/`tags` entry anywhere in the file**; `.github/workflows/release.yml:272-275` runs `goreleaser release --clean` with no `GOFLAGS` in env | on | **no** | **shipped binaries (GitHub releases + Homebrew tap, `.goreleaser.yaml:160-174`) fail at migration time** |
| Legacy `.goreleaser.yml` (shadowed by `.goreleaser.yaml`, which GoReleaser prefers) | on | **no** | dead config, drift hazard — same omission |
| **Dockerfile dpkms** — `Dockerfile:31` (`CGO_ENABLED=1 go build`, no tags) | on | **no** | container dpkms fails at first FTS migration |
| **Dockerfile ctxt** — `Dockerfile:37` (`CGO_ENABLED=0 go build`) | **off** | no | mattn compiles but `sql.Open("sqlite3", …)` errors at runtime ("binary was compiled with CGO_ENABLED=0"); the container ctxt can only work through the dpkms HTTP path, never direct storage — an undocumented capability cliff |

### Why nothing catches this

- `internal/storage/sqlite/sqlite3_fts5.go` is labeled a "compile-time guard"
  but contains only a doc comment and the package clause; its own text
  (`sqlite3_fts5.go:5-7`) admits: *"if the fts5 tag is missing the package
  still compiles but migrations will fail at runtime."* It guards nothing.
- Exactly one test file is tag-gated — `internal/storage/sqlite/graph_rw_test.go:1`
  (`//go:build fts5`) — so an untagged `go test` **silently skips** it rather
  than failing, hiding the degradation instead of surfacing it.
- No code anywhere probes `PRAGMA compile_options`, `fts5_version()`, or
  `vec_version()`; grep finds no runtime capability detection in `internal/`
  or `cmd/`.
- The failure point is deep: FTS5 is first required when migrations execute
  `CREATE VIRTUAL TABLE objects_fts USING fts5(…)`
  (`internal/storage/sqlite/migrations.go:412`), long after startup and config
  validation have "succeeded".

## 2. What degrades on a non-canonical build?

**FTS5** — tag-dependent. Without `-tags fts5`, mattn's embedded amalgamation
omits `SQLITE_ENABLE_FTS5`; every FTS DDL/`MATCH` fails. That takes out
keyword search (`internal/service/service.go:826` `FindByText`), the
FTS half of blended retrieval, and — because migrations abort — **the entire
database open path**. Degradation is total, not partial: an untagged binary
cannot even initialize a fresh store.

**sqlite-vec** — CGO-dependent, not tag-dependent. The repo uses only the
`cgo` flavor of the bindings (`driver.go:8`, `vectors.go:7`), statically
linking the C extension and registering it via `sqlite_vec.Auto()`
(`driver.go:50`) before the vec0 table is created
(`migrations.go:142,:449-456`). Any `CGO_ENABLED=1` build gets vec0 regardless
of the fts5 tag; a `CGO_ENABLED=0` build gets nothing, because the pure-Go
escape hatch the bindings offer (the `ncruces`/wasm flavor) is not imported and
would require the ncruces driver, not mattn or modernc.

**modernc as hypothetical fallback** — worse than commonly assumed. modernc
ships FTS5 enabled by default, but: different driver name (`"sqlite"` vs
`"sqlite3"`), different DSN pragma syntax (the `_journal_mode=WAL&…` DSN at
`driver.go:53-55` is mattn-specific), no sqlite-vec (cgo bindings can't load
into it), and different locking/concurrency behavior under the transactional
job queue. A modernc fallback is a second driver implementation, not a build
flag — which is precisely why P-010/P-020 removed it from the design.

## 3. Decision proposal (ADR-shaped)

### Status

Proposed.

### Context

One CGO driver, one capability tag, five build entry points, three of which
(GoReleaser, both Dockerfile builds) silently produce artifacts that cannot
open a database — and no guard at compile time, startup, or config validation.
The known jobs-package "no such module: fts5" failure is the benign symptom;
the malignant one is that release artifacts are the degraded build.

### Decision

1. **One canonical build: `CGO_ENABLED=1` + `-tags fts5`, everywhere.**
   FTS5 and sqlite-vec are core substrate capabilities (the single-DB bet in
   the storage POV depends on both), not optional extras. Concretely:
   - Add `flags: ["-tags", "fts5"]` to both builds in `.goreleaser.yaml`
     (or set `GOFLAGS=-tags=fts5` in the release job env).
   - Delete the shadowed `.goreleaser.yml`.
   - Add `-tags fts5` to both Dockerfile builds; build the container `ctxt`
     with `CGO_ENABLED=1`, or explicitly document it as HTTP-only and make
     direct-storage code paths unreachable in that image.
   - Fix `Makefile` `test-cover`/`test-gate` (`:91,:97`) to use
     `$(BUILD_TAGS)` like every sibling target.
2. **Turn the fake guard into a real one.** Replace the comment-only
   `sqlite3_fts5.go` with a tag-enforcing pair:
   `//go:build fts5` file defining `const fts5Enabled = true`, and a
   `//go:build !fts5` file whose body is a deliberate compile error (or a
   `func init() { panic(...) }` if a buildable degraded binary is ever
   wanted). Cheapest possible fix; makes every untagged build fail loudly at
   compile time instead of at migration time.
3. **Runtime capability probe at driver open, belt-and-braces.** In
   `sqlite.New`, after opening the DB and before migrations, execute
   `SELECT fts5_version()` (or check `pragma_compile_options` for
   `ENABLE_FTS5`) and `SELECT vec_version()`. On failure, return a
   `storage.ErrCapabilityMissing`-style error naming the capability and the
   remedy ("rebuild with -tags fts5"), surfaced by config
   validation / `doctor` rather than mid-migration SQL errors. This catches
   the cases a compile guard cannot: dynamically mislinked system SQLite,
   future driver swaps, and the Docker `CGO_ENABLED=0` ctxt binary.
4. **Drop `modernc.org/sqlite` from `go.mod`.** Rewrite the one test fixture
   (`test/integration/us0034_export_backup_test.go:84`) to open `"sqlite3"`
   like the rest of the tree; correct `docs/dependencies.md:27` to reflect
   the P-010/P-020 driver decision. If a pure-Go fallback is ever genuinely
   needed (e.g. a platform with no C toolchain), it must arrive as a designed
   second driver behind the existing `internal/storage` interfaces with an
   explicit degraded-capability declaration — never as a silent build variant.
5. **Degraded-capability strategy for any future fallback:** capabilities are
   declared, not discovered by crashing. The storage driver exposes a
   `Capabilities()` set (fts, vector, …); startup logs it, config validation
   rejects configurations that require an absent capability (e.g. search
   enabled without fts), and the API/CLI report it (`ctxt doctor`,
   `dpkms serve` banner). Silent feature loss is treated as a bug class, same
   severity as data loss.

### Consequences

- Release and container artifacts regain the capabilities CI actually tests;
  the artifact/CI skew (tested build ≠ shipped build) disappears.
- Untagged builds fail in seconds at compile time; the jobs-package failure
  class is eliminated rather than memorized.
- One dependency and one dead config file leave the tree; the docs stop
  contradicting the code.
- Cost: mandatory CGO keeps cross-compilation friction (mingw-w64 for
  Windows, per `.github/workflows/release.yml:260-261`) — already paid today,
  now paid knowingly.

### Alternatives considered

- **Ship a pure-Go (modernc or ncruces/wasm) fallback binary now** — rejected:
  doubles the driver surface for zero current demand; sqlite-vec parity
  requires the ncruces driver stack, a migration this codebase already
  evaluated and declined in P-010/P-020.
- **Runtime probe only, no compile guard** — rejected: fails later than
  necessary and only on the storage path; the two-line build-tag pair is
  strictly cheaper.
- **Vendor a custom amalgamation with FTS5 always on** — rejected:
  maintenance burden of tracking SQLite releases outweighs adding one flag to
  three build files.
