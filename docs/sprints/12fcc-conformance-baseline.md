# 12fcc Strict Validation Baseline

Track: `ctxt-kit-12fcc-conformance` · Tasks: T-0591 (dogfood), T-0592 (baseline).
Pinned upstream: `hop.top/kit` via local `replace` → `kit/hops/12fcc-leak`.

## Method

A build-tagged probe (`cmd/ctxt/cmd/baseline_probe_test.go`, tag `ctxtbaselineprobe`) constructs the ctxt root the same way `Execute()` does, then calls `Root.Validate()` and `Root.ValidateSignature()` directly with kit's shipped strict defaults (no `DisableValidate`, no env opt-outs). A second pass flips the optional strict gates (`EnforceGuidance`, `EnforceDryRunRationale`, `EnforceDestructiveToken`, `SignatureStrictness=reject`, `PassthroughStrictness=reject`) to preview T-0595's post-gate picture.

Reproduce:

```
go test -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd
```

## Key finding: boot validator is not running

`ctxt`'s `Execute()` in `cmd/ctxt/cmd/root.go` calls `fang.Execute` directly. It bypasses kit's `Root.Execute()` which contains the pre-flight `r.Validate()` call (see `kit/go/console/cli/cli.go:732-738`). Boot-time strict validation is currently a no-op for end users — `ctxt --help`, `ctxt status`, `ctxt <verb> --help` all exit `0` with no stderr output even though `EnforceValidate=true` is set on the Root config.

Implication for T-0593/T-0595: simply turning the strict gate "on" requires either replacing `cmd.Execute()` with `root.Execute(ctx)` or invoking `root.Validate()` from the existing entry point. Both surface the 160-command failure set below.

## Bucket totals (default kit 12fcc-leak strictness)

| Bucket | Count | Source |
|--------|-------|--------|
| missing-side-effect | 149 | `ValidationError.Missing` |
| missing-idempotency | 98 | `ValidationError.MissingIdempotency` |
| missing-Long | 79 | `ValidationError.MissingLong` |
| missing-status | 0 | `ValidationError.MissingStatusSubcommand` |
| shape-violation | 43 | `UnannotatedTopLevelLeaf` (28) + `UnannotatedDepthExceedance` (14) + `TooManyTopLevelVerbs` (1) + signature `depth-hierarchical` (11, overlap) |
| missing-passthrough | 0 | `ValidationError.PassthroughRejected` (gated by `PassthroughStrictness=reject`) |
| local-global-collision | 31 | `SignatureReport` `local-globals` check |

**Total distinct command paths affected: 160 (pre-foundation).**

### Post-foundation snapshot (T-0593)

After `cmd/ctxt/cmd/root.go::applyShapeAnnotations` runs and
`MaxTopLevelVerbs=30` lands, the shape buckets close:

| Bucket | Pre-foundation | Post-foundation |
|--------|----------------|-----------------|
| `UnannotatedTopLevelLeaf` | 28 | 0 |
| `UnannotatedDepthExceedance` | 14 | 0 |
| `TooManyTopLevelVerbs` | 1 | 0 |
| signature `depth-hierarchical` | 11 | 0 |
| signature `local-globals` | 31 | 31 (subtree work) |
| `Missing` (side-effect) | 149 | 149 (subtree work) |
| `MissingIdempotency` | 98 | 98 (subtree work) |
| `MissingLong` | 79 | 79 (subtree work) |

Reproduce post-foundation:
`go test -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd`.

## Top-level shape problems

* `MaxTopLevelVerbs=10` but ctxt registers **28** top-level runnable leaves. Either group more verbs under nouns, mark some as reserved (matching `IsReserved`), or raise `Config.MaxTopLevelVerbs`.
* 28 of those depth-1 leaves are missing the `kit/top-level-verb=true` annotation.
* 14 depth-3 commands (chiefly under `ctxt profile schema …`, `ctxt lateral eval …`, `ctxt import batch …`, `ctxt lateral config …`) need `kit/hierarchical` on every intermediate node.

## Strict-gate preview (T-0595 target)

Re-running `Validate()` with `EnforceGuidance + EnforceDryRunRationale + EnforceDestructiveToken + SignatureStrictness=reject + PassthroughStrictness=reject` adds:

* `MissingExamples`: **149** (every leaf will need `kit/examples`)
* `MissingNextSteps`: **149** (every non-read leaf will need `kit/next-steps`)
* All current sig-report violations promote to hard errors.

## Post-Sprint State (T-0595 close)

The sprint shipped. The live `kitcli.Config` literal in `cmd/ctxt/cmd/root.go` now enables every strict gate. The build-tagged probe (`go test -tags=ctxtbaselineprobe …`) and the hermetic regression test
`TestRootValidate_StrictGatesPass` in `cmd/ctxt/cmd/strict_validation_test.go` both report `root.Validate() == nil`.

### Bucket totals (post-sprint)

| Bucket | Pre-foundation | Post-foundation (T-0593) | Post-sprint (T-0595) |
|--------|----------------|--------------------------|-----------------------|
| `Missing` (side-effect) | 149 | 149 | **0** |
| `MissingIdempotency` | 98 | 98 | **0** |
| `MissingLong` | 79 | 79 | **0** |
| `MissingExamples` (strict) | 149 | 149 | **0** |
| `MissingNextSteps` (strict) | 149 | 149 | **0** |
| `UnannotatedTopLevelLeaf` | 28 | 0 | **0** |
| `UnannotatedDepthExceedance` | 14 | 0 | **0** |
| `TooManyTopLevelVerbs` | 1 | 0 | **0** |
| signature `depth-hierarchical` | 11 | 0 | **0** |
| signature `local-globals` | 31 | 31 | **0** |
| `PassthroughRejected` (strict) | n/a | n/a | **0** |

**Total distinct command paths still failing strict validation: 0.**

### Gates enabled on the live `kitcli.Config`

| Gate | Value |
|------|-------|
| `EnforceValidate` | `true` (kit default) |
| `EnforceGuidance` | `true` |
| `EnforceDryRunRationale` | `true` |
| `EnforceDestructiveToken` | `true` |
| `SignatureStrictness` | `SignatureStrictnessReject` |
| `PassthroughStrictness` | `"reject"` |
| `MaxTopLevelVerbs` | `30` (raised from kit default `10`) |
| `ValidationFailureMode` | `ValidationFailureError` (returned, not `os.Exit`) |

### Command surface metrics

* **160** total commands across the tree (depth-1 verbs + depth-2/3 subtrees).
* **28** top-level (depth-1) runnable verbs after T-0593 grouping — under the explicit `MaxTopLevelVerbs=30` cap. The cap was raised from kit's default of `10` to preserve documented depth-1 verbs (`analyze`, `find`, `show`, etc.) without forcing a UX-breaking group rename.
* Max nesting depth: **3** (kit default cap).
* Reserved subcommand `status` is wired on root (kit reserved-name check passes).

### Regression guard

`cmd/ctxt/cmd/strict_validation_test.go` runs unconditionally (no build tag). It re-mirrors `Execute()`'s pre-flight order — `InitDefaultCompletionCmd → applyCommandGroups → ApplyGroupVisibility → applyShapeAnnotations → root.Validate()` — and asserts `root.Validate() == nil`. Adding a new subcommand without the required annotations fails this test in CI before it can ship.

### Operator UX changes (carried by T-0594 + this close)

* Destructive ctxt commands now require kit's global `--confirm=yes` (or `--confirm=prompt` + `--confirm-token=<sha>`) instead of any per-command `-y/--yes` flag.
* The `ctxt delete -y/--yes` local alias has been removed; affected destructive surfaces include `delete`, `profile delete`, `profile schema remove-*`, `config restore`, `cursor delete`, `detector remove`, `embeddings deprecate/purge`, `feed delete`, `inbox clear`/`discard`, `link delete`, `registry delete`.

### Reproduce post-sprint state

```
/usr/bin/env go test -buildvcs=false -run TestRootValidate_StrictGatesPass -v ./cmd/ctxt/cmd
/usr/bin/env go test -buildvcs=false -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd
```

## Top-level command roots (for T-0593 fan-out)

Discovered roots under `cmd/ctxt/cmd/*` after `applyCommandGroups()` runs:

| Root | Kind |
|------|------|
| `analyze` | leaf |
| `audit` | group |
| `capture` | leaf |
| `classify` | leaf |
| `completion` | leaf |
| `compose` | leaf |
| `config` | group |
| `cursor` | group |
| `delete` | leaf |
| `detector` | leaf |
| `dev` | leaf |
| `doctor` | leaf |
| `edit` | leaf |
| `embeddings` | group |
| `entity` | group |
| `export` | leaf |
| `feed` | group |
| `find` | leaf |
| `help` | group |
| `import` | group |
| `inbox` | group |
| `index` | leaf |
| `ingest` | leaf |
| `instance` | group |
| `job` | leaf |
| `key` | leaf |
| `lateral` | leaf |
| `link` | group |
| `list` | leaf |
| `log` | leaf |
| `page` | group |
| `profile` | group |
| `registry` | group |
| `remind` | group |
| `reprocess` | leaf |
| `resurface` | leaf |
| `secret` | leaf |
| `setup` | leaf |
| `shell` | leaf |
| `show` | leaf |
| `stats` | leaf |
| `status` | leaf |
| `tui` | leaf |
| `upgrade` | group |
| `uri` | group |
| `version` | leaf |
| `watch` | group |

Other binary entrypoints:

* `cmd/ctxt/main.go` → ContextHelp CLI (probed above)
* `cmd/dpkms/main.go` → daemon, separate cobra tree; build verified OK
* `cmd/ben-adapter-ctxt-recall/main.go` → adapter binary; no cobra surface

## Per-command failure checklist

Sorted by command path. `Buckets` is the union across `Validate()` + `ValidateSignature()`. `Example` is the most useful single-line snippet for the conformance work.

| Command path | Buckets | Example error |
|--------------|---------|---------------|
| `ctxt` | shape-violation | 28 top-level verbs > MaxTopLevelVerbs=10 |
| `ctxt analyze` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt audit export` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): format |
| `ctxt audit list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt capture` | local-global-collision, missing-idempotency, missing-side-effect, shape-violation | leaf redefines global flag(s): profile |
| `ctxt classify` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt compose` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt config backup` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): output |
| `ctxt config doctor` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt config edit` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt config path` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt config restore` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt config show` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt config validate` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt cursor delete` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt cursor list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt cursor reset` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt cursor set` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt cursor show` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt delete` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt detector` | missing-Long, shape-violation | missing cmd.Long |
| `ctxt detector add` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt detector disable` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt detector enable` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt detector list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt detector remove` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt dev` | missing-Long, shape-violation | missing cmd.Long |
| `ctxt dev gen-docs` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt dev init-plugin` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt dev reindex-vectors` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt dev validate-registry` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt doctor` | local-global-collision, missing-side-effect, shape-violation | leaf redefines global flag(s): profile |
| `ctxt edit` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt embeddings deprecate` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt embeddings list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt embeddings migrate` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt embeddings purge` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt embeddings register` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): config |
| `ctxt embeddings set-default` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt entity backlink` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt entity list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt entity search` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt entity show` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt export` | local-global-collision, missing-idempotency, missing-side-effect, shape-violation | leaf redefines global flag(s): format |
| `ctxt feed add` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt feed delete` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt feed list` | local-global-collision, missing-side-effect | leaf redefines global flag(s): output |
| `ctxt feed sync` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt find` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt import batch status` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt import chrome` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import discord` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import dropbox` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import edge` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import email` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import evernote` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import firefox` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import gdrive` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import github` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run, output |
| `ctxt import linkedin` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import logseq` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import notion` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import obsidian` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import onedrive` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import pinboard` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import raindrop` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import safari` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import slack` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt import twitter` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt inbox clear` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt inbox discard` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt inbox list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt inbox triage` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt index` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt ingest` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt instance current` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt instance list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt instance use` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt job` | missing-Long, shape-violation | missing cmd.Long |
| `ctxt job cancel` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt job list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt job log` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt job retry` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt job status` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt key` | missing-Long, shape-violation | missing cmd.Long |
| `ctxt key init` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt key rotate` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt lateral` | shape-violation | depth-1 leaf missing kit/top-level-verb |
| `ctxt lateral config` | shape-violation | depth>=3 missing kit/hierarchical chain |
| `ctxt lateral config show` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt lateral eval` | shape-violation | depth>=3 missing kit/hierarchical chain |
| `ctxt lateral eval metrics` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt lateral eval replay` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt lateral start` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt lateral status` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt link create` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt link delete` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt link list` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt list` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt log` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt page list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt page refresh` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt page show` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt profile create` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt profile default` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt profile delete` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt profile list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt profile schema` | shape-violation | depth>=3 missing kit/hierarchical chain |
| `ctxt profile schema add-rule` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema add-topic` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema add-type` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema evolve` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema remove-topic` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema remove-type` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile schema show` | missing-Long, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt profile show` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry add` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry capabilities` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry delete` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry entitlements` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry info` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry login` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry logout` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry search` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry submit` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt registry sync` | local-global-collision, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt registry usage` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt remind clear` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt remind list` | missing-side-effect | missing kit/side-effect annotation |
| `ctxt remind set` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt reprocess` | missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt resurface` | shape-violation | depth-1 leaf missing kit/top-level-verb |
| `ctxt resurface dismiss` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt resurface refresh` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt secret` | missing-Long, shape-violation | missing cmd.Long |
| `ctxt secret get` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt secret list` | missing-Long, missing-side-effect | missing kit/side-effect annotation |
| `ctxt secret set` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt setup` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt shell` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt show` | local-global-collision, missing-side-effect, shape-violation | leaf redefines global flag(s): format |
| `ctxt stats` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt status` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt tui` | missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt upgrade check` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt upgrade install` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt upgrade notes` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt upgrade plan` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt upgrade run` | local-global-collision, missing-idempotency, missing-side-effect | leaf redefines global flag(s): dry-run |
| `ctxt upgrade snooze` | missing-Long, missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt upgrade status` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt uri register` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt uri snippet` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt version` | missing-Long, missing-idempotency, missing-side-effect, shape-violation | missing kit/side-effect annotation |
| `ctxt watch disable` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt watch enable` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt watch start` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt watch status` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |
| `ctxt watch stop` | missing-idempotency, missing-side-effect | missing kit/side-effect annotation |

## Removal patterns (T-0593 foundation → subtree fan-out)

### Global-flag collisions (31 leaves)

Kit owns these names as persistent flags on the root, so leaves that
re-register them shadow the global and trip
`SignatureCheckLocalGlobals`. The complete kit-owned set under the
ctxt root:

| Flag | Origin | Reader |
|------|--------|--------|
| `--dry-run` | kit Config.Disable.DryRun=false (always on) | `cmd.Flags().GetBool("dry-run")` or `sideeffect.IsDryRun(ctx)` |
| `--format`, `-o/--output` | `output.RegisterFlags` (Disable.Format=false) | `cmd.Flags().GetString("format")` |
| `-c/--config` | kit Disable.Config=false | `root.ConfigArgs()` (already wired in `initConfig`) |
| `--profile` | ctxt-supplied via `Globals` | `viper.GetString("profile")` |
| `--quiet`, `--no-color`, `-V/--verbose`, `-C/--chdir`, `--no-hints`, `--progress-format`, `--confirm`, `--max-ops`, `--policy`, `--api-version`, `--offline`, `--instance` | kit + ctxt globals | viper or `cmd.Flags()` |

**Removal pattern for a leaf that re-defines a global**:

```go
// BEFORE — local re-registration shadows the global
importChromeCmd.Flags().Bool("dry-run", false, "preview without enqueueing jobs")
...
dryRun, _ := cmd.Flags().GetBool("dry-run")

// AFTER — read the inherited persistent flag directly
// (cmd.Flags() merges local + inherited; the lookup still works)
dryRun, _ := cmd.Flags().GetBool("dry-run")
```

Subtree agents:

1. Delete the `<cmd>.Flags().Bool("dry-run", …)` / `String("format", …)` /
   `String("output", …)` / `String("config", …)` / `String("profile", …)` line.
2. Leave the `cmd.Flags().GetBool/GetString(<name>)` reader alone — cobra
   resolves it through the inherited persistent flag set.
3. If the leaf had a custom default that differed from kit's, replace
   the default-handling with a fallback inside RunE
   (`if val == "" { val = "<custom-default>" }`).
4. If the local description was load-bearing in `--help`, rely on kit's
   generic description; per-leaf overrides require a custom help template
   and are out of scope for 12fcc conformance.

### Shape annotations (depth-1 + depth-3 trees)

Foundation pass (this commit) handles these centrally in
`cmd/ctxt/cmd/root.go::applyShapeAnnotations` — subtree agents do not
need to stamp `kit/top-level-verb` or `kit/hierarchical` on individual
leaves. The walk runs after `applyCommandGroups` so every top-level
runnable cmd gets `kit/top-level-verb` and every depth-2 intermediate
above a depth-3+ leaf gets `kit/hierarchical`.

### Side-effect / idempotency / Long (subtree work)

Use the helpers in `internal/cli/cliconv`:

```go
// inside import_chrome.go init() — one-liners next to AddCommand
cliconv.WithSideEffect(importChromeCmd, cliconv.SideEffectWrite)
// kit auto-fills kit/idempotent for verbs in defaultIdempotency
// (list/show/get/sync/etc.). For NEW or non-default verbs:
cliconv.WithIdempotency(importChromeCmd, cliconv.IdempotencyNo)
```

Long descriptions stay where they live today (string literal in the
`var <cmd>Cmd = &cobra.Command{Long: ...}` block). Kit's validator
checks `cmd.Long != ""` on every runnable leaf.

## Raw artifacts

* `cmd/ctxt/cmd/baseline_probe_test.go` (build-tagged probe driver, checked in alongside this doc).
* Reproduce JSON dump: `go test -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd 2>&1`.

## Bucket label reference

| Label | Maps to (kit/cli) |
|-------|--------------------|
| missing-side-effect | `kit/side-effect` annotation absent on a runnable leaf |
| missing-idempotency | `kit/idempotent` annotation absent (verb default not enough) |
| missing-Long | `cmd.Long` empty on a runnable leaf |
| missing-status | reserved `status` subcommand not registered on root |
| shape-violation | depth-1 missing `kit/top-level-verb`, or depth>=3 missing `kit/hierarchical`, or > `MaxTopLevelVerbs`, or > `MaxHierarchyDepth` |
| missing-passthrough | command uses `cobra.ArbitraryArgs` without `kit/passthrough` |
| local-global-collision | leaf redefines a flag name owned by the persistent global flag set (e.g. `--format`, `--profile`, `--dry-run`, `--output`, `--config`) |
