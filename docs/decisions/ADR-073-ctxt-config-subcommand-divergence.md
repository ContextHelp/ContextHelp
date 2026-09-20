# ADR-073 – ctxt `config` subcommand divergence from kit §7.4

> **Status:** Accepted
> **Date:** 2026-05-13
> **Author:** jadb
> **Supersedes:** none
> **Superseded by:** none

---

## Context

Kit conventions [§7.4 / `console-cli-config`][kit-ref] mandate a minimum
`<tool> config path` + `<tool> config paths` surface on every kit-built CLI.
The minimum is deliberately narrow: cross-tool introspection only. It is the
contract downstream consumers (agents, MCP bridges, doc generators) rely on to
locate a tool's config file the same way `git config --list --show-origin` or
`gh config get` do — and nothing more.

ctxt is a meta-tool that ingests, curates, profiles, and composes context for a
per-user operator. Per-user config ergonomics (read the loaded values, edit in
`$EDITOR`, scan for plaintext secrets, lint and auto-fix, snapshot/restore the
whole config tree) are central to that operator UX. Kit, in contrast, has to
keep §7.4 narrow because broadening it would explode adoption costs across
every kit-built CLI (dpkms, aps, wsm, …) — most of which never need anything
richer than `path`/`paths`.

The conformance work in T-0591..T-0595 (ADR-072) audited every `ctxt config`
leaf against the 12fcc strict gate and confirmed each non-§7.4 verb carries the
required `kit/side-effect` + `kit/idempotent` (+ guidance) annotations. We
need a single source of truth recording this intentional divergence so future
audits do not flag the additional surface as a regression and future
contributors do not flatten ctxt back to the §7.4 minimum.

`ctxt config` exposes **eight** subcommands; the kit minimum is **two**.
Sibling `dpkms` hews to the kit minimum (17-line adapter, no extras).

| Subcommand            | Kit §7.4 minimum | ctxt | dpkms |
|-----------------------|------------------|------|-------|
| `path`                | required         | yes  | yes   |
| `paths`               | required         | yes  | yes   |
| `show`                | —                | yes  | —     |
| `validate`            | —                | yes  | —     |
| `edit`                | —                | yes  | —     |
| `doctor` (alias `lint`) | —              | yes  | —     |
| `backup`              | —                | yes  | —     |
| `restore`             | —                | yes  | —     |

---

## Decision

ctxt keeps the eight-verb `config` surface. The split is enforced and
documented:

1. `path` + `paths` are registered through `kitconfigcli.RegisterPathSubcommands`
   on the shared `configCmd` parent. They are NOT re-implemented locally.
2. `show`, `validate`, `edit`, `doctor` (alias `lint`), `backup`, `restore`
   remain hand-rolled, mounted onto the same `configCmd` parent.
3. Every hand-rolled leaf carries the 12fcc-strict annotations
   (`kit/side-effect`, `kit/idempotent`, `Short`, `Long`, and — where the leaf
   is `write`/`destructive` or non-read — `kit/examples`, `kit/next-steps`,
   `kit/destructive-token`). This is enforced at boot by the strict gate
   (ADR-072) and in CI by `TestRootValidate_StrictGatesPass`
   (`cmd/ctxt/cmd/strict_validation_test.go`).
4. `restore` (the only destructive leaf in this set) consumes kit's global
   `--confirm=yes` / `--confirm=prompt --confirm-token=<sha>` UX rather than a
   local `-y/--yes` flag, per ADR-072.

The principle for future config tooling:

> If a config concern is **ctxt-specific** (per-user pipelines, profiles,
> secrets layering, disaster recovery, doctor lint) it belongs in
> `ctxt config <verb>`. If it is **cross-tool** (just locate the config file
> the tool is using) it belongs in `kit/console/cli/config`'s `PathCommand` /
> `PathsCommand`, and ctxt defers to them — never re-implement `path`/`paths`
> locally.

---

## Rationale

### Per-verb justification for the extras

- **`show`** — read-side counterpart to `edit`. §7.4 tells operators *where*
  config lives; `show` tells them *what is currently loaded*, grouped under
  storage / server / profile / i18n / registries. `--format=json` makes it
  scripting-friendly. Natural endpoint for "is this setting actually active?".
- **`validate`** — `config.Load` already errors on a malformed file at boot.
  `validate` makes the same check *invocable on demand* (CI pre-flight,
  post-`edit` sanity check) and adds a plaintext-secret scan over string
  fields. Also surfaces the migration rewrite `Load` does silently, so the
  operator sees the proposed change before the next run writes it.
- **`edit`** — `$EDITOR` convenience wrapper. Resolves the active config path
  (same precedence as every other ctxt command) and shells out to `$EDITOR`
  (`vi` fallback). Help text nudges toward `validate` / `doctor` afterward.
  Without it, the recipe is `ctxt config path | xargs $EDITOR` — fine for
  power users, footgun for everyone else.
- **`doctor`** (alias `lint`) — proactive hygiene with `--fix`. Where
  `validate` answers "does this parse and contain secrets?", `doctor` answers
  "is the *whole environment* healthy?". Checks: schema, plaintext secrets,
  file permissions (world/group-readable), deprecated keys. `--fix` applies
  safe remediations (currently `chmod 600`). Severity ERROR / WARN; exit 1 on
  any finding (CI-safe).
- **`backup`** — disaster-recovery primitive. Produces a `.zip` of config
  files + sibling `.zip.sig` Ed25519 signature, with the public key embedded
  in the bundle manifest for self-contained verification. Pairs strictly with
  `restore`.
- **`restore`** — destructive half of the backup/restore pair. Verifies the
  Ed25519 signature before any file write when `--verify` is set, gates
  execution behind kit's global `--confirm` per ADR-072. Dry-run path
  previews which files would be overwritten.

### Why ctxt and not kit

Adoption cost. dpkms's 17-line adapter is the canonical example of how cheap
§7.4 should be for any new kit-built CLI. Folding `show` / `validate` / `edit`
/ `doctor` / `backup` / `restore` into kit would require:

- A `core/config`-level value tree to feed `show`'s grouped output.
- A schema-and-secret validation pipeline (kit currently has no schema layer).
- An editor-shell helper (cross-platform, `$EDITOR` precedence, atomic write).
- A doctor framework with severity, `--fix` plan/apply, and CI exit codes.
- A signed-bundle backup format that has to round-trip across kit-tool
  versions.

None of those have demand from the other adopters today. Putting them in kit
would either bloat every minimal adopter or force kit to ship a flagged
"big-config" build that nobody else uses. ctxt-local is the right home.

### Alternatives rejected

- **Push the extras into kit.** Rejected on adoption-cost grounds (above).
- **Drop the extras from ctxt.** Rejected: each verb has documented operator
  demand, and dropping them would degrade the per-user UX that the "brain"
  positioning requires.
- **Fork `path`/`paths` locally and abandon §7.4 conformance.** Rejected
  outright: ctxt explicitly registers via `kitconfigcli.RegisterPathSubcommands`
  so the cross-tool introspection contract stays uniform.

---

## Consequences

### Positive

- Future audits (12fcc, MCP-surface inventory, doc generators) can treat the
  divergence as deliberate by referencing this ADR.
- Contributors adding a ctxt-specific config concern have an explicit home
  (`ctxt config <verb>` under the shared parent) and an explicit set of
  annotation requirements (enforced by `TestRootValidate_StrictGatesPass`).
- `path`/`paths` stay canonical: any improvement to kit's resolver, output
  format, or `--from` semantics lands automatically in ctxt without a local
  fork.

### Negative

- The eight-verb surface is heavier to maintain than dpkms's two-verb
  adapter. The strict-gate annotations and the kit/12fcc CI guard mitigate
  drift, but every new verb still costs review attention.
- Anyone copying ctxt's pattern wholesale onto a thinner tool inherits the
  cost without the demand. This ADR is the deterrent.

### Neutral or Considerations

- If kit later ships a canonical `config show` or `config doctor` (because
  enough adopters demand them), ctxt re-evaluates and consolidates onto the
  kit-canonical surface, dropping the local implementation. This ADR would
  then be amended (status `Superseded`) rather than left as a permanent
  divergence.

---

## Implementation Notes

Affected modules:

- `cmd/ctxt/cmd/config.go` — parent `configCmd` + `RegisterPathSubcommands`
  wiring for the §7.4 minimum.
- `cmd/ctxt/cmd/config_show.go`, `cmd/ctxt/cmd/config_validate.go`,
  `cmd/ctxt/cmd/config_edit.go`, `cmd/ctxt/cmd/config_doctor.go`,
  `cmd/ctxt/cmd/config_backup.go`, `cmd/ctxt/cmd/config_restore.go` —
  hand-rolled extras, each with strict-gate annotations.
- `cmd/ctxt/cmd/strict_validation_test.go` — enforces annotation parity
  across the full `config` subtree at boot and in CI.

Annotation invariants (per hand-rolled verb):

- `kit/side-effect` — one of the §7.4 enum, matching the stamps in
  `cmd/ctxt/cmd/config.go`:
  - `read` for `show`
  - `write-local` for `validate` (config.Load may rewrite migrations to
    disk), `edit`, `doctor` (`--fix` chmods the file), `backup`
  - `destructive-local` for `restore`
- `kit/idempotent` — `yes` for read leaves, `conditional` for `doctor`
  (`--fix` mutates), `no` for `restore`.
- `Short` + `Long` — required by Layer-A.
- `kit/examples` + (non-read) `kit/next-steps` — required by
  `EnforceGuidance=true`.
- `kit/destructive-token=required` — required on `restore` by
  `EnforceDestructiveToken=true`.

Forward note: if kit ever ships a canonical `config show` / `config doctor`,
ctxt re-evaluates and consolidates onto kit's surface, demoting this ADR to
`Superseded`.

---

## References

- [ADR-072 — Kit 12fcc strict CLI validation at boot](./ADR-072-kit-12fcc-strict-cli-validation.md)
- [`kit/console/cli/config` (§7.4 source)][kit-ref] — canonical reference for
  the `path` + `paths` minimum, the `WithResolver` injection point, and the
  dpkms / aps / wsm adopter patterns.
- `internal/cli/configpath` — shared resolver used by both ctxt and dpkms
  to expose the real `internal/config.LoadWithOverrides` cascade through
  `<bin> config path(s)`. See `console-cli-config.md` §"Per-bin shared
  namespace resolvers" for the adopter pattern this codifies.
- `cmd/ctxt/cmd/strict_validation_test.go` — regression guard ensuring the
  full eight-verb surface stays 12fcc-conformant.

[kit-ref]: kit conventions reference (internal notes)
