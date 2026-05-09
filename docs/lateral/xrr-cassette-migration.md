# xrr cassette migration plan

**Status**: deferred — xrr binary not yet available in the build path.

## Why we deferred

P3 (`internal/lateral/strategies/github`) and P4 (`internal/lateral/
strategies/roster` — google, x, linkedin, arxiv, wikipedia, medium,
substack, beehiiv, youtube) tests use in-memory `fixtureFetcher` /
`stub*Client` shims today. Those shims simulate the upstream APIs
well enough for unit-level coverage but they:

- can drift silently from the real APIs (a payload schema change
  upstream doesn't break our tests);
- duplicate the wire format every test author has to reverse-engineer;
- can't catch parsing regressions in our own code that only surface
  on real responses.

[xrr](https://github.com/hop-top/xrr) is the project's standard
cassette tool: it intercepts HTTP / gRPC / SQL / Redis traffic and
records replay-able cassettes. T-0334 was meant to migrate the
fetchers above to xrr, but xrr isn't yet on the engineering path
(`which xrr` returns nothing).

## Migration shape

When xrr lands, this is the migration:

### Per-strategy adapter

Each strategy package has a `Fetcher` (or `*Client`) interface that
production wires to a real HTTP client and tests wire to a stub.
Replace the stub with `xrr.NewHTTPClient(cassettePath)`:

```go
// before:
fetcher := &fixtureFetcher{fixtures: map[string][]byte{...}}

// after:
client, err := xrr.NewHTTPClient("testdata/cassettes/github/list_repos.cassette")
if err != nil { t.Fatal(err) }
fetcher := github.NewHTTPFetcher(client)
```

### Recording new cassettes

Cassettes are recorded once against a real API key, then committed
to the repo. Workflow:

```bash
GITHUB_TOKEN=... xrr record \
  -o internal/lateral/strategies/github/testdata/cassettes/list_repos.cassette \
  -- go test -run TestGitHubStrategy_ListRepos ./internal/lateral/strategies/github/
```

xrr captures request + response shape. Tokens / cookies are scrubbed
on save (xrr's default sanitisers cover `Authorization`, `Cookie`,
`X-API-Key`).

### Strategy-by-strategy migration

| Strategy | Stub today | Cassette path on migration |
|----------|-----------|----------------------------|
| github (parent / gist / advisory) | `internal/lateral/strategies/github/cassette_test.go` `fixtureFetcher` | `internal/lateral/strategies/github/testdata/cassettes/<endpoint>.cassette` |
| google (parent + 4 children) | `internal/lateral/strategies/roster/cassettes_test.go` stub `GoogleClient` | `internal/lateral/strategies/google/testdata/cassettes/<endpoint>.cassette` |
| arxiv | stub `ArxivClient` | `internal/lateral/strategies/arxiv/testdata/cassettes/...` |
| wikipedia | stub `WikipediaClient` | `internal/lateral/strategies/wikipedia/testdata/cassettes/...` |
| medium / substack / beehiiv | stub `*Client` per family | one `testdata/cassettes/` dir per strategy package |
| youtube | stub `YouTubeClient` | `internal/lateral/strategies/youtube/testdata/cassettes/...` |
| x / linkedin | URL-structural — no fetcher | not applicable |
| jit | LLM; xrr could capture HTTP transport but reasoning is non-deterministic | migrate independently after deterministic-LLM work |

### Order

Migrate platforms with stable schemas first (arxiv, wikipedia) because
their cassettes age slowest. Defer YouTube + Google until those APIs
have a stable rate-limit posture for cassette refresh.

## What ships with T-0334

- This document — operator-facing migration guide.
- The existing `fixtureFetcher` / stub clients stay in place; no
  `replace_all` rewrite is attempted.
- A test-helper file in each strategy package that exposes the
  cassette-load shape (commented-out). When xrr lands, uncomment +
  delete the stub.

The placeholder helpers are net-zero: they compile but don't run any
new test logic until cassettes exist. They give the strategy package
the integration seam ready for the migration PR.

## Verification when xrr lands

1. `which xrr` returns a binary.
2. Run one strategy through the migration above (suggest arxiv —
   simplest schema).
3. Open a PR that:
   - Records the cassettes,
   - Replaces the stub client with xrr.NewHTTPClient,
   - Drops the now-unused stub fixture data.
4. Use that PR as the template for the rest.

Until then, the existing fixture-driven tests cover the same shape
they always did.
