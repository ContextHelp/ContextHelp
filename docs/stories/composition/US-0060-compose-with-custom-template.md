# US-0060: Compose With Custom Template

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to define and use custom composition templates so I can produce briefs and documents that match my organisation's specific structure and language rather than only using built-in templates.

---

## Context

Built-in templates (default, executive, technical, chronological) cover common cases, but teams often have house styles, governance frameworks, or reporting formats that differ. Custom templates let teams define their own section structure, instructions, and output format, and apply them to any composition command. Templates can be scoped to an individual, a team, or an organisation, enabling reuse and consistency across the team.

---

## Acceptance Criteria

- [ ] User can create a template from a YAML or JSON file (`ctxt template create --file <path>`)
- [ ] User must supply a template name (`--name <name>`)
- [ ] User can supply an optional description (`--description <text>`)
- [ ] User can set a scope: `personal`, `team`, or `org` (`--scope <scope>`)
- [ ] Template record stores `name`, `scope`, and `sections` (id, title, instructions, format)
- [ ] Template sections are stored in submission order; order preserved on retrieval
- [ ] User can list templates (`ctxt template list`); newly created template appears in list
- [ ] User can use a custom template in composition (`ctxt make brief --template <custom-name>`)
- [ ] Custom template name is resolved server-side; not inlined in the composition request payload
- [ ] Custom template sections appear in output in declared order
- [ ] Custom `instructions` field drives section content generation (verified via output content)
- [ ] `format: bullet_list` produces bullet list; `format: numbered_list` produces numbered list
- [ ] Unknown section format falls back to prose without error
- [ ] Updating a template via PUT /templates/{name} increments version; old compositions are unaffected
- [ ] Deleting a template returns 200; subsequent GET /templates/{name} returns 404
- [ ] Using a deleted template name in a composition returns 404 with descriptive error
- [ ] Invalid template YAML/JSON returns 400 with parse error details
- [ ] Template name collision returns 409 with descriptive error
- [ ] Missing required `name` field returns 400 with descriptive error

---

## Implementation Notes

### CLI Interface

```bash
# Create a custom template
ctxt template create --file my-template.yaml --name sprint-review \
  --description "Sprint review doc" --scope team

# List templates (includes built-ins and custom)
ctxt template list

# Use custom template in brief generation
ctxt make brief --from o-abc123 --template sprint-review

# Delete a template
ctxt template delete sprint-review

# Returns JSON with template record
{
  "template_id": "tmpl-xyz789",
  "name": "sprint-review",
  "scope": "team",
  "description": "Sprint review doc",
  "sections": [
    {"id": "summary", "title": "Sprint Summary", "instructions": "...", "format": "prose"},
    {"id": "shipped", "title": "Shipped", "instructions": "...", "format": "bullet_list"}
  ]
}
```

### Template YAML Format

```yaml
name: sprint-review
description: Sprint review document
scope: team
sections:
  - id: summary
    title: Sprint Summary
    instructions: Summarise what the team accomplished this sprint
    format: prose
  - id: shipped
    title: Shipped
    instructions: List features and fixes delivered
    format: bullet_list
  - id: risks
    title: Risks and Blockers
    instructions: List unresolved blockers and risks
    format: numbered_list
```

### REST API

```
POST /templates
Content-Type: application/json

{
  "name": "sprint-review",
  "description": "Sprint review doc",
  "scope": "team",
  "sections": [...]
}

→ 200 OK  (template created)
→ 400 Bad Request  (invalid YAML/JSON, missing name)
→ 409 Conflict     (name already exists)

GET /templates/{name}
→ 200 OK  (template record with sections)
→ 404 Not Found    (template does not exist)

PUT /templates/{name}
→ 200 OK  (updated; version incremented)

DELETE /templates/{name}
→ 200 OK  (deleted)
→ 404 Not Found    (template does not exist)
```

---

## E2E Test Checklist

### CLI → Server payload propagation (template registration)
- [ ] `ctxt template create --file <path>` sends template YAML/JSON body in POST to /templates
- [ ] `--name <name>` sends `name` in POST body
- [ ] `--description <text>` sends `description` in POST body when provided
- [ ] `--scope <personal|team|org>` sends `scope` in POST body when provided

### CLI → Server payload propagation (using custom template)
- [ ] `ctxt make brief --template <custom-name>` sends `template: "<custom-name>"` in POST body
- [ ] Custom template name resolved server-side; not inlined in request payload

### Server-side receipt and storage
- [ ] POST /templates persists template; GET /templates/{name} returns same record with sections
- [ ] Template record stores `name`, `scope`, `sections` (id, title, instructions, format)
- [ ] Template sections stored in submission order; order preserved on retrieval
- [ ] Template used in composition: GET /compositions/{id} shows `template` field matching custom name
- [ ] Updating template via PUT /templates/{name} increments version; old compositions unaffected

### CLI output validation
- [ ] `ctxt template create --file <path>` exits 0 and returns template record JSON with `template_id`
- [ ] `ctxt template list` exits 0 and includes newly created template name
- [ ] `ctxt make brief --template <custom-name>` exits 0 and composition uses correct sections

### Template behaviour
- [ ] Custom template sections appear in output in declared order
- [ ] Custom `instructions` field drives section content generation (validated via output content)
- [ ] `format: bullet_list` produces bullet list; `format: numbered_list` produces numbered list
- [ ] Unknown section format falls back to prose (no error)
- [ ] Deleting template with `ctxt template delete <name>` returns 200; GET /templates/{name} returns 404
- [ ] Using deleted template name returns 404 with descriptive error

### Error handling
- [ ] Invalid template YAML/JSON returns 400 with parse error details
- [ ] Template name collision returns 409 with descriptive error
- [ ] Missing required `name` field returns 400 with descriptive error

---

## Related Stories

- [US-0022: generate-brief-from-objects](./US-0022-generate-brief-from-objects.md) — Brief generation that consumes custom templates
- [US-0025: export-brief-to-markdown-pdf](./US-0025-export-brief-to-markdown-pdf.md) — Export output shaped by custom template
- [US-0059: compose-recommendation-document](./US-0059-compose-recommendation-document.md) — Recommendation document using custom template
- [US-0028: register-custom-pipeline](../admin/US-0028-register-custom-pipeline.md) — Parallel extensibility pattern (pipelines)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)

---

## E2E Tests

- `test/integration/us0060_custom_template_test.go::TestUS0060_CustomTemplateSectionsRegisteredInPipeline`
- `test/integration/us0060_custom_template_test.go::TestUS0060_CustomTemplateContentInterpolated`
- `test/integration/us0060_custom_template_test.go::TestUS0060_CustomTemplateSectionOrderInComposition`
- `test/integration/us0060_custom_template_test.go::TestUS0060_BulletListSectionFormatPreserved`
