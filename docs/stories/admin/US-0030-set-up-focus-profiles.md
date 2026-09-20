---
status: shipped
---

# US-0030: Set Up Focus Profiles

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md),
[Maintainers](../../personas/maintainers.md)

---

## User Goal

As a knowledge worker or maintainer, I want to create and manage focus profiles so I can
tailor search, enrichment, and capture behaviour to specific roles or projects.

---

## Context

Focus profiles are named presets stored in the ctxt config file
(`~/.config/contexthelp/config.yaml`). They carry `description`, `tags`, `mention_namespaces`,
and `rerank_boosts`. All `ctxt profile` subcommands operate on the local config file — there
is no server-side REST endpoint. The `--profile` flag on `ctxt add` passes the profile name
in the `POST /analyze` payload so the server can apply rerank boosts and tag defaults.

---

## Acceptance Criteria

- [ ] `ctxt profile create <name>` adds the profile to the config file
- [ ] `ctxt profile delete <name>` removes the profile from the config file
- [ ] `ctxt profile set-default <name>` persists the default profile in config
- [ ] `ctxt profile set-default` (no arg) clears the default profile
- [ ] `ctxt profile list` shows all profiles and marks the default with `*`
- [ ] `ctxt profile show <name>` displays `description`, `tags`, `mention_namespaces`,
  `rerank_boosts`
- [ ] `ctxt profile list --output json` returns JSON with `default` and `profiles` keys
- [ ] `ctxt add --profile <name>` passes profile name in request payload to server
- [ ] Creating a duplicate profile returns an error
- [ ] Deleting a non-existent profile returns an error
- [ ] Config file changes persist across process restarts

---

## Implementation Notes

### CLI Commands

```
ctxt profile list
ctxt profile list --output json

ctxt profile show <name>
ctxt profile show <name> --output json

ctxt profile create <name>
ctxt profile delete <name>
ctxt profile set-default <name>
ctxt profile set-default          # clears default
```

### Config File Structure (YAML)

```yaml
profile:
  default: founder
  profiles:
    founder:
      description: "Startup founder lens"
      tags: [strategy, growth]
      mention_namespaces: [investor, company]
      rerank_boosts:
        decision: 1.5
        entity: 1.2
    engineer:
      description: "Engineering deep-dives"
      tags: [code, architecture]
      mention_namespaces: [repo, tool]
      rerank_boosts:
        code: 1.8
```

### Profile Used at Capture (REST Payload)

```
POST /analyze
Content-Type: application/json

{
  "content": "Strategic insight about fundraising",
  "content_type": "text/plain",
  "profile": "founder",
  "project": "series-a"
}
```

---

## E2E Test Checklist

### Create — Config File Written
- [ ] Create: `ctxt profile create myproject` exits `0` and prints confirmation
- [ ] Create: Config file (`~/.config/contexthelp/config.yaml`) contains `myproject` key
  under `profile.profiles` with auto-generated `description` field
- [ ] Create: Duplicate name → exits non-zero with "profile already exists" error
- [ ] Create: `ctxt profile list` immediately reflects new profile (no restart needed)

### Delete — Config File Updated
- [ ] Delete: `ctxt profile delete myproject` exits `0`
- [ ] Delete: Config file no longer contains `myproject` under `profile.profiles`
- [ ] Delete: Deleting the current default also clears `profile.default` in config
- [ ] Delete: Non-existent name → exits non-zero with "profile not found" error

### Set-Default — Config File Updated
- [ ] SetDefault: `ctxt profile set-default <name>` sets `profile.default` in config file
- [ ] SetDefault: `ctxt profile list` shows `*` next to the new default
- [ ] SetDefault: `ctxt profile set-default` (no arg) clears `profile.default` to empty string
- [ ] SetDefault: Non-existent name → exits non-zero with "profile not found" error

### List — Output Reflects Config
- [ ] List: Human-readable output shows all profile names and marks default with `*`
- [ ] List: `--output json` returns `{"default":"...","profiles":{...}}` matching config contents
- [ ] List: Empty profiles section → prints "No profiles defined."

### Show — All Fields Displayed
- [ ] Show: `ctxt profile show <name>` prints `description`, `tags`, `mention_namespaces`,
  `rerank_boosts` for the named profile
- [ ] Show: `--output json` returns `{"name":"...","is_default":true/false,"profile":{...}}`
- [ ] Show: Non-existent name → exits non-zero with "profile not found" error

### Profile Persistence Across Restarts
- [ ] Persist: Profile created in one process is present when ctxt is invoked again
- [ ] Persist: Default profile persists across invocations

### Profile Forwarded in Capture Payload
- [ ] Capture: `ctxt add "text" --profile founder` sends `{"profile":"founder"}` in
  `POST /analyze` request body
- [ ] Capture: Absence of `--profile` → `profile` field omitted or empty in payload

---

## Related Stories

- [US-0001](../ingestion/US-0001-text-capture-minimal-friction.md) — `--profile` flag at capture
- [US-0016](../search/US-0016-basic-keyword-search.md) — profile applied at search time
- [US-0027](US-0027-configure-ai-provider.md) — provider config may be profile-scoped

---

## Personas

- [Maintainers](../../personas/maintainers.md)
- Platform Engineer
- [Operations](../../personas/operations.md)

---

## E2E Tests

- `test/integration/us0030_focus_profiles_test.go::TestUS0030_ProfileConfigRoundTrip`
- `test/integration/us0030_focus_profiles_test.go::TestUS0030_ProfileConfigDefaultRoundTrip`
- `test/integration/us0030_focus_profiles_test.go::TestUS0030_ObjectWithProfileIDStoredAndRetrievable`
- `test/integration/us0030_focus_profiles_test.go::TestUS0030_EmptyProfileIDIsGlobal`
- `test/integration/us0030_focus_profiles_test.go::TestUS0030_SearchFilteredByProfileReturnsCorrectObjects`
- `test/integration/us0030_focus_profiles_test.go::TestUS0030_MultipleProfilesCoexistIndependently`
