// Package githubapi wires a daemon-side github API client into the
// github.APIClient interface the github strategy family consumes.
//
// # REST-only for v1
//
// github ships an HTTP reference client (github.HTTPAPIClient) that
// covers most of github.APIClient via REST. Three methods return
// (nil, nil) because they have no REST surface and require GraphQL:
//
//   - ListOwnerPinned   — GraphQL `viewer.pinnedItems`
//   - ListSponsored     — GraphQL `user.sponsoring` connection
//   - ListSimilarSponsors — composite, GraphQL-only
//
// The daemon ships this REST-only adapter for v1 and defers GraphQL to
// a follow-on task. The downstream strategies tolerate the deferred
// surface — github sub-path probes for pinned/sponsored degrade to
// "no candidates" rather than failing.
//
// Future GraphQL adapter (P5 work): wraps a separate githubv4 client
// and overrides the three deferred methods on top of the REST adapter
// via a struct-embed + method shadowing — the wrapper looks like
// `struct{ *RESTClient; gql githubv4.Client }` with method-receivers
// on the wrapper for ListOwnerPinned/ListSponsored/ListSimilarSponsors;
// every other method falls through to the embedded REST client by
// promotion. DeferredGraphQLMethods names the three methods so a future
// composition can assert coverage at boot.
package githubapi

import (
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
)

// Options configures the REST adapter.
type Options struct {
	// BaseURL is the github API base. Empty defaults to
	// "https://api.github.com" (kit/strategy default). Override for
	// GitHub Enterprise Server: "https://github.example.com/api/v3".
	BaseURL string
}

// NewREST constructs a github.APIClient backed by the package's
// HTTPAPIClient (REST-only). f is the http.Fetcher the daemon wires
// (typically the fetcher.HTTPFetcher from T-0315). Returns the
// concrete *github.HTTPAPIClient as the github.APIClient interface.
//
// Caller is responsible for wrapping the result with
// github.NewGuardedAPI when a breaker / floor tracker should gate
// the calls — this package returns the raw client so the daemon
// composes the gating layer once at boot.
func NewREST(f github.Fetcher, opts Options) github.APIClient {
	return github.NewHTTPAPIClient(f, opts.BaseURL)
}

// DeferredGraphQLMethods enumerates the github.APIClient methods that
// HTTPAPIClient returns (nil, nil) for because they have no REST
// surface. Operators triaging "why didn't I get pinned-repo
// candidates?" can grep on these names. Stable string slice; the
// daemon's status command reports against this list.
var DeferredGraphQLMethods = []string{
	"ListOwnerPinned",
	"ListSponsored",
	"ListSimilarSponsors",
}
