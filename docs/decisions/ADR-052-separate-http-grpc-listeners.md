# ADR-052 – Separate HTTP and gRPC Listeners in Same Process

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

The `dpkms serve` command exposes both a REST API (HTTP) and a gRPC API. The scaffolded command already accepts `--port` (default 8080) and `--grpc-port` (default 9090) flags, implying two separate listeners.

Three architectural approaches exist for serving both protocols:

1. **Separate listeners** — HTTP server and gRPC server on different ports in the same process.
2. **grpc-gateway** — Generate REST handlers from `.proto` files, single gRPC backend.
3. **connectrpc** — Serve both gRPC and HTTP/JSON from the same handlers via the Connect protocol.

The REST API (`api-rest.md`) and gRPC API (`api-grpc.md`) are specified independently with different endpoint shapes, error formats, and middleware needs. The REST API uses JSON with a specific error envelope (`{"error": {"code": ..., "message": ..., "details": {}}}`), while gRPC uses Protobuf with standard gRPC status codes.

---

## Decision

**HTTP and gRPC will run as separate listeners within the same `dpkms serve` process.** Each has its own middleware stack. Either can be disabled independently via configuration.

### Architecture

```
dpkms serve
├── HTTP listener (:8080)
│   ├── chi router (ADR-051)
│   ├── Middleware: RequestID → Logger → Auth → RateLimit → CORS
│   └── Handlers: REST endpoints per api-rest.md
│
├── gRPC listener (:9090)
│   ├── grpc.NewServer()
│   ├── Interceptors: Auth → Logger → Recovery
│   └── Services: Protobuf services per api-grpc.md
│
└── Shared
    ├── Storage (Store interface)
    ├── Job queue
    ├── Query engine
    └── Auth manager (ADR-023)
```

### Configuration

```yaml
server:
  http:
    enabled: true
    port: 8080
    bind: 127.0.0.1
  grpc:
    enabled: true
    port: 9090
    bind: 127.0.0.1
```

### Lifecycle

```go
// In dpkms serve command
g, ctx := errgroup.WithContext(ctx)

g.Go(func() error {
    return httpServer.ListenAndServe()
})

g.Go(func() error {
    return grpcServer.Serve(grpcListener)
})

// Graceful shutdown on SIGINT/SIGTERM
g.Go(func() error {
    <-ctx.Done()
    httpServer.Shutdown(shutdownCtx)
    grpcServer.GracefulStop()
    return nil
})

return g.Wait()
```

---

## Rationale

### Alternatives Considered

#### 1. grpc-gateway (Rejected)

Generate REST handlers from `.proto` files. Single gRPC backend, REST is a generated proxy.

**Rejected because:**
- Ties REST API design to Protobuf message structure — the REST API in `api-rest.md` has JSON shapes that don't map 1:1 to the gRPC messages in `api-grpc.md`
- REST error envelope (`{"error": {...}}`) differs from gRPC status codes — gateway can't produce both without custom error handlers
- Adds proto annotation dependency (`google.api.http`)
- Code generation step adds build complexity
- REST middleware (CORS, rate limit headers) doesn't map to gRPC interceptors
- Limits REST flexibility (can't have REST-only endpoints or different pagination)

#### 2. connectrpc (Rejected)

Serve both gRPC and HTTP/JSON from the same handler implementations via Connect protocol.

**Rejected because:**
- Less mature ecosystem than standard gRPC
- Requires clients to use Connect protocol or gRPC — standard HTTP clients need adaptation
- Single handler implementation means middleware can't differ between HTTP and gRPC paths
- The REST API specification in `api-rest.md` is explicitly HTTP/JSON, not Connect
- Team would need to learn a new protocol

### Benefits of Chosen Approach

- **Independent middleware** — HTTP gets CORS, rate limit headers, JSON error envelopes; gRPC gets interceptors, status codes, metadata
- **Independent lifecycle** — can disable gRPC for simple deployments, or disable HTTP for agent-only setups
- **No code generation dependency** — `.proto` files compiled once for gRPC; REST handlers are hand-written Go
- **Matches existing design** — `serve.go` already has separate port flags; `api-rest.md` and `api-grpc.md` are independently specified
- **Simple to reason about** — two servers, two ports, two middleware stacks, shared business logic

---

## Consequences

### Positive

- REST and gRPC APIs can evolve independently
- Middleware stacks are purpose-built for each protocol
- Either server can be disabled without affecting the other
- No proto annotation or code generation coupling
- Straightforward error handling per protocol

### Negative

- Two sets of handlers to maintain (REST handlers + gRPC service implementations)
- Shared business logic must be factored into service layer (not in handlers)
- Two ports to document and configure

### Neutral

- `.proto` files still needed for gRPC service definitions (not yet created)
- OpenAPI spec for REST can be maintained independently of proto definitions
- `errgroup` pattern for concurrent listener management is standard Go

---

## Implementation Notes

**Service layer pattern:**
```go
// Shared business logic — both HTTP handlers and gRPC services call this
type ObjectService struct {
    store  storage.Store
    query  query.Engine
}

func (s *ObjectService) Get(ctx context.Context, id string) (*Object, error) { ... }
func (s *ObjectService) List(ctx context.Context, q *Query) ([]*Object, error) { ... }

// HTTP handler wraps service
func (h *HTTPHandler) GetObject(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    obj, err := h.objects.Get(r.Context(), id)
    // JSON response with error envelope
}

// gRPC service wraps same service
func (s *GRPCServer) GetObject(ctx context.Context, req *pb.GetObjectRequest) (*pb.Object, error) {
    obj, err := s.objects.Get(ctx, req.Id)
    // Protobuf response with gRPC status
}
```

---

## References

- **api/api-rest.md** — REST endpoint specification
- **api/api-grpc.md** — gRPC service specification
- **cmd/dpkms/cmd/serve.go** — Scaffolded serve command with `--port` and `--grpc-port` flags
- **ADR-023** — Authentication model (shared auth manager)
- **ADR-051** — chi as HTTP framework

---
