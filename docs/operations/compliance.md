# Compliance

Operational compliance guide for data residency, privacy, erasure, SOC 2 controls,
and dependency governance in ctxt / dPKMS deployments.

Related: [Security Model](../security/security-model.md) |
[Security Policy](../SECURITY.md) |
[Compliance Standards](../security/model/compliance.md) |
[Controls](../security/model/controls.md)

---

## Data Residency

**Default behaviour: all data stays on the local machine.**

| Layer | Location | Cloud egress? |
|-------|----------|--------------|
| Primary store | `~/.local/share/contexthelp/db.sqlite` | No |
| Object cache | `~/.cache/contexthelp/` | No |
| Audit log | `~/.local/share/contexthelp/audit.log` | No |
| Vector index | `~/.local/share/contexthelp/vecs/` | No |
| Plugin data | `~/.local/share/contexthelp/plugins/<name>/` | No |

External egress only occurs when **explicitly configured**:

- AI provider API calls (OpenAI, Anthropic) — opt-in via `ai_providers.*`
- Registry sync — opt-in via `registries.*`
- Remote dPKMS backend (Postgres, S3) — opt-in via `storage.type`

Strict offline mode blocks all egress unconditionally:

```
ctxt config set offline true
# or
export CTXT_OFFLINE=true
```

See also: sprint 007 task 1.3 (`ctxt config --offline`) for hard-blocking implementation.

For multi-region or sovereign-cloud deployments that use a remote backend, configure
the backend to remain within the required jurisdiction and disable AI providers that
route outside it.

---

## PII Handling & Retention

### What counts as PII in ctxt objects

A ctxt object may contain PII when the ingested source includes any of:

- Personal names, email addresses, phone numbers
- IP addresses or device identifiers captured in URLs
- Health, financial, or location data embedded in documents
- Conversation transcripts imported via the import pipeline

ctxt does **not** add PII. It preserves whatever the source contained.

### Retention policy defaults

| Data class | Default TTL | Configurable? |
|------------|-------------|---------------|
| Knowledge objects | Indefinite | Yes — `retention.objects_days` |
| Audit log entries | 90 days | Yes — `retention.audit_days` |
| Pipeline run logs | 30 days | Yes — `retention.pipeline_logs_days` |
| Vector embeddings | Tied to parent object | No (cascade delete) |
| Temp import buffers | Deleted on completion | No |

Configure auto-expiry in `~/.config/contexthelp/config.yaml`:

```yaml
retention:
  objects_days: 365       # 0 = keep forever
  audit_days: 90
  pipeline_logs_days: 30
```

A background sweep runs nightly; force a run:

```
ctxt maintenance sweep --dry-run
ctxt maintenance sweep
```

### Minimisation guidance

- Do not ingest documents containing bulk PII unless the object store is
  encrypted at rest (`security.encryption.enabled: true`).
- Use `ctxt object tag <id> pii:true` to mark objects for expedited review.
- Scope pipeline outputs to exclude PII fields before storing enrichment results.

---

## Right to Erasure

ctxt supports GDPR Art. 17 / CCPA right-to-delete via the `ctxt object delete`
command and its bulk variants.

### Single-object deletion

```
ctxt object delete <id>
```

Cascade effects:
- Removes the object row from SQLite
- Removes associated vector embeddings
- Removes backlink edges from the entity graph
- Removes mention records referencing the object
- Does NOT remove audit log entries (immutable by design)

### Bulk deletion by date range

```
# Preview what would be deleted
ctxt object delete --all --before 2025-01-01 --dry-run

# Execute deletion
ctxt object delete --all --before 2025-01-01

# Scope to a namespace
ctxt object delete --all --before 2025-01-01 --namespace personal
```

`--before` accepts ISO 8601 dates (`YYYY-MM-DD`) or RFC 3339 timestamps.
The command deletes objects whose `created_at` is strictly before the given date.

### Cascade effects summary

| Affected store | Action on delete |
|----------------|-----------------|
| `objects` table | Hard delete |
| `vectors` table | Cascade delete |
| `mentions` table | Cascade delete |
| `backlinks` table | Edge removed |
| `entity_refs` table | Orphan check; stub if no other refs |
| `audit_log` | Immutable; deletion event appended |
| Plugin sidecar data | Plugin-managed; call `ctxt plugin purge <name> --object <id>` |

### Verification

After a deletion run, confirm with:

```
ctxt object list --before 2025-01-01 --count
# Should return 0
```

For regulated environments, export the audit log entry as evidence:

```
ctxt audit export --event object.delete --since 2025-01-01 --format json
```

### Full database wipe

To destroy all data (unrecoverable):

```
ctxt db drop --confirm
```

This removes the SQLite file, the vector store, and the plugin data directories.
The audit log is preserved unless `--include-audit` is passed.

---

## SOC 2 Control Mapping

Mapping of SOC 2 Trust Service Criteria to ctxt implementation controls.
Reference: AICPA TSC 2017.

> **Pre-alpha status:** auth (ADR-023) and encryption at rest (ADR-019) are designed
> but not yet shipped. "Partial" = design exists; runtime control not yet active.

| SOC 2 Criterion | Control description | ctxt implementation | Status |
|-----------------|--------------------|--------------------|--------|
| CC1.1 — Integrity & ethics | Commitment to security values | Local-first design; no auto-telemetry | Met |
| CC2.2 — Information communication | Security events communicated | Structured audit log; syslog integration | Met |
| CC3.2 — Risk assessment | Identify and analyse risks | Threat model in `docs/security/model/threat.md` | Met |
| CC4.1 — Monitoring | Ongoing control evaluation | `govulncheck` in CI; quarterly audit review | Met |
| CC5.2 — Control activities | Select/develop controls | Multi-layer defence; file perms enforced | Met |
| CC6.1 — Logical access (ACL) | Restrict access to authorised users | File permissions 0600; localhost-only bind | Partial |
| CC6.6 — External threats | Protect against external attacks | Localhost-only binding; registry untrusted by default | Met |
| CC6.7 — Encryption at rest | Protect data from unauthorised access | AES-256-GCM planned (ADR-019); not yet active | Partial |
| CC7.1 — Detection (signed artefacts) | Detect configuration tampering | GPG release signing optional (skipped if secret absent) | Partial |
| CC7.2 — Audit log monitoring | Monitor security events | Append-only audit log; `ctxt audit list`; syslog | Met |
| CC7.4 — Incident response | Respond to security events | IR procedures in `docs/SECURITY.md#incident-response` | Met |
| CC8.1 — Change management | Authorise and test changes | Conventional Commits; PR gate; `govulncheck` | Met |
| CC9.2 — Third-party risk | Vendor risk management | Dependency assessment process (see below) | Met |

Notes:

- **CC6.1 partial**: JWT auth and plugin ACL planned (ADR-023, ADR-027); not yet enforced.
  Current protection: localhost-only bind + OS file permissions.
- **CC6.7 partial**: Encryption not active; operators handling PII **must** use OS-level
  disk encryption (FileVault / LUKS / BitLocker) until ADR-019 ships.
- **CC7.1 partial**: GPG release signing is conditional — skipped when `GPG_PRIVATE_KEY`
  secret is absent from CI. HMAC bundle verification is also planned.

Full compliance evidence checklist for auditors: `docs/security/model/compliance.md`.

---

## Third-Party Dependency Assessment

Process for evaluating new Go module dependencies before merging.

### Evaluation criteria

1. **Licence compatibility** — must be MIT, Apache-2.0, BSD-2, BSD-3, ISC, or MPL-2.0.
   GPL/AGPL dependencies require explicit sign-off from the maintainer.
2. **Maintenance status** — last commit within 12 months; open CVEs triaged.
3. **Minimal footprint** — prefer std-lib or existing transitive deps over new roots.
4. **Supply-chain hygiene** — check that the module path matches the canonical VCS
   path; no typosquats.

### Licence check

```
# Install
go install github.com/google/go-licenses@latest

# Run from repo root
go-licenses check ./... --allowed_licenses=MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MPL-2.0
```

### CVE / vulnerability scan

```
# Install
go install golang.org/x/vuln/cmd/govulncheck@latest

# Run
govulncheck ./...
```

`govulncheck` runs automatically in CI (`just check`). PRs introducing new
deps with known CVEs are blocked until the vulnerability is fixed or a waiver
is recorded in `docs/decisions/`.

### Approval workflow

1. Open PR; include rationale in description.
2. CI runs `govulncheck` and `go-licenses check`.
3. Reviewer confirms criteria above; approves or requests alternatives.
4. For high-impact deps (crypto, networking, parsing): second reviewer required.

### Ongoing monitoring

- `govulncheck` runs on every PR and nightly via scheduled CI.
- `dependabot` (or equivalent) opens PRs for patch/minor updates weekly.
- Quarterly review of all direct deps for maintenance status.

---

## Compliance Stubs

The following sections are placeholders for future regulatory work.

### HIPAA (stub)

> Not yet applicable. Activate when ctxt is used to process Protected Health
> Information (PHI) as defined under 45 CFR Part 160/164.
>
> Required additions:
> - Business Associate Agreement (BAA) template
> - PHI data-flow mapping
> - Minimum Necessary Standard implementation
> - Breach notification runbook (72-hour rule)
> - Audit controls per 164.312(b)

### CCPA (stub)

> Partially addressed by the Right to Erasure section above.
>
> Required additions when operating a covered business:
> - "Do Not Sell" opt-out mechanism
> - Consumer request intake process (45-day response SLA)
> - Data inventory covering categories of personal information collected
> - Third-party disclosure register

### ISO 27001 (stub)

> Security controls are aligned to ISO 27001 Annex A (see
> `docs/security/model/compliance.md`). Full certification requires:
> - Formal ISMS scope statement
> - Statement of Applicability (SoA)
> - Risk treatment plan
> - Management review cadence
> - Internal audit programme

### FedRAMP (stub)

> Not planned. Revisit if ctxt is deployed in US federal environments.

---

**Last updated:** 2026-03-25
**Owner:** $USER
**Status:** Active — stubs pending
