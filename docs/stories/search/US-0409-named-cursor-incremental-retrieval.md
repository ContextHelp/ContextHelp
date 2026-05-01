# Story: Named Cursor for Incremental Retrieval

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs/Tools](../../personas/agents-llms-tools.md)
**Status:** paper
**Author:** $USER
**Task:** T-0089

---

## User Goal

As a knowledge worker (or agent on my behalf), I want a named, persistent
cursor over `ctxt list` results so I can poll "what's new since I last
looked?" without re-deriving a date filter or losing my place across
sessions, machines, and tool runs.

---

## Context

Existing primitives are close but insufficient:

- `ctxt list --after <date>` filters by absolute time → caller must
  remember when they last looked and pick a date manually.
- `ctxt list --since <duration>` is relative → drifts on every call;
  no per-viewer state.
- Resurface queue (US-0319) is profile-relevance scored, not chronological;
  wrong primitive for "feed since last read".
- Saved search (US-0054) captures a query but no read position.

A *named cursor* is the missing primitive: a tiny per-viewer marker
(timestamp + last-object-id + captured query snapshot) that any list
call can advance. Primary motivating workflow: client dashboards
(scenario-1) where Noor polls `ctxt list --cursor sales-acme
--mention @client.acme --advance` to see only what's accumulated
since last check-in. Same pattern serves agent inboxes, weekly
review feeds, and a future thin web dashboard.

Sovereign storage principle: cursor state is local-only. No
server-side cursor table. File at `~/.config/contexthelp/cursors.yaml`,
human-readable, hand-editable, portable.

---

## Acceptance Criteria

- [ ] `ctxt list --cursor <name>` returns only items added after the
      cursor's `last_seen_at` (or all matching items if cursor is new)
- [ ] `--cursor <name> --advance` advances cursor to the most-recent
      returned item after the listing completes successfully
- [ ] New cursor (no prior state) starts at epoch 0 → returns full
      matching set; first `--advance` initializes state
- [ ] Cursor name validation: lowercase alphanumeric + hyphens, ≤64
      chars; invalid name → exit 2 with helpful message
- [ ] Cursor file is YAML, human-readable, safe to hand-edit
- [ ] Cursor file location overridable via `CTXT_CURSOR_FILE` env var
- [ ] `ctxt cursor list` lists all cursors with name, last_seen_at,
      last_object_id, and (when available) match count
- [ ] `ctxt cursor show <name>` prints cursor state + captured query
      snapshot (mention, tag, profile, q, type, after filters)
- [ ] `ctxt cursor reset <name>` rewinds cursor to epoch 0
- [ ] `ctxt cursor set <name> --to <ts>` jumps to a specific timestamp
      (RFC3339 or relative like `-7d`)
- [ ] `ctxt cursor delete <name>` removes the cursor from file
- [ ] Composes with `--mention`, `--tag`, `--profile`, `--q`, `--after`,
      `--type` (cursor adds an additional `created_at > last_seen_at`
      gate over the existing filter)
- [ ] Unknown cursor name on `--cursor foo` (without `--advance`):
      exits with helpful message; suggests `ctxt cursor list`
- [ ] Query-snapshot drift: if current flags differ from cursor's stored
      snapshot, print a warning to stderr but continue (user decides);
      `--advance` updates the snapshot
- [ ] File format is versioned (`schema_version: 1`); upgrade path
      reserved for future revisions
- [ ] Concurrent `--advance` from two processes is safe (atomic
      rename on write; advisory file lock during read-modify-write)
- [ ] `--output json` returns `{cursor: {...}, items: [...], advanced: bool}`

---

## Implementation Notes

### CLI surface

```
ctxt list --cursor <name> [--advance] [other list flags...]
ctxt cursor list                         # all cursors
ctxt cursor show <name>                  # state + query snapshot
ctxt cursor reset <name>                 # rewind to epoch 0
ctxt cursor set <name> --to <ts>         # jump to timestamp
ctxt cursor delete <name>                # remove cursor
```

### Storage

- File: `~/.config/contexthelp/cursors.yaml` (override:
  `$CTXT_CURSOR_FILE`)
- Directory created lazily with `0700`; file `0600`
- Schema (versioned):

```yaml
schema_version: 1
cursors:
  sales-acme:
    last_seen_at: 2026-04-28T14:22:11Z
    last_object_id: ko_01HZ...
    query:
      mention: ["@client.acme"]
      tag: []
      profile: ""
      q: ""
      type: ""
      after: ""
    created_at: 2026-04-20T09:00:00Z
    updated_at: 2026-04-28T14:22:11Z
```

### Composition

Cursor adds one extra `created_at > last_seen_at` predicate to the
list query; all existing filters apply unchanged. Sort is forced to
`created_at ASC` when `--cursor` set so `--advance` can pick the tail
deterministically. `--advance` only fires on success (non-empty AND
list call exited 0); empty results leave cursor untouched.

### Concurrency

- Read: open + parse + close
- Write (advance / set / reset / delete): write to
  `cursors.yaml.<pid>.tmp`, fsync, `rename(2)` over the target
- Advisory `flock(LOCK_EX)` on a sidecar `cursors.yaml.lock` for the
  read-modify-write window of `--advance`

### Out of scope (this story)

- Server-side cursor mirroring → never (sovereign storage)
- Cursor sync across machines → out of scope; user copies the file
- Auto-advance-on-alert wiring → US-0054 follow-up

---

## E2E Tests

All tests `planned:` (story is paper). Target package
`tests/e2e/cursor/`. Go test binary, against a temp dpkms.

- planned: `tests/e2e/cursor/cursor_basic_test.go::TestCursor_NewCursorReturnsAll`
- planned: `tests/e2e/cursor/cursor_basic_test.go::TestCursor_AdvanceMovesPosition`
- planned: `tests/e2e/cursor/cursor_basic_test.go::TestCursor_NoAdvanceLeavesPositionUnchanged`
- planned: `tests/e2e/cursor/cursor_basic_test.go::TestCursor_EmptyResultDoesNotAdvance`
- planned: `tests/e2e/cursor/cursor_compose_test.go::TestCursor_ComposesWithMention`
- planned: `tests/e2e/cursor/cursor_compose_test.go::TestCursor_ComposesWithTag`
- planned: `tests/e2e/cursor/cursor_compose_test.go::TestCursor_ComposesWithProfile`
- planned: `tests/e2e/cursor/cursor_compose_test.go::TestCursor_ComposesWithQAndAfter`
- planned: `tests/e2e/cursor/cursor_subcommands_test.go::TestCursor_List`
- planned: `tests/e2e/cursor/cursor_subcommands_test.go::TestCursor_ShowIncludesQuerySnapshot`
- planned: `tests/e2e/cursor/cursor_subcommands_test.go::TestCursor_ResetRewindsToEpoch`
- planned: `tests/e2e/cursor/cursor_subcommands_test.go::TestCursor_SetJumpsToTimestamp`
- planned: `tests/e2e/cursor/cursor_subcommands_test.go::TestCursor_DeleteRemovesEntry`
- planned: `tests/e2e/cursor/cursor_validation_test.go::TestCursor_NameValidationRejectsUppercase`
- planned: `tests/e2e/cursor/cursor_validation_test.go::TestCursor_NameValidationRejectsTooLong`
- planned: `tests/e2e/cursor/cursor_validation_test.go::TestCursor_UnknownCursorWithoutAdvanceFails`
- planned: `tests/e2e/cursor/cursor_drift_test.go::TestCursor_QuerySnapshotDriftWarnsButContinues`
- planned: `tests/e2e/cursor/cursor_drift_test.go::TestCursor_AdvanceUpdatesQuerySnapshot`
- planned: `tests/e2e/cursor/cursor_storage_test.go::TestCursor_FileLocationOverrideViaEnv`
- planned: `tests/e2e/cursor/cursor_storage_test.go::TestCursor_FilePermissionsLockedDown`
- planned: `tests/e2e/cursor/cursor_storage_test.go::TestCursor_SchemaVersionPersisted`
- planned: `tests/e2e/cursor/cursor_concurrent_test.go::TestCursor_ConcurrentAdvanceSafe`
- planned: `tests/e2e/cursor/cursor_output_test.go::TestCursor_JSONOutputShape`

unit-only (covered by package tests, not e2e):

- name validator regex
- YAML round-trip + schema_version migration scaffold
- timestamp parser for `cursor set --to <ts>` (RFC3339 + relative)

---

## Open Questions / Future

- Auto-advance-on-alert: tie cursor advance to US-0054 alert delivery.
- Per-cursor TTL / GC for stale cursors.
- `ctxt cursor export` / `import` for cross-machine portability.
- Multi-cursor union (`--cursor a,b`) → likely out of scope.

---

## Related Stories

- US-0016 — Natural-language search (composes via resolved RSQL)
- US-0017 — Structured RSQL query (cursor adds time-gate predicate)
- US-0319 — Resurface by profile relevance (scored, not chronological)
- US-0054 — Saved search and alerts (future: advance on alert)
- US-0055 — Search history and recommendations
