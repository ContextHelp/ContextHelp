# Input Validation

What ctxt and dpkms check on the way in: configuration, HTTP requests and
search queries. Secret detection in config is covered in
[Secrets Validation and Log Sanitization](secrets-validation-and-log-sanitization.md).

There is no user-configurable validation rule set: no custom secret
patterns, no exclusion lists, no `security.validation` config key.

---

## Configuration

`ctxt config validate` runs the full validator (`internal/config/validate.go`)
on the effective configuration (every file layer, env var and `-c`
override). It reports, among others:

- ports, storage and blob settings, quotas, duplicates, federations,
  embeddings and the secrets backend
- `server.access` must be `private`, `protected` or `public`; a non-private
  instance needs `server.auth` with at least one static token
- each static token: at least 32 characters, no `REPLACE_ME`, at least one
  role out of `admin`, `writer`, `reader`
- `registries[].auth.type` must be `bearer`, `api_key` or `oauth2`
- values written as `${VAR}` or `$VAR`: config files never expand them, so
  they are rejected
- keys the config does not declare (a warning: they are ignored)

`dpkms serve` applies the same access and token rules and refuses to bind a
port when they fail. `ctxt config doctor` runs the validator plus the secret
scan, the file permission check and the access lint in one pass.

```bash
ctxt config validate
ctxt config doctor
```

---

## HTTP API

The dpkms HTTP server (`internal/server/http`) applies these checks:

| Check | Response | Applies to |
|---|---|---|
| Host header allowlist: loopback names for the bound port plus `server.allowed_hosts` | `403 HOST_NOT_ALLOWED` | private instances always; protected/public only when `server.allowed_hosts` is set |
| Cross-origin state-changing browser requests (`Sec-Fetch-Site`, then `Origin` vs `Host`); GET/HEAD/OPTIONS and requests with neither header pass, as do browser-extension origins | `403 CROSS_ORIGIN_REQUEST` | every instance |
| A state-changing request with a body that is not `Content-Type: application/json` | `415 UNSUPPORTED_MEDIA_TYPE` | every instance |
| Missing or invalid credential | `401 UNAUTHORIZED` | protected and public instances |
| Credential without the route's scope | `403 INSUFFICIENT_SCOPE` | protected and public instances |
| `POST /api/v1/find` body larger than 1 MiB, or with unknown fields | `400 INVALID_REQUEST` | every instance |

Scopes and roles are listed in
[Inbound auth, roles and scopes](../manual/reference/config-and-permissions.md#inbound-auth-roles-and-scopes).
Proxy-related failures are explained in
[Put dpkms Behind a Reverse Proxy](../manual/operations/reverse-proxy.md#troubleshooting).

---

## Search queries

Free-text queries never reach the full-text engine raw.
`search.SanitizeFTSQueryFor` (`internal/search/safe_fts.go`, package
`internal/search/ftsq`) tokenizes the input and rebuilds it per dialect:
for SQLite FTS5 each token becomes a quoted phrase, which neutralises FTS5
operators (`-`, `:`, `"`, `*`, `AND`, `OR`, `NOT`, `NEAR`, parentheses);
PostgreSQL gets its own leg. An input with no usable tokens matches
nothing instead of erroring.

---

## Scanning files for secrets

ctxt has no command that scans arbitrary files or directories for secrets.
Use an external scanner:

```bash
gitleaks detect --source ~/.config/contexthelp/ --no-git --verbose --redact
gitleaks detect --verbose --redact   # the current repository's history
```

---

## Related Documentation

- [secrets-validation-and-log-sanitization.md](secrets-validation-and-log-sanitization.md) — config secret scan, file permissions, what output shows secrets
- [Config and Permissions](../manual/reference/config-and-permissions.md) — server access, auth and scopes
- [model/controls.md](model/controls.md) — security controls
