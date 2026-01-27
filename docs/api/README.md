# API Documentation

This directory contains documentation for external APIs — the programmatic interfaces for integrating with the system.

## Available APIs

### REST API
- [api-rest.md](api-rest.md) - HTTP/REST API reference
- Primary use case: Web integrations, automation scripts, frontend apps
- Format: JSON over HTTP
- Authentication: Token-based

### gRPC API
- [api-grpc.md](api-grpc.md) - gRPC API reference
- Primary use case: High-performance agent runtimes, real-time systems
- Format: Protocol Buffers
- Features: Streaming support, efficient binary protocol

### Node Admin API
- [node-admin-api.md](node-admin-api.md) - Admin surface for nodes (used by context.help cloud)

## API Architecture

Both APIs are served by **`dpkms serve`** and provide access to both **ctxt** (intelligence) and **dPKMS** (substrate) capabilities:

- **Input Layer** - Ingestion endpoints (`POST /analyze`, `Ingest()`)
- **Query Layer** - Search and retrieval (`GET /objects`, `Search()`)
- **Composition Layer** - Brief/plan generation (`POST /compose`, `Compose()`)
- **Management Layer** - Jobs, profiles, registries (`GET /jobs`, `GetJob()`)
- **Entity Layer** - Entity resolution and graph navigation (`GET /entities`, `GetEntity()`)

## Common Patterns

### Authentication
- Local-only mode (no auth required by default)
- Token-based auth for remote access (enable with `--public`)
- Scoped permissions via focus profiles

### Versioning
- API version in path: `/api/v1/...`
- Protocol buffer versioning for gRPC
- Backward compatibility guarantees

### Error Handling
- HTTP status codes for REST
- gRPC status codes
- Structured error responses with details

### Streaming
- REST: Server-Sent Events (SSE) where applicable
- gRPC: Native bidirectional streaming

## Starting the API Server

Both REST and gRPC APIs are served by the same command:

```bash
dpkms serve [options]
```

Options:
- `--port <number>` - HTTP port (default: 8080)
- `--grpc-port <number>` - gRPC port (default: 9090)
- `--public` - Allow remote connections
- `--workers <n>` - Number of worker threads

## See Also

- [../ctxt/api-cli.md](../ctxt/api-cli.md) - CLI interface documentation
- [../architecture.md](../architecture.md) - System architecture
- [../dpkms/](../dpkms/) - Underlying substrate capabilities
- [../ctxt/](../ctxt/) - Intelligence and behavior layer
