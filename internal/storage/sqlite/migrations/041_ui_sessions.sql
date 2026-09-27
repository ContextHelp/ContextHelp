-- Migration 041: web UI browser sessions and their one-time login codes.
--
-- A browser signs in by exchanging a login code, minted by an
-- authenticated CLI call, for a session cookie. Only SHA-256 hashes of
-- the cookie secret, the code and the minting static token are stored.
-- Times are Unix milliseconds (UTC) so range comparisons are numeric.
CREATE TABLE IF NOT EXISTS ui_sessions (
    id              TEXT PRIMARY KEY,
    secret_hash     TEXT NOT NULL UNIQUE,
    principal_id    TEXT NOT NULL,
    token_hash      TEXT NOT NULL,
    scope           TEXT NOT NULL,
    user_agent      TEXT NOT NULL DEFAULT '',
    remote_addr     TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    idle_expires_at INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    revoked_at      INTEGER,
    revoke_reason   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ui_sessions_token_hash ON ui_sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_ui_sessions_principal ON ui_sessions(principal_id);

CREATE TABLE IF NOT EXISTS ui_login_codes (
    code_hash    TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    scope        TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
