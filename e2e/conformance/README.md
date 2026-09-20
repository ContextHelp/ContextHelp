# Conformance grading (12fcc service tier)

Scenario library, recorded cassettes, and captured verdicts for the
service-graded factors of the 12-Factor AI-CLI conformance contract:
F3 Structured I/O, F4 Corrective Error Model, F5 Explicit Contracts,
F6 Previewability, F7 Idempotency, F8 State Transparency, and F11
Exit Code Semantics. The story-graded factors (F1, F2, F9, F12) and
leak scanning (F10) are covered by the `12fcc` leaves over
`e2e/stories/`.

## Layout

```text
scenarios/<binary>/<id>/1.0.0/scenario.yaml  graded rubric, one per scenario
stories/<id>.yaml                        user story each scenario is bound to
cassettes/<id>/                          captures from real binary runs
  manifest.yaml                          upload manifest (story hash, steps)
  story.yaml                             byte-exact copy of the story
  steps/<step-id>/result.json            exit code + duration as observed
  steps/<step-id>/stdout.txt             stdout as observed
  steps/<step-id>/stderr.txt             stderr as observed
verdicts/<id>.json                       grading service verdict, tier 3
```

Cassettes are recorded, never authored: every stdout/stderr byte and
exit code comes from executing the actual binary via `kit conformance
harness record`. Scenarios encode the spec-correct expectation even
where current behavior violates it — a failing verdict is the honest
outcome, not a broken pipeline. Re-records churn only `recorded_at`,
`binary_version`, and `duration_ms`; captures are byte-stable.

Each scenario runs in its own throwaway work dir with `HOME` and all
four XDG roots pointed inside it and `CTXT_*` / `DPKMS_*` / `KIT_*`
env scrubbed, so the knowledge database a scenario writes never leaks
between scenarios or into the developer's real data. `USER` and
`LOGNAME` are pinned to a fixed token so the recording host's account
name can never reach a committed capture.

## Keeping `verify-no-leak` clean

Scenario rubrics are exactly the shape `kit conformance verify-no-leak`
exists to catch: a top-level `scenario_id` trips rule R1 and the
`assertions` list trips R2. That is correct behavior — the detector is
meant to stop rubrics leaking into stories, docs, and commit messages,
where they would let an implementation be written against the answer
key.

This directory is the one place rubrics legitimately live, so it is
allowlisted at the repo root in `.verifynoleak.allow`:

```text
e2e/conformance/**
```

The file uses gitignore syntax (negation with `!` supported) and is
read by the scanner's suppression layer, so `--staged`, `--diff`, and
`--audit` all honor it. The scope is deliberately narrow: a rubric
committed anywhere else — `e2e/stories/` included — is still reported.
Verify that property after changing the allowlist, not just that the
gate went green.

## Re-running

Record fresh cassettes (builds the binary with the `fts5` tag,
isolates a work dir per scenario, runs every scenario step as a real
subprocess):

```sh
KIT_BIN=/path/to/kit scripts/12fcc-record.sh        # ctxt scenarios
KIT_BIN=/path/to/kit scripts/12fcc-record-dpkms.sh  # dpkms scenarios
```

The two recorders are separate because they build different binaries
from different entry points and iterate different scenario namespaces.
Grading covers every cassette regardless of which recorder produced
it, but the scaffold's `12fcc-grade.sh` mints a `grade:ctxt` token
only; `12fcc-grade-all.sh` derives one `grade:<ns>` scope per
namespace directory under `scenarios/` and grades the whole library.

Grade them against a locally served scenario library (requires a
`kit` binary that ships the `conformance` command group; point
`KIT_BIN` at one built from `hop.top/kit` `cmd/kit`):

```sh
KIT_BIN=/path/to/kit scripts/12fcc-grade.sh
```

The grade script boots `kit conformance svc serve` on a loopback port
with `--scenarios-root e2e/conformance`, mints a `grade:ctxt` token
into a throwaway claims DB, uploads every cassette at tier 3,
rewrites `verdicts/`, and prints per-scenario verdicts plus a
per-factor rollup. Grading is measurement, not a gate: the script
only fails when a cassette cannot be graded at all. Set `STRICT=1` to
also fail on `fail` verdicts.

## Badge

`scripts/12fcc-badge.sh` renders `.12fc.json` — the shields.io
endpoint JSON — from measurements only: the story and leak leaves over
`e2e/stories/` supply F1, F2, F9, F10, F12, and the tier-3 verdicts in
`verdicts/` supply F3–F8 and F11. A factor with no measurement stays
`skip`; an ungradable verdict aborts rather than being guessed at.

```sh
KIT_BIN=/path/to/kit scripts/12fcc-record.sh        # re-record ctxt
KIT_BIN=/path/to/kit scripts/12fcc-record-dpkms.sh  # re-record dpkms
KIT_BIN=/path/to/kit scripts/12fcc-grade-all.sh     # tier-3 verdicts into verdicts/
KIT_BIN=/path/to/kit scripts/12fcc-badge.sh         # verify leaves + verdicts -> .12fc.json
```

Commit the resulting `.12fc.json` diff together with the re-recorded
cassettes and verdicts.

## Current verdicts

| Scenario | Binary | Verdict | Factors |
|---|---|---|---|
| `resolve-contract` | ctxt | see verdicts/ | F3 F4 F11 |
| `state-transparency` | ctxt | see verdicts/ | F3 F5 F8 F11 |
| `delete-preview` | ctxt | see verdicts/ | F3 F5 F6 F8 F11 |
| `format-contract` | ctxt | see verdicts/ | F3 F4 F5 F11 |
| `offline-state-envelope` | ctxt | see verdicts/ | F3 F4 F5 F8 F11 |
| `profile-idempotency` | ctxt | see verdicts/ | F3 F4 F5 F7 F8 F11 |
| `state-contract` | dpkms | see verdicts/ | F3 F5 F8 |
| `daemon-unreachable` | dpkms | see verdicts/ | F3 F4 F11 |
| `maintenance-preview` | dpkms | see verdicts/ | F3 F4 F6 F11 |
| `preview-idempotent` | dpkms | see verdicts/ | F6 F7 F8 |
| `output-flag-contract` | dpkms | see verdicts/ | F3 F5 F11 |

Verdicts are regenerated by `scripts/12fcc-grade.sh`; `verdicts/` is the
source of truth, not this table.

Per-factor rollup across the library: **F8 pass**; F3, F4, F5, F6, F7,
and F11 fail. Every factor except F6 and F7 has at least one passing
leaf; the rollup takes the worst status seen anywhere, so one failing
scenario keeps a factor red even where another scenario passes it.

One defect accounts for most of the red: several leaves ignore
`--format` and write human-rendered prose to stdout. It surfaces as F3
(stdout is not machine-readable), F5 (the advertised format contract is
not honored), and, where the value is a field a caller needs, F7.

### `resolve-contract` — fail (F4, F11)

`resolve-contract` is the scaffold's proving scenario: it probes the
three ways a resolve request goes wrong — an object ref that was never
stored, an entity slug that was never registered, and a flag the
parser does not define — and asserts the spec-correct contract for
each.

What passes:

- **F3** — stream discipline holds on the failure path. A failed
  resolve writes nothing to stdout and keeps diagnostics on stderr, so
  a pipe never eats half a document.
- **F4, partially** — the failing input is echoed back (`obj_deadbeef`,
  `nonexistent.slug`, `bogus-flag`), and a rejected flag points the
  caller at `--help`.

What genuinely fails, and why the assertions stay as written:

- **F4 — no error envelope under `--format json`.** `ctxt resolve
  obj_deadbeef --format json` renders a human error block on stderr and
  writes nothing machine-readable: no `code`, no `suggested_fix`. A
  caller that explicitly asked for JSON is owed a keyed envelope on the
  failure path too, otherwise it is back to regexing prose. The same
  holds for a missing entity.
- **F4 — errors are descriptive, not corrective.** A missing object
  names no recovery command. Telling the caller to try `ctxt find`
  turns a dead end into a next step.
- **F11 — no exit-code classes.** Every failure exits `1`: missing
  object, missing entity, unknown flag, unknown command, missing
  argument. kit's exit-class table distinguishes `USAGE=2`,
  `NOT_FOUND=3`, and `CONFLICT=4`, and an agent needs that split to
  tell a request it can repair from knowledge that is simply absent.
  Collapsing every class to `1` forces prose parsing.

These are contract gaps in `ctxt`, not defects in the rubric. When a
fix lands, re-record and re-grade; the scenario already encodes the
target, so the verdict flips without touching the rubric.

## dpkms findings

The five `dpkms` scenarios probe the substrate in its stopped state.
Every one grades `fail`; each failure below is a contract gap in
`dpkms`, not a defect in the rubric.

**`--format json` is accepted and ignored on most surfaces.** `dpkms
ps --format json` prints the sentence `No running dpkms instances.`;
with an instance up it prints a rendered table. `dpkms housekeeping
run --dry-run --format json` prints a progress log and a summary
block. `dpkms backup --format json` prints `OK <path> (db: …)`. `dpkms
job list --format yaml` prints the human table. A caller that declared
it cannot read prose is answered with prose, and has no signal that
the request was dropped.

**Two output flags disagree.** `dpkms ps` defines both `--format` and
`-o/--output`. `--output json` returns JSON; `--format json` returns a
table. Worse, `dpkms healthcheck --help` documents the spelling
`--output json`, and `healthcheck` defines only `--format` — so the
route the help teaches is not the route the leaf implements.

**Empty collections are `null`, and some have no envelope at all.**
`dpkms detector list --format json` and `dpkms ps --output json` with
nothing running both emit bare `null` rather than a keyed object with
an empty array. `dpkms job list --format json` emits `{"jobs": null,
"total": 0}` — `null` where an empty list belongs, forcing every caller
to special-case the idle substrate before it can iterate.

**No exit-code classes.** Every failure exits `1`: an unreachable
daemon, a job id that does not exist, an unknown flag, an unknown
command, and a wrong argument count. kit's table distinguishes
`USAGE=2`, `NOT_FOUND=3`, `CONFLICT=4`, and `TRANSIENT=6`, and the
retry decision depends on that split — "wait and retry" and "this will
never exist" are opposite instructions collapsed to the same code.

**No JSON error envelope, and errors leak internals.** A failure under
`--format json` renders a human error block on stderr with no `code`
and no `suggested_fix`. `dpkms job status <missing>` reports `job not
found: sql: no rows in result set`, surfacing the database driver's
words in a user-facing error. `dpkms healthcheck` writes its message
to stderr three times: once raw, once inside a rendered block, and
once more on an `Error:` line.

**Only two dry-run surfaces exist, and one of them refuses in the
implementer's voice.** `restore` and `housekeeping run` honour
`--dry-run`; `vacuum`, `reindex`, `compact`, `prune`, and `backup` do
not. Because `--dry-run` is a kit-global flag it is accepted on all of
them and then rejected at runtime with `--dry-run cannot be applied to
"dpkms housekeeping vacuum": the command is missing the required
kit/side-effect tag; adopter must call cli.SetSideEffect(cmd, ...)`.
That names a defect in code the caller does not own and offers it no
recovery. The steps that most need previewing — the one that rewrites
the database file and the one that deletes rows — are the ones that
cannot be previewed.

**What holds.** Stream discipline is sound on the failure path: every
failing step writes nothing to stdout and keeps diagnostics on stderr.
`housekeeping run --dry-run` genuinely does not mutate, and is
byte-stable across repeated calls. `restore --dry-run` leaves the
database untouched, verified by hashing it either side of the call.
Unknown flags echo the rejected flag and point at `--help`.

### Notes on the rubrics

Two grader properties shaped how these assertions are written, and
both would otherwise have produced misleading results:

- `output_field_equals` / `_present` / `_count` report "stdout is not
  valid JSON" as **ungradable**, not as a failure. Since a command
  answering a JSON request with prose is exactly the violation under
  test, those verbs would file a real contract gap as a measurement
  error. Where the surface does not emit JSON, the expectation is
  stated with `output_schema_matches`, which calls the same condition
  a fail.
- `exit_code_class` resolves an unrecognized class name to `GENERIC`
  (1) rather than rejecting it, so an unknown symbol silently means
  "expect exit 1". Probing the grading service with a cassette whose
  observed code is 1 separates the symbols it knows (the assertion
  fails) from the ones it does not (the assertion passes, exactly as a
  deliberately bogus name does):

  | Class | Deployed grader |
  |---|---|
  | `OK`, `NOT_FOUND`, `USAGE`, `CONFLICT`, `UNAUTHORIZED`, `RATE_LIMITED` | known |
  | `TRANSIENT`, `PROVENANCE_MISSING`, `CONSENT_REFUSED`, `PREREQUISITE` | unknown — silently mean exit 1 |

  `classes: [TRANSIENT]` therefore returned a false pass against a
  command that exits 1. The unreachable-daemon expectation is stated
  as `exit_code_equals: 6` instead, which pins the same contract
  without depending on the service knowing the symbol. These scenarios
  otherwise use only `OK`, `NOT_FOUND`, and `USAGE`, all verified to
  resolve correctly.

  Note this is narrower than the class table in the grader's own
  source, which does list `TRANSIENT` and `PROVENANCE_MISSING`. The
  deployed binary is an unstamped local build whose source point is
  not recoverable, so "which commit is it" cannot settle the question
  — only probing can. Treat the table above as a property of whichever
  grader is actually serving, and re-probe rather than trusting the
  source, before writing a class name outside the six known ones.

  To re-probe, grade a cassette whose observed exit code is the one
  the class claims, and read the result both ways:

  - The class resolves as documented → the assertion **passes**.
  - The class is unknown → it silently resolves to 1, so the
    assertion **fails**, exactly as a deliberately bogus name does.

  Use a positive control in the same run — a class you can confirm,
  against a binary that exits its code — so a method fault is not
  mistaken for a missing symbol. `CONFLICT` against a binary exiting 4
  passes, which is what makes the same harness's verdict on
  `PROVENANCE_MISSING` against a binary exiting 65 (fail, identical to
  a bogus name) trustworthy rather than an artifact. Checking whether
  the class name merely appears in the binary is not sufficient:
  `PROVENANCE_MISSING` is compiled in for error rendering while the
  grader's lookup map still lacks it.

`dry_run_no_mutation` passes vacuously when a step records no adapter
traffic, so a command that refuses to run at all satisfies it. It is
kept as corroborating evidence but is never the only assertion
carrying F6 for a step.

One more consequence of using `output_schema_matches`: the grading
service's JSON-schema library embeds the serving process's working
directory in its failure messages (`does not validate with
file:///<cwd>/inline.json#/...`). Verdicts are committed and this repo
is public, so `12fcc-grade-all.sh` rewrites that prefix to
`file://<scenario-root>` after grading and before the verdict lands.
Only the message text is rewritten; statuses and observed values are
left exactly as the service returned them, and the scrub refuses to
write a file that stopped parsing as JSON.

### Not covered

No scenario starts a daemon. `dpkms serve` requires `DPKMS_BUS_TOKEN`,
and the recorders scrub `DPKMS_*` precisely so ambient operator
environment cannot reach a committed capture; a live-instance scenario
would have to reintroduce that env. The consequence is that F8 is
measured against the stopped substrate only. The surfaces that do
report live state — `dpkms ps --output json` and `dpkms healthcheck
--format json` — were probed manually against an isolated instance on
a non-default port: `healthcheck` returns a clean, host-independent
envelope, while `ps --output json` embeds absolute database paths,
PIDs, and start timestamps, so neither its shape nor its stability
could be pinned in a committed cassette.

`dpkms config paths` is likewise excluded: it is a genuine F8 surface
emitting well-formed JSON, but every entry is an absolute path derived
from the recording host's working directory, which cannot be committed.

Separately, `dpkms secret list --format json` enumerates the names of
every environment variable in the calling process. That is host state
rather than substrate state and cannot be captured, but it is worth
recording as a finding in its own right.
