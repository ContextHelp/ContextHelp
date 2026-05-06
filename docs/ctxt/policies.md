# ctxt policies

ctxt's daemon (`dpkms serve`) uses kit's runtime policy engine
(`hop.top/kit/go/runtime/policy`) to gate state-changing pipeline
operations. Policies are declarative YAML rules, compiled once at
daemon boot, and evaluated synchronously against the kit
pre_persisted topic before any storage write. A denial vetoes the
mutation before the row changes.

For engine semantics, expression vocabulary, deny-overrides
composition, and the canonical worked examples see kit ADR-0008
(`docs/adr/0008-kit-runtime-policy-engine.md` in the kit repo).
This doc covers ctxt-specific adoption only.

## Default policies

ctxt ships two rules out of the box:

```yaml
policies:
  - name: archive-pipeline-requires-note
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "update" || !has(context.request_attrs.action) || context.request_attrs.action != "archive" || context.note != ""'
    effect: allow
    otherwise: deny
    message: "archiving a pipeline requires --note explaining why"

  - name: delete-pipeline-requires-note
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "delete" || context.note != ""'
    effect: allow
    otherwise: deny
    message: "deleting a pipeline requires --note explaining why"
```

Effect: `dpkms pipeline archive <name>` and `dpkms pipeline remove
<name>` without `--note|-n` are rejected with exit code 4.

```text
$ dpkms pipeline remove my-pipeline
Error: POLICY_DENIED: policy "delete-pipeline-requires-note" denied: deleting a pipeline requires --note explaining why
$ echo $?
4

$ dpkms pipeline remove my-pipeline --note "deprecated, replaced by v2"
$ echo $?
0
```

`pipeline create` and `pipeline unarchive` accept `--note` but the
default bundle does not require it — adopters opt in by extending
the user policy file.

## Where the policy file lives

| Resolution order | Source |
|------------------|--------|
| 1 | `$CTXT_POLICY_FILE` env var (used by tests + CI) |
| 2 | `$XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml` (default `~/.config/contexthelp/policy/ctxt.yaml`) |

> **Path relocation 2026-05-06.** The PR #23 path
> `$XDG_CONFIG_HOME/contexthelp/policies.yaml` (flat) was moved to
> `policy/ctxt.yaml` (namespaced) by the
> [ADR-065 amendment](../decisions/ADR-065-pluggable-adapters.md#amendment-2026-05-06--service-vs-sensor-distinction-ambient-capture-os-platform-slots).
> The new `policy/` subdirectory groups `ctxt.yaml` (CEL gating rules)
> and `ambient.yaml` ([sensor enablement config](ambient.md)) under one
> roof. **Migration is automatic** on first boot after the upgrade;
> operators with custom `$CTXT_POLICY_FILE` overrides must update them
> manually.

On first daemon boot, ctxt seeds option 2 from the bundled default
if it is missing or empty. **Existing non-empty user files are never
clobbered.** Edit the file in place to extend or override.

If seeding fails (read-only home, etc.) and `$CTXT_POLICY_FILE` is
unset, ctxt falls back to the embedded default — enforcement still
applies. The error surfaces the next time the user tries to edit and
the file isn't there.

## Anatomy of a policy

```yaml
policies:
  - name: <unique-string>          # for error messages + logs
    on: <kit-topic>                # see "Available topics" below
    when: <CEL expression>         # evaluated per matching event
    effect: allow | deny           # outcome when `when` is true
    otherwise: allow | deny        # outcome when `when` is false
    message: <string>              # surfaced via PolicyDeniedError
```

Both `effect` and `otherwise` are required — no implicit defaults.
This forces every policy to state both match-outcome and
no-match-outcome explicitly.

When multiple policies match a single event, kit applies
**deny-overrides**: ANY policy resolving to `deny` wins, and the
first denying policy's message is surfaced. See ADR-0008 §8.

## Available topics

ctxt currently publishes one veto-able topic, fired by
`domain.Service[Pipeline]` (server-side) on every Create / Update /
Delete:

| Topic | When it fires | ctxt commands that publish |
|-------|--------------|----------------------------|
| `kit.runtime.entity.pre_persisted` | After validation, before repo write | `dpkms pipeline create / remove / archive / unarchive` |

`kit.runtime.state.pre_transitioned` and
`kit.runtime.entity.pre_validated` are valid kit topics but ctxt
does not publish them yet. Writing a policy on those topics is not
an error — it loads, compiles, never matches, never fires.

Registry mutations (`dpkms pipeline step registry update`, etc.) do
**not** fire policy events: the registry storage is a manifest cache
without entity CRUD lifecycle, so it falls outside the
domain.Service[T] fit. Add registry-level guards by extending the
storage layer or wiring a custom subscriber, not via policy YAML.

## Available context attributes

The CEL `when` expression sees four bindings (see ADR-0008 §4):

| Binding | Type | Notes for ctxt |
|---------|------|----------------|
| `payload.Op` | string | `"create"`, `"update"`, or `"delete"` |
| `payload.EntityID` | string | The pipeline name (Pipeline.GetID returns Name, not the UUID — T-1294) |
| `payload.Phase` | string | Always `"pre_persisted"` for ctxt today |
| `principal.id` | string | Resolved from kit's `DefaultPrincipalResolver` (today: `$USER`). Aps profile lookup is a follow-up — see `internal/policy/policy.go` `ctxtPrincipalResolver` |
| `principal.role` | string | From `KIT_POLICY_ROLE` env var. Empty when unset |
| `context.note` | string | Value from `--note|-n` on the CLI, plumbed through `X-Ctxt-Note` HTTP header |
| `context.request_attrs.action` | string | `"archive"` or `"unarchive"` for those subcommands; absent on plain create / delete / update |
| `resource.kind`, `resource.fields` | dyn | Pipeline reflection — see ADR-0008 §4 |

Use `has(context.request_attrs.action)` to check presence; CEL
errors on missing keys without `has()`.

## Examples

### Block deletion of built-in pipelines

```yaml
policies:
  - name: delete-not-builtin
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "delete" || !resource.fields.is_built_in'
    effect: allow
    otherwise: deny
    message: "built-in pipelines cannot be deleted via API"
```

(ctxt already enforces this in `service.DeletePipeline`; the policy
is shown for symmetry. Using both is fine — deny-overrides keeps
the message stable when either path fires.)

### Require admin role to delete

```yaml
policies:
  - name: delete-admin-only
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "delete" || principal.role == "admin"'
    effect: allow
    otherwise: deny
    message: "only role:admin may delete pipelines"
```

Set the role per-shell:

```bash
export KIT_POLICY_ROLE=admin
dpkms pipeline remove my-pipeline --note "manual cleanup"
```

### Note minimum length

```yaml
policies:
  - name: delete-note-min-length
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "delete" || size(context.note) >= 20'
    effect: allow
    otherwise: deny
    message: "delete note must be ≥20 chars; explain why this pipeline goes away"
```

### Compose with defaults

ctxt applies deny-overrides across all matching policies. With both
the default `delete-pipeline-requires-note` and a custom
`delete-admin-only` rule active, deletes need both `--note` AND
`role:admin`. Order in the file does not matter; the first denying
policy's message is surfaced.

## Authoring custom policies

kit's `e2e/` directory is the canonical reference. Each test file is
one runnable user story; lift the YAML and CEL idioms from there:

- `kit/go/runtime/policy/e2e/README.md` — story index + reading order
- `kit/go/runtime/policy/e2e/story_delete_requires_note_test.go` —
  the story ctxt's `delete-pipeline-requires-note` rule was modeled on
- `kit/go/runtime/policy/e2e/story_admin_only_cancel_test.go` —
  role-gated transitions
- `kit/go/runtime/policy/e2e/story_deny_overrides_compose_test.go` —
  multi-policy semantics

Compile-time validation:

- All CEL programs compile at daemon boot. A broken expression fails
  `dpkms serve` loud before any HTTP request is accepted.
- Topic strings are validated against the kit allowlist
  (`kit.runtime.state.pre_transitioned` /
  `kit.runtime.entity.pre_validated` /
  `kit.runtime.entity.pre_persisted`); other values fail at boot.

## Exit codes

| Code | When |
|------|------|
| 0 | Success |
| 1 | Generic error |
| 4 | Policy denied — daemon returns HTTP 409 with `code: POLICY_DENIED`; CLI maps to exit 4 via `cmd/dpkms/cmd.ExitCodeFor` |

The policy denial is distinguishable from other errors by the
prefix:

```text
Error: POLICY_DENIED: policy "<name>" denied: <message>
```

## Operator notes

- The default rules change behavior for `dpkms pipeline archive` and
  `dpkms pipeline remove`. Old scripts that called these commands
  without `--note` exit 4 after upgrade. Update them to pass
  `--note "<reason>"`.
- The note flows over HTTP via the `X-Ctxt-Note` header. Direct API
  consumers (non-CLI clients) must set this header on
  `POST /api/v1/pipelines`, `DELETE /api/v1/pipelines/<name>`,
  `POST /api/v1/pipelines/<name>/archive`, and
  `POST /api/v1/pipelines/<name>/unarchive`. Empty / missing header
  reads as `context.note == ""` in CEL.
- To restore pre-T-1296 behavior temporarily, edit
  `$XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml` (post-2026-05-06
  relocation; was `policies.yaml`) and remove the two default rules.
  ctxt does not re-seed once a non-empty user file exists, so the
  change persists across upgrades.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `policy: load <path>: ...` at daemon boot | Malformed YAML | Validate the file; daemon fails loud rather than ignore |
| `policy: build engine: ...` at daemon boot | Broken CEL in `when` | Compare to ADR-0008 §3 examples and the kit e2e stories |
| `POLICY_DENIED` on every API call | A new rule's `otherwise: deny` fires for unrelated events | Tighten the `when` clause so non-target events match and `effect: allow` |
| Custom rule never fires | Topic typo, or ctxt doesn't publish that topic yet | See "Available topics" — ctxt only publishes `kit.runtime.entity.pre_persisted` today |
| `no such key: action` in eval error | Reading `context.request_attrs.action` without `has()` | Wrap in `has(context.request_attrs.action)` first; CEL errors on missing keys |

## See also

- [api-cli.md](api-cli.md) — full CLI reference, including the
  `--note|-n` flag rows and the exit-code table
- kit ADR-0008 — engine design, full vocabulary, alternatives
- `internal/policy/policies_default.yaml` — the bundled default
- `internal/policy/policy.go` — engine bootstrap (loaded by
  `dpkms serve` PersistentPreRunE)
