package github

import "context"

// Fetcher is the package-local HTTP fetch contract. It is intentionally
// narrow: a single Get(url) returning bytes + status. Daemon code adapts
// its preferred HTTP client (ibr, net/http, recorded fixture) to this
// interface. The strategy never imports the adapter directly so probe
// tests can inject a stub.
//
// Fetchers must be safe for concurrent use; sub-path probes fire in
// parallel.
type Fetcher interface {
	Get(ctx context.Context, url string) (FetchResult, error)
}

// FetchResult captures what the strategy needs from a single GET. Body
// is the raw payload (HTML or JSON depending on URL); Status is the HTTP
// status code; ContentType is the Content-Type header value. Tests stub
// all three.
type FetchResult struct {
	Body        []byte
	Status      int
	ContentType string
}
