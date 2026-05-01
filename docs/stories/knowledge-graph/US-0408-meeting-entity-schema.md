# US-0408: @meeting.<date-slug> Entity Schema

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

**Status:** paper
**Author:** $USER
**Task:** T-0147

---

## User Goal

Drop audio/transcripts via meeting-intake-pipeline; need
`@meeting.<date-slug>` entity with required fields validated at
capture so summaries + follow-ups extract reliably.

---

## Context

Application of [US-0407](US-0407-typed-entity-schema.md). Does NOT
redefine registry, validation, or typed-payload storage — those are
US-0407. Specifies `@meeting.*` schema instance + drop-script
contract.

**Non-goals:** redefining US-0407; summary/followups extraction
(pipeline-side, future); meeting UI; auto-recording.

---

## Acceptance Criteria

- [ ] `ctxt entity register meeting --schema ./meeting-schema.yaml`
  via US-0407 verbatim
- [ ] Required: `title`, `datetime`, `participants`,
  `artifact_type`, `dropped_at`, `dropped_by`
- [ ] Optional: `location`, `context_hints`
- [ ] `datetime` + `dropped_at` ISO 8601
- [ ] `artifact_type` enum: `audio | transcript | both | none`
- [ ] `participants[]`: each entry is `@person.<slug>` OR free-form
  string — both valid in same array
- [ ] `context_hints[]`: mention strings; empty OK
- [ ] Invalid payload rejected with field-level error per US-0407
- [ ] `ctxt list --type entity --entity-type meeting` filters
- [ ] `ctxt list --mention @meeting.<slug>` returns typed object
- [ ] Mention graph composes: meeting mentioning
  `@project.lesexperts` creates standard back-edge — no
  meeting-specific edge type
- [ ] Derived (reserved): `summary` (string), `followups[]`
  (`{owner, action, due?}`)
- [ ] Drop script auto-sets `dropped_at`, `dropped_by`,
  `artifact_type` — never user-supplied
- [ ] Schema at `~/.config/contexthelp/entity-schemas/meeting.yaml`
- [ ] `schema_version: 0.1`; bumps follow US-0407

---

## Implementation Notes

### Schema (illustrative — actual JSON Schema on disk)

```yaml
schema_version: 0.1
type: meeting
required: [title, datetime, participants, artifact_type, dropped_at, dropped_by]
properties:
  title:         { type: string, minLength: 1 }
  datetime:      { type: string, format: date-time }
  location:      { type: string }
  participants:
    type: array
    minItems: 1
    items: { type: string }      # @person.<slug> or free-form
  context_hints:
    type: array
    items: { type: string, pattern: "^@[a-z0-9_-]+\\.[a-z0-9_-]+$" }
  artifact_type: { type: string, enum: [audio, transcript, both, none] }
  dropped_at:    { type: string, format: date-time }
  dropped_by:    { type: string, minLength: 1 }
  summary:       { type: string }    # derived; pipeline-set
  followups:                         # derived; pipeline-set
    type: array
    items:
      type: object
      required: [owner, action]
      properties:
        owner:  { type: string }
        action: { type: string }
        due:    { type: string, format: date }
```

### Slug shape

`@meeting.<YYYY-MM-DD>-<short-title-slug>` — date-prefix sorts
naturally; collisions append `-<n>`. Pure US-0407 mention syntax.

### Drop script contract (pseudocode)

```
drop-meeting <file> [--title T] [--participants P,Q]
  artifact_type = inspect(file)
  dropped_at    = now_iso8601()
  dropped_by    = aps_current_user_id()
  payload = { title, datetime, participants, location?,
              context_hints?, artifact_type, dropped_at, dropped_by }
  ctxt analyze <file> --mention @meeting.<slug> --payload-json <json>
```

### Storage

Reuses US-0407 columns: `bookmarks.payload`,
`bookmarks.entity_type='meeting'`, `bookmarks.payload_schema_version`.
No new tables.

---

## E2E Tests (planned)

Path: `tests/e2e/typed_entity/meeting_test.go`

- `TestMeeting_RegisterSchema` — register succeeds; `entity types` lists v0.1
- `TestMeeting_ValidPayloadAccepted` — full drop persists with typed payload
- `TestMeeting_MissingDatetimeRejected` — field-level US-0407 error
- `TestMeeting_InvalidArtifactTypeRejected` — `video` not in enum
- `TestMeeting_ParticipantsMixedForms` — `@person.x` + free-form both accepted
- `TestMeeting_MentionGraphBacklinks` — meeting → `@project.lesexperts` back-edge

---

## Open Questions / Future

- `summary` + `followups` extraction = separate story
- Cross-meeting links via `@meeting.<slug>` in `followups[].action`
- Per-org schemas via federated registry (US-0019)
- Auto-derive `participants` from transcript NER

---

## Related Stories

- [US-0407](US-0407-typed-entity-schema.md) — schema mechanism
- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) —
  capture entry point
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md)
  — mention extraction
- [US-0405](US-0405-append-only-changelog.md) — meeting drops as
  `create` mutations
