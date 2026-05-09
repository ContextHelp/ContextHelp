package jit

import (
	"context"
	"net/url"
	"strings"
)

// Fetcher is the lateral-side abstraction for the host's web fetcher (the
// production wiring is an ibr adapter; tests use a fake). Decoupled from the
// concrete ibr client so the jit package stays domain-focused — the daemon
// (T-0252) wires the real adapter at registration time.
//
// Fetch returns the body of urlStr (or whatever extracted form ibr produces;
// the executor treats it as opaque) and an error. A non-nil error MUST mean
// the resource is unfetchable; an empty body with nil error means "fetched
// but empty" and is a valid candidate-emission signal (executor preserves it
// so downstream T-0247 can decide what to do).
type Fetcher interface {
	Fetch(ctx context.Context, urlStr string) (body string, err error)
}

// FetchResult is one element of Executor.Execute's return: the fully-resolved
// URL the executor actually fetched, the body, and any per-path error.
// Executor returns one FetchResult per accepted sub-path; cross-domain
// absolutes proposed by the LLM are dropped before fetch (see Execute) so
// they don't appear here.
//
// Executor never aborts the whole batch on a single Fetch error. T-0248
// (failure handling) consumes the per-path Err to emit ctxt.lateral.scan.failed
// with Mechanism=jit_proposal; T-0247 (candidate emission) reads Body for
// success cases.
type FetchResult struct {
	URL  string
	Body string
	Err  error
}

// Executor sequentially dispatches a slice of proposed sub-paths through a
// Fetcher, scoped to the source domain. Sequential by default; a bounded
// parallel variant is a follow-on once T-0248's failure semantics are pinned.
//
// Path semantics:
//   - relative paths ("/about", "team", "../foo") are resolved against the
//     source URL as the base
//   - absolute URLs on the same host as the source are kept as-is
//   - absolute URLs on a different host are dropped silently — JIT proposals
//     are by contract same-domain (see proposalPromptTemplate); a cross-domain
//     absolute is an LLM deviation, not a valid lateral candidate
type Executor struct {
	fetcher Fetcher
}

// NewExecutor wires an Executor to its Fetcher.
func NewExecutor(f Fetcher) *Executor {
	return &Executor{fetcher: f}
}

// Execute resolves each sub-path against sourceURL and fetches it via the
// Fetcher. Returns one FetchResult per accepted (same-domain) path. Per-path
// errors are surfaced in FetchResult.Err so callers can apply per-path
// policy; ctx-cancellation aborts remaining fetches.
func (e *Executor) Execute(ctx context.Context, sourceURL string, subpaths []string) []FetchResult {
	if len(subpaths) == 0 {
		return nil
	}

	base, err := url.Parse(sourceURL)
	if err != nil || base.Host == "" {
		// Unparseable source — nothing to resolve against; bail with empty.
		return nil
	}

	results := make([]FetchResult, 0, len(subpaths))
	for _, p := range subpaths {
		if err := ctx.Err(); err != nil {
			break
		}
		resolved, ok := resolveSamehost(base, p)
		if !ok {
			continue
		}
		body, ferr := e.fetcher.Fetch(ctx, resolved)
		results = append(results, FetchResult{URL: resolved, Body: body, Err: ferr})
	}
	return results
}

// resolveSamehost resolves p against base. Returns (resolvedURL, true) if the
// final URL is on the same host as base; (_, false) otherwise (drop signal).
func resolveSamehost(base *url.URL, p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	ref, err := url.Parse(p)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Host != base.Host {
		return "", false
	}
	return resolved.String(), true
}
