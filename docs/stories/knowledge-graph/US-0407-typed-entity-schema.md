# US-0407: Typed Entity Schema for Mentions

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Agents/LLMs](../../personas/agents-llms-tools.md)

**Status:** paper
**Author:** $USER
**Task:** T-0109

---

## User Goal

As a knowledge worker (or pipeline author), I want to register a JSON
Schema for an entity TYPE (e.g. `@event.*`, `@meeting.*`,
`@client.*`) so capture pipelines validate required fields per type
and store a typed payload alongside the bookmark. Today every mention
is an opaque slug — there's no way to declare "an event MUST have
date + venue + start_time".

---

## Context

Mentions today (`docs/dpkms/mentions.md`, `schema-entity.md`) define
identity (`@<ns>.<slug>` → canonical entity ID) and registry
resolution (title, aliases, translations). They do NOT define a
**payload schema** for the bookmark that mentions the entity. So
`@event.beirut-jazz-2026-05-03` and `@event.foo` are
indistinguishable to the pipeline — neither carries date, venue,
category, scores.

Newsletter scenarios (`scenarios/4a`, `scenarios/4b`) need typed
events with date / venue / category / scoring fields the publisher
can reason over. Meeting-intake (`meeting-intake-pipeline/spec.md`)
needs typed meetings (date, attendees, agenda). Both want the same
primitive: per-type schema + validation + typed storage.

This story adds that primitive. Events = worked example. Other
consumers (meetings, clients, projects, sessions) register their own
types with their own schemas via the same mechanism.

**Non-goals:**
- NOT replacing mention syntax (`@<ns>.<slug>` parses unchanged)
- NOT changing entity identity / resolution (US-0405-style)
- NOT mandating schemas — untyped mentions remain valid (open default)
- NOT building UI for schema authoring (CLI / file only this iter)

---

## Acceptance Criteria

- [ ] Entity TYPE registry: `ctxt entity register <type> --schema
  <path>` registers a JSON Schema (or YAML) for `@<type>.*` mentions
- [ ] Multiple entity types coexist in one ctxt instance (event,
  meeting, client, project, …) — each with its own schema
- [ ] On `ctxt analyze`, when input mentions `@<type>.<slug>` and
  `<type>` has a registered schema, the supplied payload is
  validated against the schema
- [ ] Validation failure → capture rejected with a specific
  field-level error (`event.venue.name: required, missing`)
- [ ] Validation success → typed payload is persisted on the
  bookmark, joined to the canonical entity by `mention_uri`
- [ ] `ctxt list --type entity --entity-type event` filters to typed
  objects of a given entity type
- [ ] `ctxt list --mention @event.<slug>` returns the typed object
  for the given mention
- [ ] `ctxt entity show <mention>` displays the typed payload (not
  just the canonical entity record)
- [ ] Schemas can also be loaded from a registry (per
  `docs/dpkms/registries.md`) so an org publishes "this is what an
  event looks like" once, all instances pick it up
- [ ] Schemas are versioned (`schema_version: 0.1`); capture clamps
  to a specific version with `--schema-version <ver>`
- [ ] Backward compat: untyped mentions (no registered schema for
  the type) continue to work; default schema = open / permissive
- [ ] JSON output (`ctxt list --json`, `ctxt entity show --json`)
  includes `entity_type` + `payload_schema_version` fields
- [ ] Plugins can register a custom validation hook that runs
  alongside built-in JSON Schema validation (e.g. cross-field rules,
  external lookups)
- [ ] `ctxt entity types` lists registered entity types + schema
  versions

---

## Implementation Notes

### Schema registry

Two sources, registry takes precedence:

- Local: `~/.config/contexthelp/entity-schemas/<type>.yaml`
- Registry-published: per `docs/dpkms/registries.md` — entity type
  schemas as a new resource kind alongside entity definitions

### Storage

- `bookmarks.payload` JSON column holds the typed payload
- `bookmarks.entity_type`, `bookmarks.payload_schema_version` columns
  for filtering / migration
- `mention_uri` remains the join key (no change to existing schema
  semantics)

### CLI

```
ctxt entity register event --schema ./event-schema.yaml
ctxt entity types
ctxt entity show @event.beirut-jazz-2026-05-03
ctxt entity show @event.beirut-jazz-2026-05-03 --json
ctxt analyze --mention @event.<slug> --payload @./event.json
ctxt list --type entity --entity-type event --since 2026-05-01
```

### Composition

- Plugs into existing capture pipeline (US-0001 .. US-0008): typed
  entities are bookmarks with schema-validated payloads — same
  enrichment, same indexing, same federation
- Mention extraction (`docs/dpkms/mentions.md`) unchanged
- Entity resolution (US-0405 schema-entity) unchanged — typed
  payload is bookmark-side, identity is entity-side

### Worked example: `@event.*` schema (illustrative)

```yaml
# pseudocode — actual JSON Schema on disk
schema_version: 0.1
type: event
required: [date, title, venue, category, start_time, source_url]
properties:
  date: { type: string, format: date }              # ISO 8601
  title: { type: string, minLength: 1 }
  venue:
    type: object
    required: [name]
    properties:
      name: { type: string }
      address: { type: string }
  category:
    type: string
    enum: [music, tech, food, family, sports, arts, civic, other]
  start_time: { type: string, format: date-time }
  end_time: { type: string, format: date-time }
  price: { type: string }                            # free|paid|range
  source_url: { type: string, format: uri }
  popularity_score: { type: integer, minimum: 0, maximum: 100 }
  novelty_score: { type: integer, minimum: 0, maximum: 100 }
  audience_fit_score: { type: integer, minimum: 0, maximum: 100 }
  composite_score: { type: integer, minimum: 0, maximum: 100 }
```

`composite_score` derived; pipeline-set, not user-set.

---

## E2E Tests (planned)

Path: `tests/e2e/typed_entity/`

- `register_test.go::TestEntity_RegisterSchema`
- `validate_test.go::TestEntity_ValidPayloadAccepted`
- `validate_test.go::TestEntity_MissingRequiredFieldRejected`
- `validate_test.go::TestEntity_WrongTypeRejected`
- `list_test.go::TestEntity_ListByType`
- `list_test.go::TestEntity_ListByMention`
- `show_test.go::TestEntity_ShowDisplaysPayload`
- `registry_test.go::TestEntity_SchemaFromRegistry`
- `registry_test.go::TestEntity_SchemaVersioning`
- `compat_test.go::TestEntity_UntypedMentionsStillWork`
- `plugin_test.go::TestEntity_CustomValidationHook`

---

## Open Questions / Future

- Schema migration: when an org bumps `event` 0.1 → 0.2, do existing
  typed bookmarks lazy-migrate, eager-migrate, or stay pinned?
- Cross-type references: can `@event.<slug>` payload contain
  `@venue.<slug>`? (likely yes; future story on typed-link
  validation across schemas)
- Derived fields: should the schema declare which fields are
  pipeline-derived (`composite_score`) vs user-supplied? Today the
  pipeline just overwrites — explicit declaration would be cleaner
- TUI / browser-extension forms generated from schema (post-paper)
- Schema discovery via federated registry search (US-0019)

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) —
  capture entry point (typed payload arrives here)
- [US-0009](../enrichment/US-0009-extract-entities-and-mentions.md) —
  mention extraction (unchanged; typed payload is orthogonal)
- [US-0046](../enrichment/US-0046-extract-relationships-between-entities.md)
  — entity-relationship enrichment
- [US-0405](US-0405-append-only-changelog.md) — changelog entries
  for schema register / payload validation events
- [US-0406](US-0406-associative-object-links.md) — typed payloads
  enrich the link graph (e.g. `event` extends `venue`)
- [US-0044](../plugins/US-0044-implement-registry-adapter-plugin.md)
  — registry adapter (delivers schemas alongside entity defs)
- [US-0109](../plugins/US-0109-fetch-registry-manifest.md) — registry
  manifest fetch (extend to carry entity type schemas)
