# Development Environment Variables

Debug output and test-suite configuration.

---

## Debug Logging

No env vars. Logs go to stderr at `warn` level; pass `--verbose` to
`ctxt` or `dpkms` for debug output.

```bash
dpkms serve --verbose
```

`ctxt` and `dpkms` expose no profiling (pprof) or metrics endpoints.

---

## Testing

Read by the test suites only, never by the binaries.

### INTEGRATION
**Type:** any non-empty value\
**Default:** _(unset — tests skip)_

Runs the env-gated tests under `test/integration/`; they skip without
it. Some files there also need the `integration` build tag.

```bash
INTEGRATION=1 go test -tags=integration,fts5 ./test/integration/...
```

### POSTGRES_DSN
**Type:** string (PostgreSQL connection URL)\
**Default:** _(unset)_

Server for the Postgres-backed tests. When unset, the tests build the URL
from the variables below. (The `storageutil` driver smoke test reads
`POSTGRES_URL` instead.)

```bash
POSTGRES_DSN=postgres://contexthelp:test_password@localhost:5432/contexthelp_test?sslmode=disable
```

### POSTGRES_HOST / POSTGRES_PORT / POSTGRES_USER / POSTGRES_PASSWORD / POSTGRES_DB

Used when `POSTGRES_DSN` is unset. `POSTGRES_HOST` and `POSTGRES_USER` are
required (the Postgres tests skip without them); `POSTGRES_PORT` defaults
to `5432`, `POSTGRES_DB` to the user name, `POSTGRES_PASSWORD` to none.
The connection always uses `sslmode=disable`.

```bash
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=contexthelp
POSTGRES_PASSWORD=test_password
POSTGRES_DB=contexthelp_test
go test -tags=integration,fts5 ./internal/storage/postgres/...
```

---

## Related

- [../development.md](../development.md#debugging) — Development guide
- [../scaling.md](../scaling.md#monitoring--observability) — Production monitoring
