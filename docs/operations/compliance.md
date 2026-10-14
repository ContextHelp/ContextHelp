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

Everything lives under the data directory (`storage.data_dir`; default
`$XDG_DATA_HOME/contexthelp`: `~/.local/share/contexthelp` on Linux,
`~/Library/Application Support/contexthelp` on macOS).

| Data | Location | Cloud egress? |
|------|----------|--------------|
| Database: objects, embeddings and vector indexes, jobs, audit log | `<data dir>/db.sqlite` (`storage.path`) | No |
| Blobs (externalized object content) | `<data dir>/blobs/` (`storage.blob.local.path`) | No |
| Pre-migration database snapshots | `<data dir>/backups/` | No |

External egress only occurs when **explicitly configured**:

- AI provider API calls (OpenAI, Anthropic) — opt-in via `providers.*` and the provider API key env vars
- Registry sync — opt-in via `registries`
- Remote database — opt-in via `storage.type: postgres`
- Remote blob store — opt-in via `storage.blob.backend: s3` (`storage.blob.s3.*`)
- Security alert webhook — opt-in via `security.alerts.webhook_url`

The `--offline` global flag refuses outbound HTTP for one invocation. Any
request a command makes through Go's default HTTP transport on the
command's context fails with `network disabled by --offline`; loopback
(`127.0.0.1`, `localhost`) stays reachable, so a local dpkms still works.
It is per-invocation only: no env var or config key turns it on.

```
ctxt --offline find "query"
```

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

### Retention

There are no retention settings and no automatic expiry: knowledge objects
are kept until deleted. Remove old objects explicitly:

```bash
dpkms housekeeping prune --before 2025-01-01 --dry-run   # preview what goes
dpkms housekeeping prune --before 2025-01-01
```

`dpkms housekeeping run` also prunes job history older than 30 days.

### Minimisation guidance

- Do not ingest documents containing bulk PII unless the object store is
  encrypted at rest (OS-level disk encryption; ctxt has no storage-encryption
  setting).
- Use `ctxt edit <id> --tags pii` to mark objects for expedited review (this replaces the object's tags; list the existing ones too).
- Scope pipeline outputs to exclude PII fields before storing enrichment results.

---

## Right to Erasure

ctxt supports GDPR Art. 17 / CCPA right-to-delete via the `ctxt delete`
command and its filters.

### Single-object deletion

```
ctxt delete --id <id> --confirm-token=<sha>
```

Deleting needs a token with `delete:objects` on a protected or public instance, and only the `admin` role holds it. `writer` and `reader` tokens exit 5 and delete nothing. A private instance (the default) authenticates nobody, so every local caller can delete. Without `--confirm-token` the command refuses and prints the token to echo back; `--dry-run` lists what it would delete.

### Bulk deletion

```
# Preview, then delete every active object
ctxt delete --all --dry-run
ctxt delete --all --confirm-token=<sha>

# Delete by tag
ctxt delete --tagged temporary --confirm-token=<sha>

# Delete by mention
ctxt delete --mention @project.archived --confirm-token=<sha>
```

A filter (`--all`, `--tagged`, `--mention`, `--type`) matches active objects only. Objects in the inbox, raw or discarded state are deleted by `--id`; find them with `ctxt list --status all`.

`ctxt delete` has no date filter. `dpkms housekeeping prune --before` deletes every object created before a date. It runs on the dpkms host, opens the database directly, and handles up to 10,000 objects per run, so repeat it until the preview finds none:

```bash
dpkms housekeeping prune --before 2025-01-01 --dry-run   # preview
dpkms housekeeping prune --before 2025-01-01
```

### Cascade effects summary

Every hard delete (`DELETE /api/v1/objects/{id}`, which `ctxt delete` calls, and `dpkms housekeeping prune`) takes the same path. On the SQLite driver:

| Affected store | Action on delete |
|----------------|-----------------|
| `objects` row (with its tags and mentions) and its full-text index entry | Hard delete |
| `attachments` | Deleted with the row |
| `embeddings` and the per-model vector indexes | Cascade delete |
| `aliases`, `object_nodes`, `resurfacing_queue` | Cascade delete |
| `edges` from or to the object (backlinks, mention links) | Deleted before the row |
| Blobs holding the object's content | Deleted when no other object refers to them and they are over an hour old; younger ones are left for `dpkms housekeeping run` |
| `object_proximity` | Not deleted |
| Entities | Kept |
| `audit_log` | Untouched; no deletion event is written |

Deleting an object does not reach copies of the database. Each schema migration first copies a SQLite database into `backups/` beside it (the last 3 are kept; `storage.migrate_snapshot` turns this off), and `dpkms backup` writes archives wherever the operator points it. Both still hold deleted objects. Rotate or delete them to complete an erasure.

### Verification

After a deletion run, confirm with:

```
ctxt list --tagged temporary --status all --format json | jq '.total'
# Should return 0 for the relevant subset
```

Object deletions are not written to the audit log. The audit log records deduplication decisions, fan-out edge creation, security alerts and `ctxt doctor` runs. To keep evidence of an erasure, save the dry-run plan (`--dry-run --format json`) alongside the request.

### Full database wipe

There is no wipe command. To destroy all data, which cannot be undone, stop dpkms and remove its data directory (`storage.data_dir`). It holds the database (objects, embeddings, jobs and the audit log), `blobs/`, `backups/` and `run/`. A `storage.path` or `storage.blob.local.path` set outside it must be removed as well.

```bash
dpkms shutdown
rm -rf "<data dir>"   # see Data Residency for the default
```

---

## SOC 2 Control Mapping

Mapping of SOC 2 Trust Service Criteria to ctxt implementation controls.
Reference: AICPA TSC 2017.

> **Pre-alpha status:** encryption at rest (ADR-019) is designed but not yet
> active. API authentication is active on protected and public instances.
> "Partial" = the control exists only for some deployments, or is designed
> but not active.

| SOC 2 Criterion | Control description | ctxt implementation | Status |
|-----------------|--------------------|--------------------|--------|
| CC1.1 — Integrity & ethics | Commitment to security values | Local-first design; no auto-telemetry | Met |
| CC2.2 — Information communication | Security events communicated | Append-only audit log; `ctxt audit export` as JSON, CEF or syslog text; security alert webhook (`security.alerts.webhook_url`) | Met |
| CC3.2 — Risk assessment | Identify and analyse risks | Threat model in `docs/security/model/threat.md` | Met |
| CC4.1 — Monitoring | Ongoing control evaluation | `govulncheck` and `nancy` on every PR and nightly; Trivy on PRs touching `go.mod` or Docker files, and nightly | Met |
| CC5.2 — Control activities | Select/develop controls | Multi-layer defence; config file permission check (`ctxt config doctor --fix` sets 0600) | Met |
| CC6.1 — Logical access (ACL) | Restrict access to authorised users | Static tokens with roles on protected/public instances; loopback-only bind on private ones | Partial |
| CC6.6 — External threats | Protect against external attacks | Loopback bind by default; Host allowlist; capture fetches refuse private addresses (`fetch.allow_private`); registry data untrusted | Met |
| CC6.7 — Encryption at rest | Protect data from unauthorised access | AES-256-GCM planned (ADR-019); not yet active | Partial |
| CC7.1 — Detection (signed artefacts) | Detect configuration tampering | Ed25519-signed config bundles; GPG release signing optional (skipped if secret absent) | Partial |
| CC7.2 — Audit log monitoring | Monitor security events | Append-only audit log; `ctxt audit list` / `ctxt audit export` | Met |
| CC7.4 — Incident response | Respond to security events | IR runbook in `docs/operations/incident-response.md` | Met |
| CC8.1 — Change management | Authorise and test changes | Conventional Commits; PR gate; `govulncheck` | Met |
| CC9.2 — Third-party risk | Vendor risk management | Dependency assessment process (see below) | Met |

Notes:

- **CC6.1 partial**: protected and public instances require static tokens with roles (`admin`, `writer`, `reader`) on REST, gRPC and MCP. A private instance (the default) binds `127.0.0.1` and authenticates nobody, so any local process holds every scope; OS file permissions and the loopback bind are its only access control. There is no plugin system (ADR-078).
- **CC6.7 partial**: Encryption not active; operators handling PII **must** use OS-level
  disk encryption (FileVault / LUKS / BitLocker) until ADR-019 ships.
- **CC7.1 partial**: GPG release signing is conditional, skipped when the `GPG_PRIVATE_KEY` secret is absent from CI. Config bundles are Ed25519-signed (`ctxt config backup`; `ctxt config restore --verify` checks the signature). Signed knowledge objects (HMAC or Ed25519, ADR-024) are designed, not active.

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
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0

# Run
govulncheck -tags fts5 ./...
```

`govulncheck` and `nancy` run in CI on every pull request and nightly. A HIGH or CRITICAL finding fails the build. Locally, `make vuln-scan` runs both. A `nancy` finding that cannot be fixed by upgrading is suppressed in `.nancy-ignore`, following the process in `.github/workflows/vuln-scan.yml`. CI does not check licences: run `go-licenses` before adding a dependency.

### Approval workflow

1. Open PR; include rationale in description.
2. CI runs `govulncheck`, `nancy` and Trivy; the author runs `go-licenses check`.
3. Reviewer confirms criteria above; approves or requests alternatives.
4. For high-impact deps (crypto, networking, parsing): second reviewer required.

### Ongoing monitoring

- `govulncheck` and `nancy` run on every PR and nightly; Trivy runs on PRs touching `go.mod` or Docker files, and nightly.
- Dependabot opens grouped update PRs weekly for Go modules and GitHub Actions (`.github/dependabot.yml`).
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
> - Breach notification runbook (60-day rule, 45 CFR 164.404)
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

**Last updated:** 2026-10-14
**Owner:** $USER
**Status:** Active — stubs pending
