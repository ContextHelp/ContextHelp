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
env scrubbed (the ctxt recorder also scrubs `CH_*` and `BUS_TOKEN`),
so the knowledge database a scenario writes never leaks between
scenarios or into the developer's real data. `USER` and `LOGNAME` are
pinned to a fixed token so the recording host's account name can never
reach a committed capture.

### ctxt scenarios and the server

ctxt commands run over the dpkms API, and with nothing configured ctxt
targets the built-in default endpoint (`127.0.0.1:8080`) — where a
developer's real instance may be listening. `12fcc-record.sh` never
lets a scenario reach it. Before recording, it writes `server.url`
into the scenario's own user config
(`$XDG_CONFIG_HOME/contexthelp/ctxt.yaml`) and checks, through `ctxt
config show`, that ctxt resolves exactly that URL; on a mismatch it
refuses to record. Each scenario has one of two server modes, set per
scenario in the script's `server_mode` from what the scenario and its
story say they test (an unclassified scenario is refused):

| Mode | Scenarios | ctxt is routed to |
|---|---|---|
| live | `delete-preview`, `format-contract`, `profile-idempotency`, `resolve-contract`, `state-transparency` | a throwaway `dpkms serve` started for that scenario alone |
| closed | `offline-state-envelope` ("orientation works with nothing running") | `http://127.0.0.1:1`, a port nothing listens on |

The live instance is private (loopback only, no inbound tokens) with
its own `HOME`/XDG tree and SQLite database, stub providers and stub
embeddings, a random bus token held only in the daemon's env, and HTTP
and gRPC on OS-assigned ports. It starts empty: no scenario seeds data,
and the empty-store expectations (`knowledge_objects: 0`, `total: 0`)
depend on that. It leads its own process group; the recorder stops the
group before the next scenario starts and on every exit path, and
waits until the port stops accepting connections.

The port is random, so byte stability rests on no capture naming it.
After each scenario the recorder fails the run if any capture contains
a default-port endpoint, the live instance's address or bus token, the
repo path, or the operator's `HOME`/`TMPDIR`.

The work root is a fixed `/tmp/ctxt-12fcc-record` rather than a
`$TMPDIR` path: `ctxt config path` in `offline-state-envelope` reports
the scenario's config file, and a fixed root keeps that capture stable
and free of host-specific temp paths. Overriding it with
`CTXT_12FCC_WORK` changes that capture.

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

Record fresh cassettes (builds the binary with the `fts5` tag —
the ctxt recorder builds `dpkms` too, for its throwaway instance —
isolates a work dir per scenario, runs every scenario step as a real
subprocess; the ctxt recorder needs `curl` and `python3`):

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

`verdicts/` is the source of truth, not this table: it is rewritten on
every grade, and the table below reflects the last committed re-grade.

| Scenario | Binary | Verdict | Factors | Contract probed |
|---|---|---|---|---|
| `resolve-contract` | ctxt | pass | F3 F4 F11 | missing object, missing entity, undefined flag: corrective envelope + matching exit class |
| `state-transparency` | ctxt | pass | F3 F5 F8 F11 | derived counts as JSON fields; empty store and no-match search explicit; repeat reads agree |
| `delete-preview` | ctxt | pass | F3 F6 F8 F11 | declined delete yields a machine-readable preview; state unchanged after |
| `format-contract` | ctxt | pass | F3 F4 F5 F11 | listed formats honored; unlisted ones refused as `USAGE`, naming the value |
| `offline-state-envelope` | ctxt | pass | F3 F4 F5 F8 F11 | version and config answer as data with nothing running; unknown subcommand refused |
| `profile-idempotency` | ctxt | pass | F3 F4 F5 F7 F8 F11 | re-asserting the active profile converges; repeat create distinguishable by exit class |
| `state-contract` | dpkms | pass | F3 F5 F8 | instance, job, and detector listings as stable JSON; empty collections stay collections |
| `daemon-unreachable` | dpkms | pass | F3 F4 F11 | unreachable daemon, absent job, rejected flag: corrective envelope + matching exit code |
| `maintenance-preview` | dpkms | pass | F3 F4 F6 F11 | writing maintenance steps preview as a JSON plan; refusals addressed to the caller |
| `preview-idempotent` | dpkms | pass | F6 F7 F8 | two identical previews agree, mutate nothing, enqueue no work |
| `output-flag-contract` | dpkms | pass | F3 F5 F11 | the output flag help documents is the one the leaf accepts and honors |

Each scenario's own `description` states its contract in full. The
dpkms scenarios probe the substrate in its stopped state (see
[Not covered](#not-covered)).

Per-factor rollup takes the worst status seen in any scenario carrying
the factor, so one failing scenario turns a factor red even where
others pass it. At the last re-grade every scenario passes, so every
service-graded factor rolls up `pass`:

| Factor | Carried by |
|---|---|
| F3 Structured I/O | every scenario except `preview-idempotent` |
| F4 Corrective Error Model | `resolve-contract`, `format-contract`, `offline-state-envelope`, `profile-idempotency`, `daemon-unreachable`, `maintenance-preview` |
| F5 Explicit Contracts | `state-transparency`, `format-contract`, `offline-state-envelope`, `profile-idempotency`, `state-contract`, `output-flag-contract` |
| F6 Previewability | `delete-preview`, `maintenance-preview`, `preview-idempotent` |
| F7 Idempotency | `profile-idempotency`, `preview-idempotent` |
| F8 State Transparency | `state-transparency`, `delete-preview`, `offline-state-envelope`, `profile-idempotency`, `state-contract`, `preview-idempotent` |
| F11 Exit Code Semantics | every scenario except `state-contract` and `preview-idempotent` |

Scenarios encode the target contract, not current behavior: when a
regression lands, re-record and re-grade, and the verdict flips without
touching the rubric.

## Notes on the rubrics

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
  without depending on the service knowing the symbol. Every other
  `exit_code_class` assertion names a class from the known row.

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
kept as corroborating evidence only: every step it covers also has its
payload pinned by `output_schema_matches`.

One more consequence of using `output_schema_matches`: the grading
service's JSON-schema library embeds the serving process's working
directory in its failure messages (`does not validate with
file:///<cwd>/inline.json#/...`). Verdicts are committed and this repo
is public, so `12fcc-grade-all.sh` rewrites that prefix to
`file://<scenario-root>` after grading and before the verdict lands.
Only the message text is rewritten; statuses and observed values are
left exactly as the service returned them, and the scrub refuses to
write a file that stopped parsing as JSON.

## Not covered

No dpkms scenario starts a daemon (the ctxt recorder's throwaway
instance serves ctxt scenarios only). `dpkms serve` requires `DPKMS_BUS_TOKEN`,
and the recorders scrub `DPKMS_*` precisely so ambient operator
environment cannot reach a committed capture; a live-instance scenario
would have to reintroduce that env. The consequence is that dpkms F8
is measured against the stopped substrate only. The surfaces that do
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
