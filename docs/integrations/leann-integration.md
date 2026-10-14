# LEANN Integration

Not implemented. There is no LEANN code in ctxt or dpkms: `storage.type` accepts
only `sqlite` and `postgres` (`internal/storageutil/factory.go`), so a `leann`
storage type fails at startup.

Vector search runs inside the storage backend: `sqlite-vec` on SQLite,
`pgvector` on PostgreSQL. See [../dpkms/storage.md](../dpkms/storage.md) and
[../dpkms/embeddings.md](../dpkms/embeddings.md).
[ADR-022](../decisions/ADR-022-vector-semantic-search.md) lists LEANN as a
planned vector backend; it was never built.
