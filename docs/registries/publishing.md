# Publishing a Registry

This guide targets registry publishers building curated, optionally paid
registries.

## Default distribution model

- **Thin sync (index/schema):** subscribers can replicate tags/entities/edges and
  safe summaries/headers
- **JIT pull (full content):** canonical content is fetched on demand and can be
  gated by entitlements and metered via credits

## What to publish in the index

- Taxonomy and entity definitions
- Object headers (id, title, timestamps, tags, mentions)
- Safe abstracts (optional)

## What to keep JIT-only

- Full object bodies
- Chunked sections
- High-value derived outputs

See `docs/dpkms/registry-protocol.md` and `docs/decisions/ADR-034-registry-index-sync-and-jit-resolution.md`.
