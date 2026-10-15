package storage

// System returns d as the unscoped, system-level driver: every row,
// regardless of who is asking.
//
// It is the only way the pipeline, jobs and housekeeping should name
// the raw driver, so that each unscoped read is a visible, greppable
// choice rather than the ambient default. Request handlers (the HTTP
// and gRPC servers, the MCP mount) never call it: they receive the
// viewer-scoped driver instead, and a lint rule rejects System in
// those packages (ADR-080 §4).
//
// The call itself changes nothing today; it exists so the unscoped
// path is named before the scoped one lands, and so call sites that
// legitimately bypass visibility are already marked when it does.
func System(d StorageDriver) StorageDriver { return d }
