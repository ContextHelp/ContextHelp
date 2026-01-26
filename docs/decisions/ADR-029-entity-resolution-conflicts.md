# ADR-029 – Entity Resolution and Conflict Handling

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS + ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Mentions (`@entity.slug`) form the semantic backbone of the knowledge graph by creating canonical, stable references to entities. These entities may be defined in:

1. **Local registry** — User-created entities
2. **Remote registries** — Community or organizational knowledge registries
3. **Multiple remote registries** — Same entity defined differently across sources

**Entity Resolution Challenges:**
- Same entity referenced with different slugs (aliases)
- Same slug defined differently across registries (conflicts)
- Entity renamed or deprecated in registry (version evolution)
- Circular alias chains (A → B → C → A)
- Local override vs. registry authority
- Namespace collisions across registries

**User Impact:**
- Broken references when entity definitions change
- Ambiguous entities create incorrect graph edges
- Lost context when entities merge or split
- Trust issues when conflicting definitions exist

The system must resolve entity references deterministically while respecting user sovereignty and enabling federation without breaking the knowledge graph.

---

## Decision Drivers

- **Identity Stability:** Entity references must remain stable across registry updates
- **Determinism:** Same configuration + inputs = same entity resolution
- **User Sovereignty:** Local definitions always take precedence
- **Federation:** Multiple registries coexist without central authority
- **Graceful Degradation:** Unresolved entities don't break the system
- **Transparency:** Users understand where entity definitions come from

---

## Considered Options

### Option 1: First-Registry-Wins Resolution

**Approach:**
- Entity resolution stops at first match in registry priority order
- No conflict detection or merging
- Simple, fast

**Pros:**
- Simple implementation
- Predictable resolution order

**Cons:**
- Silent conflicts (user unaware of alternatives)
- No way to merge compatible definitions
- Priority order becomes critical, hard to manage
- Changing registry order breaks resolution

### Option 2: Conflict Detection with Manual Resolution

**Approach:**
- Scan all registries for entity definitions
- Detect conflicts (same slug, different metadata)
- Present conflicts to user for manual resolution
- Store user's choice as local override

**Pros:**
- User controls conflict resolution
- Transparent about conflicts
- Can merge compatible definitions

**Cons:**
- Interrupts ingestion workflow
- Requires UI for conflict resolution
- Doesn't scale to many entities

### Option 3: Layered Resolution with Local Precedence

**Approach:**
- Resolution layers: Local → Remote Registries → Unresolved Local Placeholder
- Alias normalization applied before lookup
- Conflicts resolved by precedence rules
- Local overrides stored explicitly
- Unresolved entities tracked for later resolution

**Pros:**
- Deterministic resolution path
- Local sovereignty preserved
- Aliases handled automatically
- Federation-friendly (registries don't need coordination)
- Graceful degradation (unresolved = local placeholder)

**Cons:**
- More complex resolution logic
- Must track local overrides separately
- Alias chains require cycle detection

---

## Decision Outcome

**Chosen Option:** Option 3 — Layered Resolution with Local Precedence

**Rationale:**

This approach aligns with all architectural principles:

- **Local-First:** Local entities always take precedence
- **Federated:** Registries coexist without central coordination
- **Deterministic:** Resolution follows clear precedence rules
- **Graceful:** Unresolved entities don't break the system
- **Transparent:** Provenance shows where entity came from
- **User Sovereignty:** Users can override any registry definition

---

## Implementation Details

### Entity Resolution Algorithm

**Resolution Steps:**

```
1. Normalize mention slug:
   - Lowercase
   - Trim whitespace
   - Validate syntax

2. Check Local Overrides:
   - If explicit local override exists → use it
   - (User has explicitly chosen definition)

3. Check Local Entities:
   - If local entity exists → use it
   - (User-created entities have highest priority)

4. Check Remote Registries (in priority order):
   - For each registry in user's priority list:
     a. Lookup slug in registry
     b. If found, check for aliases
     c. If alias chain exists, follow it (max depth = 5)
     d. Return first resolved entity

5. Create Unresolved Local Placeholder:
   - If no entity found in any source
   - Create local placeholder entity with slug
   - Mark as "unresolved" for user review
   - Still usable for graph edges

6. Record Provenance:
   - Store resolution source (local, registry name)
   - Store resolution timestamp
   - Store alias chain if applicable
```

**Example Resolution:**

```go
type EntityResolution struct {
    Slug          string    // Original mention slug
    ResolvedSlug  string    // Final canonical slug (after aliases)
    Title         string
    Description   string
    Labels        map[string]string // Translations
    Source        string    // "local", "registry:default", "unresolved"
    AliasChain    []string  // ["old-slug", "current-slug"]
    Conflicts     []EntityConflict
    ResolvedAt    time.Time
}

type EntityConflict struct {
    RegistryName  string
    ConflictType  string // "metadata", "alias", "definition"
    Details       string
}
```

### Alias Handling

**Alias Normalization:**

Registry defines aliases in entity metadata:

```json
{
  "entities": {
    "ui.best-practice": {
      "title": "UI Best Practice",
      "aliases": ["ux.best-practice", "ui-best-practice"],
      "deprecated_aliases": ["ui.pattern"]
    }
  }
}
```

**Resolution with Aliases:**

```
Mention: @ux.best-practice
1. Not found in local entities
2. Check registry "default"
   - Not found as primary slug
   - Found as alias of "ui.best-practice"
   - Follow alias → "ui.best-practice"
3. Return resolved entity "ui.best-practice"
```

**Alias Chain Detection:**

```go
func resolveWithAliases(slug string, registry Registry, maxDepth int) (Entity, error) {
    visited := make(map[string]bool)
    current := slug
    depth := 0

    for depth < maxDepth {
        if visited[current] {
            return Entity{}, ErrCircularAlias
        }
        visited[current] = true

        entity := registry.LookupEntity(current)
        if entity != nil && entity.AliasTarget == "" {
            return entity, nil // Found canonical entity
        }

        if entity != nil && entity.AliasTarget != "" {
            current = entity.AliasTarget
            depth++
            continue
        }

        return Entity{}, ErrNotFound
    }

    return Entity{}, ErrMaxDepthExceeded
}
```

### Conflict Detection

**Conflict Types:**

1. **Metadata Conflict:**
   - Same slug, different title/description across registries
   - Resolution: Use first registry in priority order
   - User notified via conflict list

2. **Alias Conflict:**
   - Same alias points to different entities in different registries
   - Resolution: Use first registry in priority order
   - User notified via conflict list

3. **Namespace Conflict:**
   - Entity exists in multiple registries with incompatible definitions
   - Resolution: Use registry priority order
   - User notified via conflict list

**Conflict Storage:**

```sql
CREATE TABLE entity_conflicts (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    conflict_type TEXT NOT NULL,
    registries TEXT NOT NULL, -- JSON array
    detected_at TIMESTAMP,
    resolved_at TIMESTAMP,
    resolution TEXT, -- "use_local", "use_registry:name", "ignore"
    INDEX idx_slug (slug)
);
```

### Local Overrides

**Override Schema:**

```sql
CREATE TABLE entity_overrides (
    slug TEXT PRIMARY KEY,
    override_type TEXT NOT NULL, -- "replace", "merge", "ignore"
    entity_data TEXT NOT NULL, -- JSON
    reason TEXT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);
```

**Override Types:**

1. **Replace:** Local definition completely replaces registry definition
2. **Merge:** Local definition extends registry definition (additional fields)
3. **Ignore:** Ignore registry definition, treat as unresolved

**Example Override:**

```bash
# User creates local override
ctxt entity create @ui.best-practice \
  --title "My UI Guidelines" \
  --override replace \
  --reason "Company-specific definition"
```

### Registry Priority Configuration

**User Configuration:**

```yaml
registries:
  priority:
    - local             # Always checked first (implicit)
    - company-internal  # Company registry
    - community-default # Community registry
    - experimental      # Experimental registry (lowest priority)

  conflict_strategy: first_match  # Options: first_match, prompt, ignore
```

**Priority Resolution:**

```
Mention: @stripe.api
1. Local entities: not found
2. company-internal: found → use this
3. community-default: also found → conflict detected, use company-internal
4. Return company-internal definition + log conflict
```

### Unresolved Entity Handling

**Placeholder Entity:**

When entity cannot be resolved, create local placeholder:

```json
{
  "slug": "unknown.api.endpoint",
  "title": "unknown.api.endpoint",
  "description": "Unresolved entity - no definition found",
  "status": "unresolved",
  "source": "placeholder",
  "created_at": "2026-01-26T10:00:00Z"
}
```

**Placeholder Lifecycle:**

1. **Created:** When mention extracted but no entity found
2. **Usable:** Can still create graph edges
3. **Discoverable:** Shows in entity list with "unresolved" status
4. **Resolvable:** User can define it or sync from registry later
5. **Auto-Resolved:** If registry sync provides definition, placeholder upgraded

**CLI Support:**

```bash
# List unresolved entities
ctxt entity list --status unresolved

# Resolve placeholder
ctxt entity resolve @unknown.api.endpoint \
  --title "API Endpoint" \
  --description "..."

# Or pull from registry
ctxt registry sync --entities-only
```

### Provenance Tracking

**Entity Provenance:**

Every resolved entity includes provenance metadata:

```json
{
  "slug": "ui.best-practice",
  "title": "UI Best Practice",
  "source": "registry:community-default",
  "alias_chain": ["ux.best-practice", "ui.best-practice"],
  "resolved_at": "2026-01-26T10:00:00Z",
  "conflicts": [
    {
      "registry": "experimental",
      "conflict_type": "metadata",
      "details": "Different title: 'UI Patterns'"
    }
  ]
}
```

**Provenance in Knowledge Objects:**

Objects store entity resolution provenance:

```json
{
  "mentions": ["ui.best-practice"],
  "enrichment_metadata": {
    "entity_resolution": {
      "ui.best-practice": {
        "source": "registry:community-default",
        "resolved_at": "2026-01-26T10:00:00Z"
      }
    }
  }
}
```

### Graph Consistency

**Edge Integrity:**

Graph edges reference entity slugs:

```
Object[id] --mentions--> Entity[slug]
Entity[slug] --backlinks--> Object[id]
```

**Alias Updates:**

When alias chain changes (registry update):

1. **Detect:** Registry sync finds new alias
2. **Validate:** Check if mentions need updating
3. **Prompt:** Ask user to update mentions (optional)
4. **Update:** Rewrite mentions if user approves
5. **Audit:** Log mention rewrites

**Example:**

```
Registry update:
  "ui.pattern" → alias of "ui.best-practice"

Objects with mention "@ui.pattern":
  - Option 1: Keep as-is (alias resolution handles it)
  - Option 2: Rewrite to "@ui.best-practice" (canonical form)

User choice stored in config:
  alias_rewrite_strategy: keep_original | canonicalize
```

---

## Consequences

### Positive

- **Deterministic Resolution:** Same inputs produce same entity mappings
- **Local Sovereignty:** Users control entity definitions
- **Federation-Friendly:** Registries coexist without coordination
- **Graceful Degradation:** Unresolved entities don't break system
- **Conflict Awareness:** Users notified of conflicts
- **Provenance Transparency:** Clear source tracking
- **Alias Support:** Flexible entity naming across registries

### Negative

- **Complexity:** Multi-layer resolution logic is more complex
- **Conflict Management:** Users must handle conflicts explicitly
- **Storage Overhead:** Tracking overrides, conflicts, provenance adds data
- **Registry Priority:** Users must understand priority implications

### Neutral

- **Alias Chains:** Limited to max depth 5 (prevents infinite loops)
- **Placeholder Entities:** Visible to users, may need cleanup

---

## Compliance

**Must Have for v1.0:**
- [ ] Layered resolution algorithm (local → registries → placeholder)
- [ ] Alias normalization and chain resolution
- [ ] Local override storage and precedence
- [ ] Registry priority configuration
- [ ] Unresolved entity placeholders
- [ ] Basic conflict detection and logging

**Should Have for v1.0:**
- [ ] Conflict resolution UI/CLI commands
- [ ] Entity provenance in knowledge objects
- [ ] Circular alias detection
- [ ] Mention rewrite on alias changes (optional)

**May Have for v2.0:**
- [ ] Automatic conflict resolution strategies
- [ ] Entity merge proposals (compatible definitions)
- [ ] Historical entity version tracking
- [ ] Alias deprecation warnings

---

## Notes

**Design References:**
- `docs/dpkms/mentions.md:108-134` (Entity resolution via registries)
- `docs/dpkms/knowledge-graph.md:107-112` (Identity stability)
- `docs/ctxt/schema-taxonomy.md:193-205` (Namespace handling)

**Related User Stories:**
- User syncs from community registry, conflict with local entity
- Entity renamed in registry, mentions still resolve via alias
- User overrides registry definition with company-specific version
- Unresolved mention discovered during graph exploration

**Open Questions:**
- Should alias rewrite be automatic or opt-in?
- Should conflicts block ingestion or just warn?
- Should we support entity versioning (v1, v2)?

**Future Considerations:**
- Entity similarity detection (suggest merges)
- Cross-registry entity reconciliation
- Distributed entity resolution (P2P registries)
