# ADR-051 – Use chi as HTTP Framework for REST API

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

The REST API is fully specified in `api/api-rest.md` with all endpoints, request/response shapes, error envelopes, and authentication headers defined. The `dpkms serve` command (already scaffolded in `cmd/dpkms/cmd/serve.go`) needs an HTTP framework to implement these endpoints.

Go offers several options for HTTP routing and middleware. The choice affects:
- Middleware chaining (auth, logging, CORS, rate limiting)
- Routing patterns and path parameters
- Compatibility with `net/http` handlers
- OpenAPI generation potential
- Dependency footprint
- Idiomatic Go patterns

---

## Decision

**Use [chi](https://github.com/go-chi/chi) (v5) as the HTTP framework for the dPKMS REST API.**

### Why chi

- **stdlib compatible** — chi handlers are `http.Handler` / `http.HandlerFunc`. No custom context types. Any `net/http` middleware works unchanged.
- **Lightweight** — zero external dependencies. chi itself is ~1500 LOC.
- **Idiomatic** — follows Go conventions. No magic, no reflection, no code generation.
- **Middleware chaining** — `r.Use(middleware)` for clean, composable middleware stacks.
- **Route grouping** — `r.Route("/api/v1", func(r chi.Router) { ... })` for versioned APIs.
- **Path parameters** — `chi.URLParam(r, "id")` for `GET /objects/{id}`.
- **Battle-tested** — widely adopted in production Go services.

### Example Wiring

```go
r := chi.NewRouter()

// Global middleware
r.Use(middleware.RequestID)
r.Use(middleware.RealIP)
r.Use(middleware.Logger)
r.Use(middleware.Recoverer)

// API routes
r.Route("/api/v1", func(r chi.Router) {
    // Auth middleware (optional, per ADR-023)
    r.Use(authMiddleware)

    r.Get("/health", healthHandler)
    r.Post("/analyze", analyzeHandler)

    r.Route("/objects", func(r chi.Router) {
        r.Get("/", listObjectsHandler)
        r.Get("/{id}", getObjectHandler)
        r.Patch("/{id}", updateObjectHandler)
        r.Delete("/{id}", deleteObjectHandler)
    })

    r.Route("/jobs", func(r chi.Router) {
        r.Get("/", listJobsHandler)
        r.Get("/{id}", getJobHandler)
        r.Post("/{id}/retry", retryJobHandler)
    })
})
```

---

## Rationale

### Alternatives Considered

#### 1. Pure `net/http` (Rejected)

Go 1.22+ enhanced `http.ServeMux` with method-based routing and path parameters.

**Rejected because:**
- Middleware chaining requires manual wrapping (`func(next http.Handler) http.Handler`)
- No route grouping — verbose for versioned APIs with many endpoints
- Path parameter extraction less ergonomic than chi
- No built-in middleware library (logging, recovery, CORS)
- Adequate for small APIs but cumbersome for 20+ endpoints

#### 2. echo (Rejected)

Full-featured framework with built-in middleware, validation, and binding.

**Rejected because:**
- Uses custom `echo.Context` — not stdlib `http.Handler` compatible
- Larger dependency footprint
- More opinionated than needed
- Existing `net/http` middleware requires adapters
- chi provides the same routing with stdlib compatibility

#### 3. gin (Rejected)

Most popular Go web framework, fast, battle-tested.

**Rejected because:**
- Uses custom `gin.Context` — not stdlib compatible
- Heavier dependency (includes rendering, binding, validation)
- More opinionated patterns don't align with dPKMS's minimalist approach
- Performance difference vs chi is negligible for our workload (<1000 req/sec)

---

## Consequences

### Positive

- All middleware is standard `func(next http.Handler) http.Handler` — reusable across projects
- Auth interceptor from ADR-023 maps directly to chi middleware
- Route groups cleanly match the `/api/v1/` endpoint structure in `api-rest.md`
- Zero vendor lock-in — can migrate to any stdlib-compatible router if needed
- chi's built-in middleware (Logger, Recoverer, RequestID) cover common needs

### Negative

- Less "batteries included" than echo/gin — need to implement or import JSON binding, validation
- No built-in OpenAPI generation (can add `swaggo` or manual spec later)

### Neutral

- `go get github.com/go-chi/chi/v5` is the only new dependency for HTTP routing
- chi middleware and gRPC interceptors remain separate (by design, per ADR-052)

---

## References

- **api/api-rest.md** — All REST endpoints with request/response shapes
- **ADR-023** — Authentication model (token-based auth middleware)
- **ADR-052** — Separate HTTP and gRPC listeners
- **cmd/dpkms/cmd/serve.go** — Scaffolded serve command entry point
- chi documentation: https://github.com/go-chi/chi

---
