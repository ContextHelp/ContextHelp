# Skeleton 8 Goal: "Safe Automation and Trusted Semantic Identity"

## Package Focus

**Primary Package:** dPKMS (70%) + ctxt (30%)

Trust and safety are primarily infrastructure concerns: permissions, sandboxing, graph integrity.

**Package Breakdown:**
- **dPKMS:** Plugin manifest enforcement, capability layer, entity integrity guard, graph safety validator
- **ctxt:** Watcher interface, clipboard/directory watchers, semantic watcher hooks

---

By the end of this skeleton:

1. **Security:** Plugins execute under a strict permission and capability model, enforced via `manifest.yaml` and runtime validation.
2. **Automation:** The engine supports background watchers (clipboard, directory, refresh-driven) that enqueue ingestion safely without core modifications.
3. **Connectivity:** Search output and knowledge object introspection support entity-aware related-item browsing through the Knowledge Graph.
4. **Semantic Identity:** Mentions, entities, aliases, and provenance are first-class trust primitives.
5. **Graph Safety:** The Knowledge Graph is protected against spoofing, namespace attacks, and semantic drift.

---

## 1. dPKMS Infrastructure Team (The Sheriff)

Focus: Plugin sandboxing, permissions, provenance, and graph safety.

Why: The revised ADR-012 requires plugins to operate independently of core while respecting explicit permissions, semantic integrity, and trust boundaries.

### Task 1.1: Plugin Manifest (`manifest.yaml`)

Define the manifest specification required by ADR-012.

Plugins must declare:

```
permissions:
  - network
  - filesystem
  - clipboard
  - refresh
  - notifications
  - entity.write
  - entity.alias
```

Rules:

- `entity.write` and `entity.alias` are privileged.
- Permissions must be explicitly granted before plugin activation.
- During `dpkms serve` startup:
  - Detect permission changes.
  - Halt execution and request user/admin confirmation.

### Task 1.2: Capability Enforcement Layer

Wrap all plugin-facing capabilities (network, FS, watchers, entity mutation).

Behavior:

- `FetchURL` requires `network`.
- `ReadFile` / `WriteFile` require `filesystem`.
- `WatchClipboard` requires `clipboard`.
- `refresh.Trigger()` requires `refresh`.
- `notifications.Create()` requires `notifications`.
- `PublishEntity`, `UpdateEntity`, or `DefineAlias` require `entity.write` or `entity.alias`.

Return `PermissionDenied` for unauthorized actions.

### Task 1.3: Secret Redaction

Extend the logging system to ensure:

- All secrets (LLM keys, embedding keys, registry tokens, Authorization headers) are scrubbed from:
  - `ctxt jobs logs`
  - stderr/stdout
  - plugin-produced logs

Secrets must never appear in persisted logs.

### Task 1.4: Entity Integrity Guard

Extend the entity acceptance rules when registries or plugins propose updates.

Validate:

- Canonical namespace mapping.
- Version increments must be linear and expected.
- Required fields must exist (`id`, `title`, `namespace`, `version`).
- No entity may overwrite or shadow a namespace not owned by the source registry.

Rejected changes must be logged with provenance.

### Task 1.5: Graph Safety Validator

Before linking or updating graph edges:

- Validate mention syntax.
- Reject malformed or spoofed namespaces.
- Prevent backlinks from referencing nonexistent or unauthorized entities.
- Enforce canonical slug formatting and lowercase (registry-defined rules).

---

## 2. ctxt Ingestion Team (The Watcher)

Focus: Background ingestion, semantic extraction, and safe automation.

Why: With mentions and entities as first-class semantic primitives, all automated ingestion must produce consistent and trustworthy semantic identity.

### Task 2.1: The `Watcher` Interface

Define a plugin-extensible abstraction:

- Emits raw content into the job queue.
- Annotates jobs with `origin=watcher`.
- Prevents re-ingestion loops via job metadata and hashing.

Allow watchers to be provided by plugins without core changes.

### Task 2.2: Clipboard Watcher (Reference)

- Poll clipboard contents.
- If content changes and matches ingestion heuristics (URL, text, code block), enqueue a job.
- Configurable via:

```
watchers:
  clipboard:
    enabled: true
    auto_ingest: false
```

- Pass raw content into pipelines for mention extraction and entity resolution.

### Task 2.3: Directory Watcher

- Monitor a configured directory using `fsnotify`.
- On file drop (PDF, markdown, HTML), enqueue ingestion.
- Integrate full pipeline logic:
  - OCR if needed
  - mention extraction
  - entity resolution

### Task 2.4: Semantic Watcher Hooks

Allow watchers to perform lightweight pre-semantic checks:

- Detect inline mentions in filenames or file metadata.
- Attach early-guess mention metadata to the job.
- Use this for optimization only; pipelines remain authoritative.

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Graph)

Focus: Entity-aware retrieval, related content, and graph integrity.

Why: Mentions and entities are now the primary semantic identifiers; retrieval must leverage graph connectivity.

### Task 3.1: Pipeline Mention and Entity Integration

Ensure every pipeline capable of producing textual or structured content performs:

- Mention extraction with `@namespace.slug` rules.
- Entity resolution using:
  - local registry
  - external registries
  - placeholder creation for unresolved entities
- Storage of:
  - knowledge object mentions
  - entity backlinks
  - provenance metadata

### Task 3.2: `related:` Query Operator

Implement the operator:

```
related:<bookmark-id>
```

Execution:

1. Identify entities referenced by the target bookmark.
2. Return other knowledge objects with overlapping entities.
3. Rank by shared-entity weight.

### Task 3.3: "See Also" in CLI Output

Update:

```
ch show <id>
```

Display:

- entities referenced by the bookmark
- top related knowledge objects (via graph)
- provenance of entities

### Task 3.4: Entity-Aware Querying

Add filters:

- `mention:ui.best-practice`
- `mention:stripe.api.*`
- `entity:frontend.react`
- wildcard entity namespace queries

Support mention-to-entity resolution during query parsing.

### Task 3.5: Graph Integrity Corridor

Validate edges before insertion:

- No entity defined in a namespace the registry does not control.
- Mentions referencing nonexistent namespaces are rejected.
- Placeholders must remain isolated unless explicitly resolved later.
- Prevent plugins from injecting malformed backlinks.

---

## 4. dPKMS Registry Team (The Librarian)

Focus: Registry discovery, entity provenance, trust levels, and safe namespace management.

Why: Registries now carry both taxonomies and canonical entities; trust must be explicit and user-controllable.

### Task 4.1: The Registry Index (Reference Registry)

Define a root registry:

- Lists known registries.
- Exposes metadata:
  - namespaces
  - trust level
  - entity counts
  - version timestamps

Used for discovery, not automatic subscription.

### Task 4.2: `ctxt registry search <topic>`

Allow users to discover registries by:

- namespace prefix
- tags
- subject matter

Provide registry metadata in the response.

### Task 4.3: Registry Trust Flags

Configurable per registry:

```
trusted: true/false
```

Behavior:

- Trusted registries may define entities and aliases.
- Untrusted registries:
  - may be used for lookup but not mutation
  - cannot define aliases
  - cannot override local entities

### Task 4.4: Entity Provenance Tracking

Store and surface for every entity:

- origin registry
- namespace ownership
- version
- trust level
- aliases and redirect history

Expose via:

```
ch show entity <slug>
```

---

## The Integration Check (The Demo)

Scenario: “Automated Researcher with Trusted Semantic Identity”

1. User enables clipboard watching.
2. User installs a “PDF Summarizer” plugin.
3. Plugin requests `filesystem` and `entity.write`.
4. User approves `filesystem` but denies `entity.write`.
5. User copies a URL to a technical article.
6. Clipboard Watcher detects new content and enqueues ingestion.
7. Pipeline extracts mentions such as `@react.server-components`.
8. Resolver matches entity to a trusted registry.
9. User runs:

```
ch show <new-bookmark-id>
```

10. Output includes:

- extracted mentions
- resolved entities with provenance
- “See Also” powered by shared-entity relationships

---

## Risks to Watch For

### Clipboard Loop

Automation may re-ingest content that ContextHelp itself writes.

Mitigation: store hash of last clipboard value.

### Entity Explosion

LLM noise or weak heuristics may generate excessive placeholder entities.

Mitigation: namespaced validators, stop-lists, frequency thresholds.

### Namespace Spoofing

Malicious plugins or registries may attempt to define entities in reserved namespaces.

Mitigation: enforce namespace ownership and provenance constraints.

### OS Permission Requirements

Clipboard and directory watchers may require OS-level permissions.

Mitigation: document per-platform setup and fallback modes.

### Graph Poisoning

Malicious content may attempt to flood graph with bogus or adversarial links.

Mitigation: entity validator, edge gating, alias verification, and permission-controlled mutation.
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
