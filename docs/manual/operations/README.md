# Operations Runbook

This section is for operating dPKMS/`ctxt` reliably in multi-user or production-like environments.

## Start here

- Primary runbook: [`runbook.md`](./runbook.md)
- Embedding models (register, migrate, switch, retire; duplicates): [`embeddings.md`](./embeddings.md)
- Troubleshooting companion: [`../troubleshooting/faq.md`](../troubleshooting/faq.md)

## Operational scope

- Service availability and health checks
- Ingestion queue throughput and failure handling
- Search/composition path verification
- Embedding model lifecycle and migrations
- Registry and step governance checks
- Maintenance operations (`housekeeping`)

## Recommended cadence

- Daily: queue health + endpoint checks
- Weekly: maintenance + registry sync validation
- Release windows: config validation + pipeline governance checks
