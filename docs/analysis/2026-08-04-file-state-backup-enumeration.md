# File-State Inventory & Backup Ownership — Enumeration Report

> **Date:** 2026-08-04
> **Applies to:** dPKMS, ctxt
> **Companion:** [2026-08-04-storage-architecture-pov.md](2026-08-04-storage-architecture-pov.md) (§ "File-state sprawl")

The storage POV flagged one liability without proving it: "what is the complete
state of this install?" has no single owner. This report proves it. Below is the
full persistent-state surface as the code writes it today, what the backup
subsystem actually captures, a must/should/must-not classification, and a spec
for making the backup subsystem own the enumeration.

## 1. Complete state inventory

Every path any component writes, with the code that resolves it and the
override that can move it. `cfgdir` = `$XDG_CONFIG_HOME/contexthelp`,
`datadir` = `$XDG_DATA_HOME/contexthelp` (both via `kit/go/core/xdg`,
`internal/config/config.go:1011-1023`).

| # | Family | Default path | Resolved at | Overrides |
|---|--------|--------------|-------------|-----------|
| 1 | SQLite DB (+ `-wal`/`-shm` sidecars) | `datadir/db.sqlite` | `internal/config/config.go:786` | `storage.path`; env `CTXT_DATA_DIR` (bound to `storage.path`, `config.go:930`); `CH_STORAGE_TYPE` |
| 2 | Blob store (local backend) | `datadir/blobs/` (sharded `aa/bb/key`, `internal/storage/blob/local/store.go:29-31`) | `internal/config/config.go:791` | `storage.blob.local.path`; `CTXT_BLOB_BACKEND`, `CTXT_BLOB_THRESHOLD` |
| 3 | Blob store (S3 backends) | remote bucket | `internal/config/config.go:792-795` | `storage.blob.s3.*`; `CTXT_BLOB_S3_*` (`config.go:933-938`) |
| 4 | Ambient event spool | `$XDG_STATE_HOME/ctxt/ambient/events/<source>/<yyyy>/<mm>/<dd>/<id>.json` | `internal/ambient/buffer/local/local.go:100-115` | `CTXT_AMBIENT_BUFFER_DIR`; `Config.RootDir`; S3 buffer backend variant |
| 5 | Config YAML — user layer | `cfgdir/<bin>.yaml` | `internal/config/config.go:1049-1060` | `CTXT_CONFIG`, `--config` |
| 6 | Config YAML — system layer | `/etc/contexthelp/<bin>.yaml` | `internal/config/config.go:725` | none |
| 7 | Config YAML — project layer | `<repo>/.contexthelp/<bin>.yaml` (walk-up) | `internal/config/config.go:1028-1047` | none |
| 8 | Policy | `cfgdir/policy/ctxt.yaml` (legacy `cfgdir/policies.yaml`, auto-migrated) | `internal/policy/policy.go:153-171` | `CTXT_POLICY_FILE` |
| 9 | Cursors | `cfgdir/cursors.yaml` | `internal/cursor/cursor.go:83-92` | `CTXT_CURSOR_FILE` |
| 10 | Signing keys — public + rotation log | `cfgdir/keys/<fp>.pub`, `cfgdir/keys/rotation_log.json` | `internal/bundle/bundle.go:88-110`, `internal/bundle/rotation.go:16,37` | none |
| 11 | Signing key — private | **OS keychain** (`ctxt-signing` / `ed25519-private-key`) | `internal/bundle/bundle.go:29-33` | none |
| 12 | Secrets (agefile backend) | none — operator-chosen paths | `internal/secrets/kit.go:107-115` | `secrets.age_file` / `secrets.age_identity_file`; `CTXT_AGE_FILE` / `CTXT_AGE_IDENTITY` (`config.go:949-950`) |
| 13 | External steps | `~/.config/contexthelp/steps/<name>/` (hardcoded `$HOME`, not XDG-aware) | `cmd/dpkms/cmd/serve.go:203-204`; downloads in `internal/steps/discovery.go:335-357` | `steps.path`, `--steps-path` |
| 14 | Pidfiles | `datadir/run/<port>.pid` | `internal/pidfile/pidfile.go:5,49`; `RunDir()` `internal/config/config.go:1089-1103` | `CTXT_DATA_DIR` (as a *directory* here) |
| 15 | Current-instance marker | `datadir/run/current-instance` | `internal/config/config.go:1105-1114` | per-call `CTXT_INSTANCE` / `--instance` bypasses it (`cmd/ctxt/cmd/helpers.go:100-114`) |
| 16 | Upgrade-state shadow | `datadir/run/upgrade-state.json` | `internal/upgrade/manager.go:11,228-260` | none |
| 17 | REPL history | `datadir/repl_history` | `internal/repl/history.go:19-24` | none |
| 18 | Clipboard-watcher state | `~/.local/share/contexthelp/watcher-state.json` (hardcoded — ignores `$XDG_DATA_HOME` *and* `CTXT_DATA_DIR`) | `internal/watcher/clipboard.go:208-217` | `cfg.StateFile` |
| 19 | Screen-watcher state | `~/.ctxt/screen-watcher-state.json` (rogue non-XDG dot-dir) | `internal/watcher/screen.go:104-105` | `SetStatePath` (tests only) |
| 20 | Service-manager units | `~/Library/LaunchAgents/com.contexthelp.dpkms.plist` / `~/.config/systemd/user/dpkms.service` | `cmd/dpkms/cmd/install_launchd.go:6-9,34-38` | none |
| 21 | Backup archives | `backup.dir`, else cwd | `internal/config/config.go:386-398`; `cmd/dpkms/cmd/backup.go:69-72` | `--output-dir`; `CTXT_BACKUP_PASSPHRASE` (encryption) |
| 22 | Watched inbox / processed dirs | operator-chosen; `<dir>/processed/<YYYY-MM>/` | `internal/ambient/filewatch/filewatch.go:139,360-378` | `filewatch` config |

Three naming inconsistencies surfaced by the sweep, worth fixing independently
of backup: the ambient spool uses app-name `ctxt` where everything else uses
`contexthelp` (#4); the clipboard watcher hardcodes `~/.local/share` instead of
resolving XDG (#18); the screen watcher writes to a pre-XDG `~/.ctxt/` dot-dir
(#19). A fourth: `CTXT_DATA_DIR` has **dual semantics** — the env-binding table
maps it onto `storage.path` (a *file* path, `config.go:930`) while
`EnsureDataDir`/`RunDir` treat it as a *directory* (`config.go:1075-1103`).
Setting it does two different things in the same process.

## 2. What backup captures today

Two disjoint subsystems, neither aware of the inventory above.

**`dpkms backup`** (`internal/service/backup.go`, `cmd/dpkms/cmd/backup.go`):

- Always: SQLite snapshot via `VACUUM INTO` (`internal/storageutil/snapshot.go:20` — correctly folds WAL, so sidecars are a non-issue).
- Opt-in `--include-blobs`: local backend only — the S3 branch is silently a no-op (`backup.go:146`).
- Opt-in `--include-configs`: the resolved `<bin>.yaml` plus a **directory walk of `filepath.Dir(configPath)`** (`backup.go:129-143`). This incidentally sweeps policy/, cursors.yaml, keys/, steps/ — *only because they happen to live in the same directory*.
- Manifest v2 records sizes, counts, embedding provenance (`backup.go:182-191`). It does not record which state families were captured or where they came from.

**`ctxt config backup`** (`cmd/ctxt/cmd/config.go:386-431`, `internal/bundle/`):
signed zip of **exactly one file** — the user-layer `<bin>.yaml`
(`Files: []string{filepath.Base(configPath)}`, `config.go:416-421`). No policy,
no cursors, no keys. Optional AES-256-GCM encryption.

**`dpkms restore`** (`internal/service/restore.go`): writes DB to `--db` target,
`config/*` into the config dir, and blobs into
`filepath.Join(filepath.Dir(opts.DBPath), "blobs")` (`restore.go:113,190`) —
**hardcoded sibling-of-DB, ignoring `storage.blob.local.path`**. A non-default
blob location restores to the wrong place and the daemon comes up blind to its
blobs.

## 3. Classification × coverage

| Family | Class | Captured today? | Gap |
|--------|-------|-----------------|-----|
| 1 SQLite DB | **must-backup** | yes (always) | — |
| 2 Blobs (local) | **must-backup** | opt-in only | default `dpkms backup` produces a corpus with dangling `blob://` refs |
| 3 Blobs (S3) | must-backup (or explicitly delegated to bucket provider) | **never** | silent; not even a warning |
| 4 Ambient spool | should-flush-then-skip | never | undelivered events are unrecoverable capture; backup should drain or copy them |
| 5 Config user layer | **must-backup** | yes | env-relocated file (`CTXT_CONFIG` outside cfgdir) is picked up for the single-file copy but its *directory siblings* are then walked instead of cfgdir's |
| 6 Config system layer | should-backup | never | acceptable if documented |
| 7 Config project layer | out of scope | never | lives in the user's repo; correctly excluded |
| 8 Policy | **must-backup** | only via `--include-configs` dir walk | `CTXT_POLICY_FILE` outside cfgdir → silently dropped |
| 9 Cursors | **must-backup** | only via dir walk | `CTXT_CURSOR_FILE` outside cfgdir → silently dropped |
| 10 Public keys + rotation log | **must-backup** | only via dir walk | rotation log is the trust chain for bundle verification; should be unconditional |
| 11 Private signing key | must-backup (encrypted) or documented-excluded | **never** — keychain only | restore on a new machine cannot sign; no export path exists |
| 12 Age secrets | must-backup (already encrypted at rest) | only if coincidentally in cfgdir | resolver never consulted |
| 13 Steps | recreatable (registry re-download) / should-backup (local custom steps) | only via dir walk | hardcoded-`$HOME` path may diverge from walked dir |
| 14 Pidfiles | **must-NOT-backup** | not captured | correct — but only because nothing walks datadir; a naive "walk datadir" fix would regress this |
| 15 Current-instance marker | must-NOT (machine-local preference, recreatable via `ctxt instance use`) | not captured | same fragility as #14 |
| 16 Upgrade-state shadow | **must-NOT** (in-flight machine state; restoring a stale `failed` banner is actively wrong) | not captured | same |
| 17 REPL history | optional | never | minor UX loss on restore |
| 18 Clipboard-watcher state | recreatable (dedupe hashes) | never | fine |
| 19 Screen-watcher state | recreatable | never | fine; fix the path anyway |
| 20 Service units | must-NOT (host-specific binary paths; recreate via `dpkms install`) | not captured | correct |
| 21 Backup archives | **must-NOT** (recursion) | n/a | enforce if `backup.dir` ever lands inside datadir |
| 22 Inbox dirs | out of scope | never | user-owned content, not install state |

The pattern across every gap is the same: **backup addresses directories, the
rest of the system addresses resolvers.** Each family has a `Path()`-style
resolution function honoring its env override; backup re-derives none of them
and instead walks one directory hoping the families live there. Every override
in column "Overrides" of §1 is therefore a silent backup-correctness hole, and
every correctly-excluded family is excluded by accident of layout rather than
by policy.

## 4. Ownership spec: a state-path registry

The backup subsystem should own the enumeration explicitly, the same way the
index-signature table owns embedding provenance. Concretely:

**1. Central registry.** A new `internal/statepaths` package (or a section of
`internal/config`) exporting:

```go
type Class int // MustBackup, ShouldBackup, Recreatable, MachineLocal, External

type Family struct {
    Name     string                    // "cursors", "policy", "run"
    Class    Class
    Resolve  func() ([]string, error)  // delegates to the owning package's resolver
    Sensitive bool                     // secrets/keys: require encrypted archive
    Flush    func(ctx) error           // optional: drain-before-copy (ambient spool)
}

func Registry() []Family
```

Each owning package registers its family using its **existing** resolver
(`cursor.Path`, `policy.PoliciesPath`, `config.RunDir`, `repl.HistoryPath`,
`local.ResolveDefaultDir`, `bundle.PublicKeyDir`…), so env overrides are
honored by construction and there is exactly one definition of each path in
the codebase.

**2. Backup iterates the registry, not a directory.** `service.Backup` walks
`Registry()`: `MustBackup` always, `ShouldBackup` behind flags, `Sensitive`
only when the archive is encrypted (refuse or warn otherwise), `MachineLocal`
never — by declared class, not by directory accident. The S3-blob and
keychain-key exclusions become explicit manifest entries
(`"skipped": [{"family": "blobs", "reason": "s3 backend"}]`) instead of silent
no-ops.

**3. Manifest v3 records the enumeration.** Per-family: name, class, resolved
source paths, file count, bytes, skipped-with-reason. Restore then addresses
files by family and routes each through the *same* resolver on the target
machine — fixing the hardcoded `Dir(DBPath)/blobs` bug structurally, and
making cross-machine restores land relocated families (custom cursor file,
age secrets) in the right place.

**4. `dpkms doctor` prints the table.** "Complete state of this install" becomes
a queryable artifact: family, resolved path, exists, size, backed-up-by-default.
This is the single-owner answer the storage POV asked for, and it doubles as
the operator-facing documentation of §1.

**5. A conformance test closes the loop.** A test enumerates writer call-sites
(or, stronger: a lint rule requiring persistent writes to go through a
registry-blessed helper) and fails when a package writes a path no family
claims. That is what prevents family #23 from repeating this cycle — the
screen-watcher's `~/.ctxt/` shows how easily a rogue path ships when nothing
audits the surface.

Sequencing: the registry and manifest v3 are the substance; the doctor view and
conformance test are cheap once the registry exists. Fixing the three
non-XDG paths (#4, #13, #18, #19) and the `CTXT_DATA_DIR` dual-semantics wart
before freezing the registry avoids enshrining today's accidents as contract.
