-- Migration 056: event_outbox, the after-event outbox (ADR-075 §4).
--
-- One row per after event, written in the transaction of the mutation
-- that emits it. seq is the monotonic position (AUTOINCREMENT, so a
-- pruned seq is never reused) and the SSE event id; id is a UUIDv7 that
-- consumers dedupe on. Times are RFC 3339 UTC. instance_meta holds the
-- database's instance_id, minted once, and the highest seq ever pruned,
-- which tells a stale cursor from a current one. Separate from
-- sync_log (ADR-074): no shared code path.
CREATE TABLE IF NOT EXISTS event_outbox (
    seq         INTEGER PRIMARY KEY AUTOINCREMENT,
    id          TEXT NOT NULL UNIQUE,
    topic       TEXT NOT NULL,
    source      TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    origin      TEXT NOT NULL DEFAULT '',
    actor       TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL,
    object_id   TEXT NOT NULL DEFAULT '',
    payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_event_outbox_occurred_at ON event_outbox(occurred_at);

CREATE TABLE IF NOT EXISTS instance_meta (
    singleton            INTEGER PRIMARY KEY CHECK (singleton = 1),
    instance_id          TEXT NOT NULL,
    outbox_pruned_through INTEGER NOT NULL DEFAULT 0
);
