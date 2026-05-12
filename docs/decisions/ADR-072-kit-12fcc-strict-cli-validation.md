# ADR-072 – Kit 12fcc strict CLI validation at boot

> **Status:** Accepted
> **Date:** 2026-05-12
> **Author:** jadb
> **Supersedes:** none
> **Superseded by:** none

---

## Context

`hop.top/kit` ships a CLI validator (`Root.Validate()`) that gates a CLI tree against the 12fcc conformance contract: every runnable leaf must declare its side-effect (`kit/side-effect`), idempotency (`kit/idempotent`), surface guidance (`kit/examples` + `kit/next-steps` where applicable), and — when destructive — a typed-token opt-in (`kit/destructive-token`). The shape validator additionally enforces depth-1 `kit/top-level-verb`, depth-3+ `kit/hierarchical` ancestor stamping, depth caps, and a top-level-verb count cap (`MaxTopLevelVerbs`).

Before this sprint, `ctxt`:

- Bypassed kit's pre-flight `Validate` because `Execute()` called `fang.Execute(..., WithoutVersion())` directly to keep ctxt's bespoke `--version` behavior (build-date + JSON output + `upgrade check` link). Kit's own `Root.Execute(ctx)` calls `fang.WithVersion(...)`, which intercepts `--version` and replaces it.
- Carried 160 distinct conformance failures across the documented buckets (149 missing-side-effect, 98 missing-idempotency, 79 missing-Long, 28 unannotated top-level leaves, 14 depth-3+ unannotated ancestors, 11 hierarchical sig issues, 31 local-global flag collisions, 1 `TooManyTopLevelVerbs`).
- Registered 28 depth-1 verbs against kit's default cap of 10 (ctxt is a meta-tool combining capture + curate + compose + organize, so the natural surface is wider than the single-purpose CLIs kit's defaults were tuned for).
- Allowed per-command destructive bypasses via local `-y/--yes` flags on at least `delete`, `profile delete`, `profile schema remove-*`, `config restore`, `cursor delete`, `detector remove`, `embeddings deprecate`, `embeddings purge`, `feed delete`, `inbox clear`/`discard`, `link delete`, `registry delete`. Kit's reserved `--confirm` global was unused.

The 12fcc-conformance track (T-0591 → T-0595) closed every bucket, raised `MaxTopLevelVerbs` to `30`, replaced per-command `-y` with kit's `--confirm` policy, and now flips the strict gates on at boot.

---

## Decision

`ctxt` enables full kit 12fcc strict CLI validation at boot:

1. The live `kitcli.Config` literal in `cmd/ctxt/cmd/root.go` sets `EnforceGuidance: true`, `EnforceDryRunRationale: true`, `EnforceDestructiveToken: true`, `SignatureStrictness: SignatureStrictnessReject`, `PassthroughStrictness: "reject"`, alongside the existing `EnforceValidate: true` (kit default) and `ValidationFailureMode: ValidationFailureError`.
2. `ctxt.Execute()` invokes `root.Validate()` explicitly (because the call site uses `fang.Execute(..., WithoutVersion())`); a failure returns an error up to `main.go` which prints + exits non-zero.
3. `MaxTopLevelVerbs` is set to `30` on the live Root config. Re-evaluate at the next major release.
4. Destructive ctxt commands rely exclusively on kit's global `--confirm=yes` (or `--confirm=prompt` with `--confirm-token=<sha>`). The legacy per-command `-y/--yes` flag is removed.

A hermetic regression test (`TestRootValidate_StrictGatesPass`, `cmd/ctxt/cmd/strict_validation_test.go`, no build tag) mirrors `Execute()`'s pre-flight and asserts `root.Validate() == nil`. Adding a new subcommand without the required annotations fails this test in CI.

---

## Rationale

**Why strict at boot, not warn?** A 12fcc violation means a user-facing surface ships without machine-readable side-effect, idempotency, or guidance metadata. Downstream consumers — agents, MCP bridges, doc generators, autocomplete renderers — depend on those annotations to behave correctly. Warning at boot trains operators to ignore stderr noise; failing fast localises the regression to the PR that introduces it.

**Why a cap of 30, not the kit default of 10?** Forcing ctxt under 10 depth-1 verbs would require either (a) renaming user-facing verbs into nouns (`ctxt analyze` → `ctxt capture analyze`, etc.), which breaks documented muscle memory and link rot, or (b) hiding verbs behind `--help-all`, which degrades discoverability. The cap was raised explicitly (not removed) so the next surface expansion still triggers a deliberate review.

**Why drop `-y/--yes`?** Two reasons:
- *Local-global collision*: every per-command `-y` shadowed a potential kit global. Kit owns the destructive-confirm UX (`--confirm=yes`, `--confirm=prompt`, `--confirm-token=<sha>`); per-command flags fragmented behavior across leaves.
- *Token-gated automation*: kit's `--confirm=prompt --confirm-token=<sha>` enables typed-token confirmation that scripts can produce deterministically from the command path + args. The old `-y` was a binary bypass with no audit trail at the kit layer.

**Alternatives rejected**:
- *Keep `-y` as an alias and accept the local-global collision warning*: rejected because the alias would still surface conformance noise on every boot under `SignatureStrictness=warn`, and we want strict-reject everywhere.
- *Reduce surface to fit `MaxTopLevelVerbs=10`*: rejected as a UX-breaking change with no clear semantic win for users.
- *Disable strict gates and rely on CI lint only*: rejected because boot-time validation is the only mechanism that catches an out-of-band local install (e.g. operator `go install`s a fork). The probe + CI test belt-and-suspender approach already runs in CI; boot-time strict-reject is the runtime contract.

---

## Consequences

### Positive

- A new ctxt command landing without `kit/side-effect`, `kit/idempotent`, `kit/examples` (or `kit/next-steps` + `kit/destructive-token` where applicable) fails `TestRootValidate_StrictGatesPass` in CI and prevents `ctxt` from starting at all post-merge.
- Destructive confirmation UX is uniform: every destructive leaf accepts `--confirm=yes` or `--confirm=prompt --confirm-token=<sha>`; no per-leaf flag surface to learn.
- Downstream consumers (agents, MCP read-surface, doc generators) can trust the 12fcc annotations are present on every leaf.

### Negative

- Scripts that previously piped `-y` to `ctxt delete` (or any other listed destructive leaf) must move to `--confirm=yes`. This is a script-level breaking change documented in `docs/release-notes/2026-05-12.md`.
- `MaxTopLevelVerbs=30` is a configuration concession — it sets the bar for accepting new top-level verbs higher (the validator only complains at 31+), but doesn't itself reduce surface. The next major should re-attempt the consolidation under the kit default.

### Neutral or Considerations

- ctxt continues to bypass kit's `Root.Execute(ctx)` and call `fang.Execute(..., WithoutVersion())` directly. Any future kit pre-flight step that lands in `Root.Execute` must be replicated explicitly in `ctxt.Execute()`. The current set is documented inline (completion registration, `applyCommandGroups`, `ApplyGroupVisibility`, `applyShapeAnnotations`, `Validate`).
- The build-tagged baseline probe (`cmd/ctxt/cmd/baseline_probe_test.go`, tag `ctxtbaselineprobe`) is retained for sprint-style bucket dumps. It is invisible to the default test command.

---

## Implementation Notes

Affected modules:
- `cmd/ctxt/cmd/root.go` — `kitcli.Config` literal: strict-gate flags + `MaxTopLevelVerbs: 30` + `applyShapeAnnotations` walk + explicit `root.Validate()` in `Execute()`.
- `cmd/ctxt/cmd/strict_validation_test.go` — regression guard (no build tag).
- `cmd/ctxt/cmd/baseline_probe_test.go` — sprint probe driver (build tag `ctxtbaselineprobe`).
- `internal/cli/cliconv/` — helpers for stamping `kit/side-effect`, `kit/idempotent`, `kit/examples`, `kit/next-steps`, `kit/destructive-token` on cobra commands. Subtree work in T-0593/T-0594 routed every annotation through these.
- `docs/sprints/12fcc-conformance-baseline.md` — sprint baseline + Post-Sprint State.
- `docs/release-notes/2026-05-12.md` — Operator-Impact rows for the strict-gate flip + `--confirm` UX change.

Migration concerns:
- Operators with `-y`-piped scripts must update to `--confirm=yes`. The release-notes file lists every affected destructive leaf.
- Forks that add ctxt commands must use `internal/cli/cliconv` helpers or stamp the annotations manually; otherwise `ctxt` will refuse to start.

Testing:
- `TestRootValidate_StrictGatesPass` runs in every `go test ./...` invocation. It is hermetic (no DB, no sqlite, no FTS5) so the pre-existing CGO/FTS5 gap in some ctxt integration tests does not occlude it.
- The build-tagged probe is the bucket-level diagnostic when the test fails — point CI at `go test -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd` for the JSON-dump variant.

---

## References

- ADR-068 — MCP read surface (consumes 12fcc annotations downstream).
- ADR-070 — pipeline-and-index versioning (release-notes contract this ADR's operator-impact rows participate in).
- `hop.top/kit` v0.3.2-patch.3 — pinned upstream via local `replace → kit/hops/12fcc-leak` during the sprint.
- `docs/sprints/12fcc-conformance-baseline.md` — sprint baseline + Post-Sprint State.
- `docs/release-notes/2026-05-12.md` — operator-impact rows.
