# Skeleton 10 Goal: "Federation & Commerce"

## Package Focus

**Primary Package:** dPKMS (70%) + ctxt (30%)

Advanced registry features — paid access, metering, thin sync, and just-in-time content pull — are predominantly substrate concerns. ctxt contributes CLI surfacing and entitlement-aware pipeline behaviour.

**Package Breakdown:**
- **dPKMS:** Paid registry auth, entitlement enforcement, metering hooks, thin sync protocol, JIT content pull, quota storage
- **ctxt:** CLI commands for registry entitlements, entitlement-gated pipeline steps, billing status display

---

By the end of this skeleton:

1. **Paid Registries:** Users can subscribe to paid registries with token-based auth; entitlement checks gate both metadata sync and full content access.
2. **Thin Sync:** Registries can operate in index-only mode — the local system holds slugs and metadata but fetches full content on-demand, reducing storage costs.
3. **Just-in-Time Pull:** Full entity or object content is fetched only when a user or agent actually touches it (lazy resolution), with policy-gated access.
4. **Metering:** Access to paid content is tracked locally; quota exhaustion is surfaced gracefully with actionable CLI output.

---

## 1. dPKMS Registry Team (The Gatekeeper)

Focus: Auth tokens, entitlement checks, quota tracking, and graceful access denial.
Why: The roadmap requires paid registries to be a first-class mode — auth and entitlements must be enforced in the substrate, not bolted on later.

### Task 1.1: Registry Auth Token Storage

- Extend registry config to support `auth` block:
  ```yaml
  registries:
    - url: https://registry.example.com
      name: example-paid
      auth:
        type: bearer       # or: api_key, oauth2
        token: ${CTXT_EXAMPLE_TOKEN}
  ```
- Store tokens in OS keychain (macOS: Keychain, Linux: `secret-service` via `99designs/keyring`).
- `ctxt registry login <name>` prompts for token and stores it securely.
- `ctxt registry logout <name>` removes the stored token.
- Tokens are **never** written to the YAML config file directly; the `token:` key is only an env-var reference or keychain lookup hint.

### Task 1.2: Entitlement Check on Sync

- Before syncing a registry, call a `/entitlements` endpoint (if declared in the registry manifest).
- Entitlement response shape:
  ```json
  { "plan": "pro", "namespaces": ["ai.*", "devops.*"], "expires_at": "2027-01-01T00:00:00Z" }
  ```
- Store entitlement record in `registry_entitlements` table:
  ```sql
  CREATE TABLE registry_entitlements (
    registry_name  TEXT PRIMARY KEY,
    plan           TEXT NOT NULL,
    namespaces     TEXT NOT NULL,  -- JSON array
    expires_at     DATETIME,
    fetched_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  ```
- During entity resolution: if a namespace is restricted and the user lacks entitlement, return `ErrEntitlementRequired` with registry name and upgrade URL.
- CLI: `ctxt registry entitlements` — lists active entitlements per registry.

### Task 1.3: Metering Hooks

- Track access events for paid content in a `metering_events` table:
  ```sql
  CREATE TABLE metering_events (
    id             TEXT PRIMARY KEY,
    registry_name  TEXT NOT NULL,
    event_type     TEXT NOT NULL,   -- "entity_resolve", "content_pull", "taxonomy_sync"
    namespace      TEXT,
    count          INTEGER NOT NULL DEFAULT 1,
    occurred_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
  ```
- Aggregate and report with `ctxt registry usage` — shows counts per registry per event type for the current billing period.
- When quota is near exhaustion (≥80% of declared limit), emit a `WARN` on the next relevant operation.
- When quota is exhausted, return `ErrQuotaExhausted` and skip the operation (no silent fallback).

---

## 2. dPKMS Storage Team (The Thin Client)

Focus: Thin sync protocol — store index and metadata without full content; pull full content on demand.
Why: Users with many registries should not be forced to replicate entire entity corpora locally.

### Task 2.1: Thin Sync Mode

- Add `sync_mode` to registry config:
  ```yaml
  registries:
    - name: large-corpus
      sync_mode: thin    # default: full
  ```
- In `thin` mode, registry sync fetches:
  - Entity index (IDs, slugs, namespaces, titles, versions) — no full definitions.
  - Taxonomy structure — no detailed descriptions.
- Store thin records with a `content_status` column:
  ```sql
  ALTER TABLE entities ADD COLUMN content_status TEXT NOT NULL DEFAULT 'full';
  -- values: 'full' | 'thin' | 'pending_pull'
  ```
- Thin entities are valid for resolution (slug lookup) and display (title + namespace) but cannot serve full definitions until pulled.

### Task 2.2: JIT Content Pull (Lazy Resolution)

- When code requests the full definition of a `thin` entity, trigger a JIT pull:
  1. Check entitlement for the namespace.
  2. Fetch from the registry's `/entities/<slug>` endpoint.
  3. Store full definition; update `content_status = 'full'`.
  4. Record a `content_pull` metering event.
- JIT pulls are synchronous for CLI interactions and async (background job) for pipeline enrichment.
- Config:
  ```yaml
  registries:
    - name: large-corpus
      jit_pull:
        enabled: true
        max_per_minute: 60   # rate limit guard
        cache_ttl: 24h
  ```
- CLI: `ctxt entity pull <namespace.slug>` — forces an immediate JIT pull.

### Task 2.3: Export Gating

- `ctxt export` must respect entitlements:
  - If an object contains entities from a paid registry with `export_gated: true`, omit the full entity definitions from the export bundle.
  - Include a stub with `{ "slug": "...", "registry": "...", "access": "restricted" }` instead.
- Document this in the export format spec.

---

## 3. ctxt CLI Team (The Accountant)

Focus: Surface billing, entitlements, and quota in a user-friendly way.

### Task 3.1: `ctxt registry login` / `logout`

- Interactive flow:
  ```
  $ ctxt registry login example-paid
  Enter token: ****
  ✓ Token stored securely in keychain.
  ✓ Entitlements fetched: plan=pro, expires=2027-01-01.
  ```
- Non-interactive: `ctxt registry login example-paid --token $TOKEN`.

### Task 3.2: `ctxt registry usage` Dashboard

- Tabular output:
  ```
  Registry        Event Type       Count   Limit   Resets
  ──────────────────────────────────────────────────────
  example-paid    entity_resolve   423     1000    2026-04-01
  example-paid    content_pull     81      200     2026-04-01
  ```
- `--json` flag for programmatic access.

### Task 3.3: Entitlement-Aware Pipeline Steps

- Wrap embedding and entity-resolution pipeline steps so that if a `ErrEntitlementRequired` is returned:
  - Log the event as a job step warning.
  - Continue pipeline without the restricted entity (use a local stub instead).
  - Do not fail the entire job.

---

## The Integration Check (The Demo)

Scenario: "Paid Registry + Thin Sync + JIT Pull"

1. User registers a paid registry:
   ```
   ctxt registry add https://registry.example.com --name example-paid
   ctxt registry login example-paid
   ```
2. Sync runs in `thin` mode:
   ```
   ctxt registry sync example-paid
   ```
   - 5000 entity stubs synced in < 5s (no full definitions fetched).

3. User ingests a document referencing `@ai.transformer-architecture`:
   - Pipeline triggers JIT pull for `ai.transformer-architecture`.
   - Metering event recorded.
   - Full definition used in enrichment.

4. User checks usage:
   ```
   ctxt registry usage
   ```
   - Shows 1 `content_pull` event.

5. User exports their knowledge base:
   ```
   ctxt export --out bundle.zip
   ```
   - `ai.*` entities present as stubs (export-gated).

---

## Risks to Watch For

- **Keychain availability on headless/CI environments:** Provide `--token` CLI flag as fallback; document env-var pattern.
- **JIT pull latency in hot paths:** Cap pull concurrency; async for background jobs.
- **Metering drift:** Local counts may diverge from server-side counts. Document this — local metering is advisory, not billing-authoritative.
- **Token expiry mid-session:** Detect `401` responses and surface `ctxt registry login` prompt.
- **Thin→full migration:** When user switches `sync_mode` from `thin` to `full`, a backfill job must pull all missing definitions without duplicate metering.

---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index
