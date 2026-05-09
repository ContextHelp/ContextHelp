package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// TestIntegration_CaptureSamberLo is the spec-named acceptance test:
// capture https://github.com/samber/lo, register the github family,
// dispatch via the substrate registry, run probe end-to-end, and
// assert sibling + owner candidates are emitted with the expected
// identity keys.
//
// The test stands up a recorded fixture set covering every API call
// the parent strategy makes for /<owner>/<repo>, then verifies:
//
//  1. The dispatcher selects GitHubStrategy (parent claims github.com).
//  2. probeRepo emits the four REST-reachable candidate types
//     (sibling/owner/sponsor/starred). pinned_repo requires GraphQL —
//     HTTPAPIClient.ListOwnerPinned returns nil/nil per spec, so this
//     end-to-end test does not exercise the pinned path. Daemon
//     adapters with GraphQL access plug in here without changing the
//     strategy code.
//  3. Each candidate carries a parseable @github.* identity key.
//  4. The captured repo (samber/lo) is NOT itself in the sibling set.
//  5. Owner candidate URL is canonical (https://github.com/samber).
//
// This is the closest analog to a recorded-fixture integration test
// the package supports without xrr; when xrr is available the same
// cassette layer (Fetcher injection point) records and replays.
func TestIntegration_CaptureSamberLo(t *testing.T) {
	mux := http.NewServeMux()

	// /users/samber/repos
	mux.HandleFunc("/users/samber/repos", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4990")
		_, _ = w.Write([]byte(`[
			{"name":"lo","html_url":"https://github.com/samber/lo","stargazers_count":12000,"language":"Go","owner":{"login":"samber"}},
			{"name":"do","html_url":"https://github.com/samber/do","stargazers_count":1500,"language":"Go","owner":{"login":"samber"}},
			{"name":"mo","html_url":"https://github.com/samber/mo","stargazers_count":800,"language":"Go","owner":{"login":"samber"}}
		]`))
	})

	// /users/samber/starred
	mux.HandleFunc("/users/samber/starred", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"thanos","html_url":"https://github.com/thanos-io/thanos","owner":{"login":"thanos-io","type":"Organization"}},
			{"name":"prometheus","html_url":"https://github.com/prometheus/prometheus","owner":{"login":"prometheus","type":"Organization"}}
		]`))
	})

	// /sponsors/samber — sponsor page exists.
	mux.HandleFunc("/sponsors/samber", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Compose: API + sponsor page both behind the same test server.
	f := &integrationFetcher{base: srv.URL, inner: newHTTPFetcher()}
	c := NewHTTPAPIClient(f, srv.URL)

	// Wire substrate registry with all three github strategies.
	reg := lateral.NewRegistry()
	reg.Register(NewGitHubStrategy(Dependencies{APIClient: c}))
	reg.Register(NewGistStrategy(Dependencies{APIClient: c}))
	reg.Register(NewSecurityAdvisoryStrategy(Dependencies{APIClient: c}))

	// Dispatch.
	ev := lateral.CapturedEvent{
		ObjectID:  "test-object-1",
		Namespace: "code.github.repository",
		SourceURL: "https://github.com/samber/lo",
	}
	dispatched := reg.Dispatch(context.Background(), ev)
	if len(dispatched) != 1 {
		t.Fatalf("dispatched %d strategies, want 1", len(dispatched))
	}
	if got, want := dispatched[0].ID(), StrategyIDGitHub; got != want {
		t.Errorf("dispatched %q, want %q (parent must claim plain github.com)", got, want)
	}

	// Probe.
	cands, err := dispatched[0].Probe(context.Background(), ev, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	// Acceptance: every required candidate type is present.
	for _, want := range []string{
		TypeSiblingRepo, TypeOwnerProfile, TypeSponsorPage, TypeStarredRepo,
	} {
		if !hasType(cands, want) {
			t.Errorf("integration: missing %q in %v", want, candidateTypes(cands))
		}
	}

	// Acceptance: captured repo is NOT in the sibling set.
	for _, c := range cands {
		if c.CandidateType != TypeSiblingRepo {
			continue
		}
		if got := c.Preview["name"]; got == "lo" {
			t.Error("integration: captured repo lo must be excluded from siblings")
		}
	}

	// Acceptance: identity keys parse for every github-resource candidate.
	for _, c := range cands {
		switch c.CandidateType {
		case TypeOwnerProfile, TypeSiblingRepo, TypePinnedRepo, TypeStarredRepo, TypeSponsorPage:
			key := ExtractIdentityKey(c)
			if key == "" {
				t.Errorf("integration: candidate %q missing identity_key", c.CandidateType)
				continue
			}
			parsed := ParseIdentityKey(key)
			if parsed.Kind == IdentityKeyKindUnknown {
				t.Errorf("integration: candidate %q identity_key %q parses as unknown",
					c.CandidateType, key)
			}
		}
	}

	// Acceptance: owner candidate URL is canonical.
	owner := findFirst(cands, TypeOwnerProfile)
	if owner == nil {
		t.Fatal("integration: missing owner_profile candidate")
	}
	if got, want := owner.URL, "https://github.com/samber"; got != want {
		t.Errorf("integration: owner URL = %q, want %q", got, want)
	}

	// Acceptance: at least 2 sibling candidates (do + mo from fixture).
	siblings := 0
	for _, c := range cands {
		if c.CandidateType == TypeSiblingRepo {
			siblings++
		}
	}
	if siblings < 2 {
		t.Errorf("integration: got %d siblings, want >= 2", siblings)
	}
}

// integrationFetcher routes both API calls (https://api.github.com/...)
// and sponsor pages (https://github.com/sponsors/...) to the test
// server. Real github URLs in the strategy code remain unchanged; the
// Fetcher rewrites them at the boundary.
type integrationFetcher struct {
	base  string
	inner *httpFetcher
}

func (f *integrationFetcher) Get(ctx context.Context, url string) (FetchResult, error) {
	url = strings.Replace(url, "https://api.github.com", f.base, 1)
	url = strings.Replace(url, "https://github.com", f.base, 1)
	return f.inner.Get(ctx, url)
}

func (f *integrationFetcher) LastResponseHeaders() (http.Header, error) {
	return f.inner.LastResponseHeaders()
}
