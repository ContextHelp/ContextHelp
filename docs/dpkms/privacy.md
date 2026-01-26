# Privacy

ContextHelp is designed as a **local-first, privacy-preserving knowledge engine**.
This document explains the principles, guarantees, and mechanisms that protect user data across ingestion, processing, storage, and registry interaction.

---

## Philosophy

ContextHelp is built around three foundational privacy commitments:

- **Your data stays yours.**
  All processing happens locally unless you explicitly configure external services.

- **You choose what leaves your machine.**
  Registries, agents, pipelines, and integrations operate under user-controlled policies.

- **Architecture before policy.**
  Privacy is enforced through system design, not disclaimers or promises.

---

## Local-First Processing

By default:

- All ingestion (clipboard, URLs, files, images, audio, video) occurs locally.
- All pipelines run locally unless configured otherwise.
- No automatic transmission of raw or enriched data is performed.
- Bookmarks and structured knowledge remain stored locally.

External calls (e.g., LLM APIs, registry queries) only occur when the user configures them explicitly.

---

## User Control

ContextHelp provides explicit controls for all data pathways:

- Local-only mode for strict privacy environments.
- Registry-level permissions and per-registry trust settings.
- Agent profiles that define what data an agent can see.
- Pipeline configuration to disable or sandbox external calls.
- Storage backends chosen and controlled by the user.

Users can review and modify all settings in configuration files or environment variables.

---

## Registry Interaction

Registries are **optional**, decentralized providers of:

- Taxonomies
- Weights
- Bookmark references

Registries **do not** receive user data.
They only expose structured information that ContextHelp may download based on configuration.

Key privacy properties:

- No raw input, bookmarks, or personal data are sent to registries.
- ContextHelp never uploads local knowledge to external sources by default.
- All outbound requests can be inspected, logged, or disabled.

---

## External AI Providers

If a user enables an external model provider (e.g., OpenAI, Anthropic, a self-hosted LLM endpoint):

- Only the minimal subset of data required for the configured pipeline step is sent.
- Sensitive sections can be stripped or redacted using pipeline configuration.
- Requests can be routed through a local proxy for auditability.
- Users may disable external AI entirely and run local models.

The responsibility to evaluate the privacy posture of third-party AI APIs lies with the user.

---

## Data Storage

ContextHelp stores structured knowledge locally through user-selectable backends:

- JSON files
- SQLite
- PostgreSQL
- Custom storage plugins

Storage privacy guarantees:

- No automatic synchronization or cloud replication.
- No hidden uploads or analytics.
- Users can clear, migrate, or encrypt data at any time.

Sensitive data (e.g., transcripts, screenshots) can be encrypted at rest using storage driver features.

---

## Permission Boundaries

ContextHelp maintains strict boundaries between:

- **Local data** — fully private by default
- **Registry data** — public or semi-public structured metadata
- **Agent-facing data** — filtered views dictated by agent profiles
- **Pipeline execution data** — ephemeral transformation stages

Each boundary is configurable and transparent.

---

## Ephemeral Processing

Pipeline execution stages:

- Do not persist intermediate data unless explicitly configured.
- Hold raw content in memory only as long as necessary.
- Allow optional sandboxing of external or untrusted plugins.

Users may enable memory-only modes for stricter environments.

---

## Logging & Telemetry

ContextHelp:

- Does **not** send telemetry, analytics, or usage statistics.
- Logs are stored locally and controlled by the user.
- External error reporting is disabled by default.
- Logging verbosity is configurable and suitable for secure environments.

Users can disable logs entirely or redirect them to encrypted sinks.

---

## Data Sharing

The engine **never** shares user knowledge without explicit configuration.

Any sharing mechanism (such as exporting bookmarks or generating registries) must be:

- Opt-in
- Transparent
- Under full user control

ContextHelp does not implement hidden syncing or background uploads.

---

## Deletion & Portability

Users may:

- Delete individual bookmarks
- Purge all knowledge
- Reset configuration
- Migrate storage files
- Export knowledge in structured formats
- Revoke registry access

ContextHelp follows a **data portability by design** philosophy.

---

## Threat Model Summary

ContextHelp protects against:

- Unintended external transmission of user data
- Unauthorized plugin behavior
- Accidental registry leaks
- Weak storage permissions

Users are responsible for:

- Evaluating trusted external services
- Securing their local environment
- Managing access to local agents or CLIs

---

## Future Enhancements

Planned privacy improvements:

- Optional end-to-end encrypted registry communication
- Hardened plugin sandbox model
- Metadata minimization modes
- Per-agent data access audit logs
- Recommended best-practice templates for enterprise deployments

---

## Contact

For privacy concerns, proposals, or reviews, contributors may open a discussion in the ContextHelp repository or submit a pull request to improve this document.
