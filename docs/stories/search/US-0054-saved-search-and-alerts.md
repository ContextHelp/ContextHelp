# US-0054: Saved Search and Alerts

**System Types:** ctxt
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a knowledge worker, I want to save a search with an alert trigger so that I am automatically notified when new content matches my query, without having to re-run it manually.

---

## Context

Recurring searches are common: "any new security decisions," "new content about the auth service." Manually re-running the same query is repetitive and unreliable. Saved searches persist a query together with its profile, strategies, and alert configuration. When new objects are ingested that match the saved query, the server fires a notification via the configured channel. Users can also re-run saved searches on demand. Saved searches are identified by a human-readable name, and support update and delete operations.

---

## Acceptance Criteria

- [ ] User can save a search: `ctxt search save "query" --name my-alert`
- [ ] User can attach an alert trigger: `--alert-on new-results`; server fires a notification when new matching objects are ingested
- [ ] User can specify a notification channel: `--notify email`
- [ ] User can attach a profile to a saved search: `--profile engineering`; re-runs use the stored profile
- [ ] Saved search record is created in the DB with all fields (`query`, `name`, `profile`, `alert_trigger`, `notify`)
- [ ] Saved search is listable via `ctxt search list` or `GET /searches`
- [ ] `ctxt search run my-alert` re-executes the saved query with stored params; results match a manual re-run
- [ ] Alert fires when a new matching object is ingested; a notification record is created in the DB
- [ ] `ctxt search delete my-alert` removes the DB row; subsequent list confirms absence
- [ ] `ctxt search update my-alert --alert-on none` persists the change server-side
- [ ] Duplicate name returns 409 conflict error
- [ ] Unknown name in run/delete/update returns 404
- [ ] Alert dispatch failure marks the alert failed in the DB and retries on next check

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload (Save)

- [ ] `ctxt search save "query" --name my-alert` sends `query`, `name=my-alert` in request
      payload to save endpoint; server receives both fields
- [ ] `ctxt search save "query" --name my-alert --alert-on new-results` sends `alert_trigger=
      new-results` in payload; server receives and stores it
- [ ] `ctxt search save "query" --name my-alert --profile engineering` sends `profile=
      engineering` in payload; server receives and stores it with the saved search
- [ ] `ctxt search save "query" --name my-alert --notify email` sends `notify=email` in payload;
      server receives notification channel

### Server-Side Receipt and Storage

- [ ] Server receives save request; saved search record created in DB with all fields (`query`,
      `name`, `profile`, `alert_trigger`, `notify`)
- [ ] Saved search retrievable via `ctxt search list` or `GET /searches`; DB row present
- [ ] `alert_trigger=new-results`: server checks saved search on new ingestion; if new matching
      results appear, alert dispatched (notification record created)
- [ ] `alert_trigger` and `notify` stored with saved search; verifiable via `ctxt search show
      <name>` or GET endpoint

### Flags Coverage

- [ ] `--name <n>` — present in save payload; server stores as human-readable identifier
- [ ] `--alert-on <trigger>` — `alert_trigger` present in payload; server stores trigger type
- [ ] `--profile <name>` — present in payload; server stores with saved search; re-run uses
      same profile
- [ ] `--notify <channel>` — present in payload; server stores notification channel
- [ ] Duplicate name → server returns 409 conflict error

### Saved Search Execution

- [ ] `ctxt search run my-alert` re-executes saved query with stored params; results match
      manual re-run of same query+profile
- [ ] Saved search re-run sends same payload as original save (query, profile, strategies);
      server receives full param set
- [ ] Alert fires when new matching object ingested; notification record created in DB with
      saved search ID

### Delete and Update

- [ ] `ctxt search delete my-alert` sends delete request; DB row removed; subsequent list
      confirms absence
- [ ] `ctxt search update my-alert --alert-on none` sends update payload with `alert_trigger=
      none`; server receives and persists change

### Error Handling

- [ ] Unknown saved search name in `run`/`delete`/`update` → 404 with name-not-found error
- [ ] Alert dispatch failure (provider error) → alert marked failed in DB; next check retries

### Interface Parity

- [ ] Save + run via CLI and REST (`POST /searches`, `POST /searches/{name}/run`) → identical
      results

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (NLQ queries can be saved)
- [US-0017](./US-0017-structured-rsql-query.md) — Structured RSQL Query (RSQL queries can be saved)
- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (profile stored with saved search)
- [US-0055](./US-0055-search-history-and-recommendations.md) — Search History and Recommendations (history records include saved search runs)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

> Not yet implemented.
