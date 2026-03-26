-- Migration 018: registry entitlements cache
-- Stores the entitlement record returned by a registry's /entitlements endpoint.
CREATE TABLE IF NOT EXISTS registry_entitlements (
    registry_name  TEXT PRIMARY KEY,
    plan           TEXT NOT NULL,
    namespaces     TEXT NOT NULL,  -- JSON array of namespace globs
    expires_at     TEXT,           -- ISO-8601, nullable
    fetched_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
