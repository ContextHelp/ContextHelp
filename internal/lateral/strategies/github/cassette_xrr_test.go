package github

// This file holds the migration target shape for T-0334 — when xrr
// lands the existing fixtureFetcher in cassette_test.go gets replaced
// with xrr-driven cassette replay. See docs/lateral/xrr-cassette-
// migration.md for the migration plan.
//
// The function below is a no-op until xrr is wired; uncommenting +
// implementing is a single-PR task once the binary is available.

// loadCassette is the migration seam for xrr. Today it returns a
// pre-existing fixtureFetcher; the migration PR replaces the body
// with:
//
//	client, err := xrr.NewHTTPClient(path)
//	if err != nil { return nil, err }
//	return NewHTTPFetcher(client), nil
//
// The Fetcher interface stays the same so callers don't change.
//
// The path is conventionally testdata/cassettes/<endpoint>.cassette
// alongside the strategy package, recorded with `xrr record` against
// a real GITHUB_TOKEN once.
func loadCassette(_ string) Fetcher {
	// Deferred per docs/lateral/xrr-cassette-migration.md — return
	// nil so a future test that calls this helper with an unwired
	// xrr panics loudly rather than silently using stale fixture
	// data.
	return nil
}

// Reference call so the migration seam stays in the build graph; a
// future xrr-enabled PR replaces this stub with a real test that
// exercises a recorded cassette.
var _ = loadCassette
