# ADR-020 – Export/Import Contract and Zero Lock-In Portability

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Knowledge systems that trap users' data undermine sovereignty and trust. Users need guarantees of portability:

**Vendor Lock-In Risk:**
- Proprietary formats prevent migration to alternatives
- Data trapped in specific tool ecosystems
- Loss of access means loss of knowledge
- No exit strategy if tool discontinued
- Switching costs prohibitively high

**Data Integrity Concerns:**
- Export/import may lose metadata or relationships
- Graph structure not preserved across boundaries
- Entity mentions and backlinks broken
- Provenance trails lost
- Attachments missing or corrupted

**Format Fragmentation:**
- No standard format for knowledge exports
- Tool-specific schemas incompatible
- Human readability sacrificed for compression
- Future software can't read old formats
- Documentation insufficient for manual recovery

**Incomplete Exports:**
- Configuration excluded from exports
- Profile definitions not portable
- Pipeline results not captured
- Enrichment lost (must re-process)
- History and revisions omitted

**Migration Complexity:**
- Moving between instances requires expertise
- No clear migration path
- Downtime during transition
- Risk of data loss during migration
- No validation tools

**Constraints:**
- Must preserve all knowledge completely (objects, entities, edges, attachments)
- Must maintain stable IDs across export/import
- Must be human-readable where possible (Markdown, JSON)
- Must support incremental export (time ranges, filters)
- Must include cryptographic verification (integrity)
- Must work offline (no network dependency)
- Must be self-contained (all dependencies bundled)
- Must support encryption (privacy during transport)

**Affected Subsystems:**
- Storage layer (all data types)
- Graph index (entities, mentions, edges)
- Attachment storage (files, blobs)
- Configuration system (profiles, settings)
- Encryption layer (encrypted exports)
- Registry system (registry snapshots)

**Goals:**
- Zero lock-in (users can leave at any time)
- Complete data preservation (nothing lost)
- Human readability (Markdown + JSON)
- Stable ID preservation (identity maintained)
- Self-documenting format (future-proof)
- Cryptographic verification (integrity proof)
- Offline operation (no network required)

---

## Decision

**dPKMS will implement a comprehensive Export/Import Contract based on portable, self-contained bundles in open formats (Markdown + JSON + SQLite) with stable ID preservation, full graph structure, attachments, metadata, configuration, and optional encryption, ensuring users can completely migrate their knowledge without data loss or vendor lock-in.**

The export/import system provides:

1. **Bundle Format:**

   **Structure:**
   ```
   bundle.ctxt/
     manifest.json           # Bundle metadata and index
     objects/
       {uuid}.md             # Human-readable Markdown
       {uuid}.json           # Machine-readable metadata
     entities/
       entities.json         # All entity definitions
     edges/
       graph.json            # All graph relationships
     attachments/
       {hash}.{ext}          # Binary attachments
     config/
       profiles.yaml         # Profile definitions
       settings.yaml         # User settings
     registries/
       {name}.snapshot.json  # Registry snapshots
     provenance/
       history.json          # Revision history
     signatures/
       bundle.sig            # Cryptographic signature (optional)
   ```

   **Manifest Schema:**
   ```json
   {
     "format_version": "1.0",
     "created_at": "2024-01-26T12:00:00Z",
     "created_by": "ctxt v1.0.0",
     "bundle_id": "uuid",
     "bundle_type": "full | incremental | selective",

     "contents": {
       "objects": 1542,
       "entities": 234,
       "edges": 3421,
       "attachments": 87,
       "profiles": 3,
       "registries": 2
     },

     "filters_applied": {
       "time_range": {
         "after": "2024-01-01",
         "before": "2024-12-31"
       },
       "query": "tag==technical",
       "profile": "engineer"
     },

     "integrity": {
       "objects_hash": "sha256-hash",
       "entities_hash": "sha256-hash",
       "attachments_hash": "sha256-hash",
       "bundle_hash": "sha256-hash",
       "signature": "optional-signature"
     },

     "encryption": {
       "enabled": true,
       "algorithm": "age",
       "key_derivation": "argon2id",
       "encrypted_files": ["objects/*", "attachments/*"]
     }
   }
   ```

2. **Object Export Format:**

   **Markdown File (`{uuid}.md`):**
   ```markdown
   ---
   id: 550e8400-e29b-41d4-a716-446655440000
   type: article
   subtype: technical-post
   created_at: 2024-01-26T10:30:00Z
   updated_at: 2024-01-26T14:22:00Z
   source: https://example.com/article
   pipeline: url.generic
   profile: engineer
   ---

   # Summary

   This article discusses API design patterns for RESTful services,
   focusing on versioning strategies and backward compatibility.

   # Tags

   - technical (weight: 0.95, source: ai)
   - api-design (weight: 0.90, source: ai)
   - architecture (weight: 0.85, source: hint)

   # Mentions

   - @api.rest
   - @pattern.versioning
   - @component.gateway

   # Decisions

   - **Use semantic versioning for API endpoints**
     Status: made
     Rationale: Clear communication of breaking changes
     Date: 2024-01-26

   # Tasks

   - [ ] Review current API versioning scheme
   - [ ] Implement v2 endpoints with breaking changes
   - [x] Document migration guide

   # Content

   [Full article content extracted and cleaned...]

   # Provenance

   - Captured: 2024-01-26T10:30:00Z
   - Enriched by: url.generic pipeline
   - Mentions extracted: 3
   - Decisions extracted: 1
   - Registry influences: technical-patterns, api-references
   ```

   **JSON Metadata (`{uuid}.json`):**
   ```json
   {
     "id": "550e8400-e29b-41d4-a716-446655440000",
     "type": "article",
     "subtype": "technical-post",
     "created_at": "2024-01-26T10:30:00Z",
     "updated_at": "2024-01-26T14:22:00Z",
     "source": "https://example.com/article",
     "content_type": "text/html",
     "pipeline": "url.generic",
     "profile": "engineer",

     "metadata": {
       "url_canonical": "https://example.com/article",
       "author": "John Doe",
       "published_at": "2024-01-20T08:00:00Z",
       "reading_time_minutes": 8
     },

     "summaries": [
       {
         "text": "Article discusses API design...",
         "type": "short",
         "generated_by": "openai-gpt-4"
       }
     ],

     "sections": [
       {
         "title": "Introduction",
         "content": "...",
         "order": 1
       },
       {
         "title": "Versioning Strategies",
         "content": "...",
         "order": 2
       }
     ],

     "tags": [
       {
         "label": "technical",
         "weight": 0.95,
         "source": "ai",
         "explanation": "Content is highly technical"
       }
     ],

     "mentions": [
       "@api.rest",
       "@pattern.versioning",
       "@component.gateway"
     ],

     "decisions": [
       {
         "text": "Use semantic versioning for API endpoints",
         "status": "made",
         "rationale": "Clear communication of breaking changes",
         "extracted_at": "2024-01-26T14:22:00Z"
       }
     ],

     "tasks": [
       {
         "text": "Review current API versioning scheme",
         "status": "open",
         "extracted_at": "2024-01-26T14:22:00Z"
       }
     ],

     "attachments": [
       {
         "filename": "diagram.png",
         "hash": "sha256-abc123",
         "mime_type": "image/png",
         "size_bytes": 45231
       }
     ],

     "embeddings": {
       "model": "text-embedding-3-large",
       "vector": [0.123, -0.456, ...],
       "generated_at": "2024-01-26T10:35:00Z"
     },

     "provenance": {
       "registry_influences": ["technical-patterns", "api-references"],
       "pipeline_steps": [
         {
           "name": "fetch_url",
           "completed_at": "2024-01-26T10:30:15Z"
         },
         {
           "name": "extract_content",
           "completed_at": "2024-01-26T10:30:18Z"
         }
       ]
     }
   }
   ```

3. **Graph Export Format:**

   **Entities (`entities.json`):**
   ```json
   {
     "entities": [
       {
         "id": "api.rest",
         "title": "REST API",
         "description": "Representational State Transfer architectural style",
         "aliases": ["REST", "RESTful API", "REST API"],
         "translations": {
           "fr": {
             "title": "API REST",
             "description": "Style architectural REST"
           }
         },
         "namespace": "api",
         "version": "1.0",
         "registry_source": "technical-patterns",
         "metadata": {
           "category": "architecture",
           "maturity": "stable"
         },
         "created_at": "2024-01-01T00:00:00Z",
         "updated_at": "2024-01-15T10:00:00Z"
       }
     ]
   }
   ```

   **Graph Edges (`edges/graph.json`):**
   ```json
   {
     "edges": [
       {
         "id": "edge-uuid",
         "from_type": "object",
         "from_id": "550e8400-e29b-41d4-a716-446655440000",
         "to_type": "entity",
         "to_id": "api.rest",
         "edge_type": "mentions",
         "weight": 1.0,
         "metadata": {
           "mentioned_in_section": "Introduction",
           "mention_count": 3
         },
         "created_at": "2024-01-26T10:35:00Z"
       },
       {
         "id": "edge-uuid-2",
         "from_type": "entity",
         "from_id": "api.rest",
         "to_type": "entity",
         "to_id": "pattern.versioning",
         "edge_type": "related",
         "weight": 0.85,
         "metadata": {
           "relationship": "commonly-used-together"
         },
         "created_at": "2024-01-15T10:00:00Z"
       }
     ]
   }
   ```

4. **Configuration Export:**

   **Profiles (`config/profiles.yaml`):**
   ```yaml
   profiles:
     engineer:
       type: role
       description: "Technical implementation focus"
       scopes:
         registries:
           - technical-patterns
         entities:
           include:
             - "@api.*"
           exclude:
             - "@marketing.*"
       # ... full profile definition
   ```

   **Settings (`config/settings.yaml`):**
   ```yaml
   storage:
     type: sqlite
     path: ~/.local/share/ctxt/db.sqlite

   pipelines:
     enabled:
       - text.short
       - url.generic

   # ... full configuration
   ```

5. **Export Modes:**

   **Full Export:**
   - All objects, entities, edges, attachments
   - All profiles and configuration
   - Complete registry snapshots
   - Full revision history
   - Usage: Migration, backup, archival

   **Incremental Export:**
   - Objects created/updated in time range
   - Related entities and edges
   - New attachments only
   - Configuration changes
   - Usage: Regular backups, sync

   **Selective Export:**
   - Filtered by query (tags, entities, profile)
   - Graph closure (include related items)
   - Relevant attachments only
   - Profile-specific configuration
   - Usage: Sharing, collaboration, publishing

   **Minimal Export:**
   - Markdown only (human-readable)
   - No metadata, no graph, no attachments
   - Degraded import (re-enrichment required)
   - Usage: Publishing, documentation

6. **Import Behavior:**

   **ID Preservation:**
   ```
   Objects imported with original UUIDs preserved
   - Detect ID collisions
   - Options: skip, merge, rename
   - Maintain graph references

   Entities imported with canonical slugs
   - Merge with existing entities
   - Preserve aliases and translations
   - Update references in objects
   ```

   **Conflict Resolution:**
   ```yaml
   import_strategy:
     on_id_collision: skip | overwrite | merge | rename
     on_entity_conflict: merge | prefer_local | prefer_import
     on_edge_duplicate: skip | update_weight
     on_attachment_collision: skip | overwrite_if_different
   ```

   **Re-Enrichment:**
   ```
   Optional: Re-run pipelines on imported objects
   - Regenerate embeddings
   - Re-extract mentions
   - Update tags with current models
   - Rebuild indexes
   ```

7. **CLI Interface:**
   ```bash
   # Export
   ctxt export --output bundle.ctxt
   ctxt export --output bundle.ctxt --format archive  # .tar.gz
   ctxt export --output bundle.ctxt --encrypt --passphrase-stdin < pass.txt

   # Selective export
   ctxt export --query "tag==technical AND created_at>=2024-01-01"
   ctxt export --profile engineer --output engineer-bundle.ctxt
   ctxt export --entity @api.stripe --graph-depth 2

   # Incremental export
   ctxt export --after 2024-01-01 --before 2024-12-31
   ctxt export --since-export last-backup.ctxt

   # Minimal export (Markdown only)
   ctxt export --format markdown-only --output docs/

   # Import
   ctxt import bundle.ctxt
   ctxt import bundle.ctxt --strategy merge
   ctxt import bundle.ctxt.enc --passphrase-stdin < pass.txt

   # Validation
   ctxt export verify bundle.ctxt
   ctxt export inspect bundle.ctxt
   ctxt export diff bundle1.ctxt bundle2.ctxt
   ```

8. **Bundle Verification:**
   ```bash
   # Verify integrity
   $ ctxt export verify bundle.ctxt

   ✓ Manifest valid
   ✓ Format version: 1.0
   ✓ Objects: 1542 (hash matches)
   ✓ Entities: 234 (hash matches)
   ✓ Edges: 3421 (hash matches)
   ✓ Attachments: 87 (hash matches)
   ✓ Bundle hash: valid
   ✓ Signature: valid (optional)

   Bundle is valid and complete.
   ```

---

## Rationale

### Alternatives Considered

#### 1. **Database Dump Only (Rejected)**
Export raw SQLite/Postgres database file.

**Rejected because:**
- Not human-readable
- Format-specific (tied to storage backend)
- No portability to other tools
- Attachments not included
- Configuration excluded
- Not self-documenting

#### 2. **JSON-Only Export (Rejected)**
Single JSON file with all data.

**Rejected because:**
- Not human-readable for content
- Attachments base64-encoded (bloat)
- Poor git diff support
- Hard to manually edit or inspect
- No markdown rendering

#### 3. **Separate Exports Per Entity Type (Rejected)**
Different export commands for objects, entities, configuration.

**Rejected because:**
- Complex user experience
- Easy to forget components
- Graph relationships broken
- No atomic export
- Migration error-prone

#### 4. **Custom Binary Format (Rejected)**
Proprietary compressed binary format for efficiency.

**Rejected because:**
- Vendor lock-in (defeats purpose)
- Not human-readable
- Requires special tools to inspect
- Format evolution complex
- Not self-documenting

#### 5. **Cloud-Based Export (Rejected)**
Export to cloud service for portability.

**Rejected because:**
- Network dependency
- Privacy concerns
- Vendor lock-in (different problem)
- Offline users excluded
- Trust required in third party

### Benefits of Chosen Approach

**Zero Lock-In:**
- Open formats (Markdown, JSON, SQLite)
- Human-readable where possible
- No proprietary dependencies
- Self-contained bundles
- Complete data portability

**Data Integrity:**
- Stable ID preservation
- Graph structure maintained
- Provenance preserved
- Cryptographic verification
- Nothing lost in export/import

**Human Accessibility:**
- Markdown for readability
- JSON for machine processing
- Can manually inspect/edit
- Git-friendly format
- Documentation built-in

**Future-Proof:**
- Self-documenting format
- Version metadata included
- Open standards used
- Future software can read
- Manual recovery possible

**Flexibility:**
- Multiple export modes
- Selective filtering
- Incremental backups
- Encrypted bundles
- Configurable strategies

**Sovereignty:**
- Offline operation
- User controls exports
- No vendor dependency
- No lock-in period
- Exit anytime

### Drawbacks / Risks

**Bundle Size:**
- Full exports can be large
- Attachments add significant size
- Embeddings add overhead
- Compression needed

**Export Performance:**
- Large knowledge bases slow to export
- Graph traversal expensive
- Attachment copying slow
- Streaming needed for large exports

**Format Evolution:**
- Version compatibility concerns
- Migration between versions
- Backward compatibility burden
- Breaking changes disruptive

**Manual Editing Risk:**
- Users may corrupt bundles
- Invalid JSON breaks import
- Graph integrity violations
- Verification required

---

## Consequences

### Positive

**User Sovereignty:**
- Complete control over data
- Can leave at any time
- No vendor lock-in
- Exit cost is zero

**Data Preservation:**
- Nothing lost in migration
- Graph structure maintained
- Attachments included
- Configuration portable

**Trust Building:**
- Transparency through open formats
- Verifiable exports
- No hidden dependencies
- User-friendly portability

**Interoperability:**
- Other tools can read bundles
- Standard formats enable integration
- Knowledge graphs portable
- Future compatibility

### Negative

**Implementation Complexity:**
- Multiple formats to maintain
- Graph export logic complex
- ID preservation tricky
- Verification comprehensive

**Storage Overhead:**
- Dual format (MD + JSON) redundant
- Attachments duplicated
- Embeddings large
- Compression essential

**Export Performance:**
- Large exports slow
- Memory intensive
- Disk I/O heavy
- Streaming required

**Format Maintenance:**
- Version evolution needed
- Backward compatibility burden
- Migration tools required
- Documentation extensive

### Neutral / Considerations

**Compression:**
- Bundle should be compressed
- .tar.gz for archive mode
- Balance size vs accessibility
- Trade-off: readable vs compact

**Partial Imports:**
- Should support importing subsets
- Selective restore from backup
- Filter on import
- Merge strategies needed

**Bundle Signing:**
- Optional signature for integrity
- Verify authenticity
- Chain of trust
- Key management required

**Delta Exports:**
- Export only changes since last export
- Efficient backups
- Requires state tracking
- Merge logic complex

---

## Implementation Notes

### Core Components

**Export Engine (`dPKMS/export/`):**
```go
type Exporter struct {
    store       Store
    graphStore  GraphStore
    attachments AttachmentStore
    config      ConfigStore
}

func (e *Exporter) Export(
    req ExportRequest,
) (*Bundle, error)

type ExportRequest struct {
    Mode         ExportMode  // full, incremental, selective
    OutputPath   string
    Filters      FilterSet
    Encrypt      bool
    Passphrase   string
    IncludeConfig bool
    GraphDepth   int
}
```

**Bundle Writer:**
```go
type BundleWriter interface {
    WriteManifest(m *Manifest) error
    WriteObject(obj Object) error
    WriteEntity(ent Entity) error
    WriteEdge(edge Edge) error
    WriteAttachment(att Attachment) error
    WriteConfig(cfg Config) error
    Close() error
}

type DirectoryBundleWriter struct { ... }
type ArchiveBundleWriter struct { ... }
```

**Import Engine (`dPKMS/import/`):**
```go
type Importer struct {
    store       Store
    graphStore  GraphStore
    attachments AttachmentStore
    config      ConfigStore
}

func (i *Importer) Import(
    bundlePath string,
    strategy ImportStrategy,
) (*ImportResult, error)

type ImportStrategy struct {
    OnIDCollision      CollisionStrategy
    OnEntityConflict   ConflictStrategy
    OnEdgeDuplicate    DuplicateStrategy
    ReEnrich           bool
    RebuildIndexes     bool
}

type ImportResult struct {
    ObjectsImported    int
    ObjectsSkipped     int
    ObjectsMerged      int
    EntitiesImported   int
    EdgesImported      int
    AttachmentsImported int
    Errors             []error
}
```

**Verification:**
```go
type BundleVerifier struct {}

func (v *BundleVerifier) Verify(bundlePath string) (*VerificationResult, error)

type VerificationResult struct {
    Valid              bool
    FormatVersion      string
    ManifestValid      bool
    IntegrityChecks    map[string]bool
    SignatureValid     bool
    Errors             []error
    Warnings           []string
}
```

**CLI Commands:**
```bash
# Export
ctxt export [flags]

# Flags
--output <path>              # Output path (file or directory)
--format <format>            # archive | directory | markdown-only
--mode <mode>                # full | incremental | selective
--query <rsql>               # Filter objects
--profile <name>             # Profile-scoped export
--entity <entity>            # Entity-centered export
--graph-depth <n>            # Graph traversal depth
--after <date>               # Incremental after date
--before <date>              # Incremental before date
--since-export <path>        # Incremental since last export
--encrypt                    # Encrypt bundle
--passphrase-stdin           # Read passphrase from stdin
--include-config             # Include configuration
--include-registries         # Include registry snapshots
--exclude-attachments        # Skip attachments
--exclude-embeddings         # Skip embeddings

# Import
ctxt import <bundle-path> [flags]

# Flags
--strategy <strategy>        # skip | overwrite | merge | rename
--no-re-enrich               # Skip pipeline re-execution
--no-rebuild-indexes         # Skip index rebuilding
--passphrase-stdin           # Read passphrase from stdin
--dry-run                    # Preview import without applying

# Verification
ctxt export verify <bundle-path>
ctxt export inspect <bundle-path>
ctxt export diff <bundle1> <bundle2>
```

### Storage Schema

No schema changes required. Export/import operates on existing data.

### Integration Points

**With Storage Layer:**
1. Query all objects for export
2. Retrieve entities and edges
3. Copy attachments to bundle
4. Write objects in import transaction

**With Graph System:**
1. Export graph closure (depth-limited)
2. Preserve edge relationships
3. Import maintains graph integrity
4. Rebuild indexes post-import

**With Encryption:**
1. Encrypt bundle with passphrase
2. Include encryption metadata
3. Decrypt on import
4. Re-encrypt with local key

**With Configuration:**
1. Export profiles and settings
2. Merge on import (don't overwrite blindly)
3. Validate configuration compatibility
4. Warn on version mismatches

### Migration Strategy

**Phase 1: Basic Export/Import (Skeleton 3)**
- Full export (directory format)
- Basic import (skip strategy)
- Markdown + JSON dual format
- Attachments included

**Phase 2: Selective Export (Skeleton 4)**
- Query-based filtering
- Incremental exports
- Profile-scoped exports
- Entity-centered exports

**Phase 3: Advanced Features (Skeleton 7)**
- Encrypted bundles
- Bundle verification
- Cryptographic signatures
- Delta exports

**Phase 4: Optimization (Skeleton 8+)**
- Archive format (.tar.gz)
- Streaming large exports
- Parallel attachment copying
- Compression optimization

**Backward Compatibility:**
- Version metadata in manifest
- Migration tools for format changes
- Fallback to older format readers
- Clear error messages on incompatibility

### Testing Requirements

**Unit Tests:**
- Export each component type
- Import with different strategies
- ID collision handling
- Graph integrity preservation

**Integration Tests:**
- Full export/import round-trip
- Incremental export/import
- Encrypted bundle handling
- Verification accuracy

**Data Integrity Tests:**
- No data loss in export/import
- Graph structure preserved
- Attachment integrity (hash verification)
- Configuration preservation

**Performance Tests:**
- Export 10k+ objects
- Import 10k+ objects
- Large attachment handling (GBs)
- Memory usage profiling

### Performance Optimization

**Streaming:**
- Stream objects during export
- Stream attachments during copy
- Avoid loading entire bundle in memory

**Parallelization:**
- Parallel object serialization
- Concurrent attachment copying
- Multi-threaded compression

**Incremental Optimization:**
- Track last export timestamp
- Delta computation
- Skip unchanged objects
- Deduplication

**Compression:**
- gzip/zstd for archives
- Level 6 compression (balance speed/size)
- Compress per-file or entire bundle

---

## References

- **architecture.md:224-227** – Export/Import Contract specification
- **ROADMAP.md** – Skeleton 0: Hello Context (export produces portable bundle)
- ADR-001 – Local-First and Decentralized (sovereignty principle)
- ADR-006 – Storage backends (export from any backend)
- ADR-013 – Knowledge Graph (graph structure in exports)
- ADR-019 – Encryption (encrypted bundles)

**Related Documents:**
- `dPKMS/export/` – Export engine (to be created)
- `dPKMS/import/` – Import engine (to be created)
- `dPKMS/bundle/` – Bundle format implementation (to be created)

**External Standards:**
- CommonMark (Markdown spec): https://commonmark.org/
- JSON Schema: https://json-schema.org/
- tar format: https://www.gnu.org/software/tar/manual/
- Age encryption: https://age-encryption.org/

---
