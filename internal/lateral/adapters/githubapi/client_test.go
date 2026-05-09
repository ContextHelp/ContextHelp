package githubapi

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
)

type stubFetcher struct{}

func (stubFetcher) Get(_ context.Context, _ string) (github.FetchResult, error) {
	return github.FetchResult{Status: 200, Body: []byte("[]"), ContentType: "application/json"}, nil
}

func TestNewREST_SatisfiesAPIClient(t *testing.T) {
	c := NewREST(stubFetcher{}, Options{})
	if c == nil {
		t.Fatal("NewREST returned nil")
	}
	// Compile-time check: c must satisfy github.APIClient.
	var _ github.APIClient = c
}

func TestNewREST_DefaultBaseURL(t *testing.T) {
	// Empty BaseURL falls through to NewHTTPAPIClient's default. We
	// exercise via a method that hits the base path; with the stub
	// fetcher returning empty JSON, a successful call confirms the
	// URL was constructed.
	c := NewREST(stubFetcher{}, Options{})
	if _, err := c.ListRepoSiblings(context.Background(), "octocat", ""); err != nil {
		t.Fatalf("ListRepoSiblings err = %v", err)
	}
}

func TestDeferredGraphQLMethods_ReturnNil(t *testing.T) {
	c := NewREST(stubFetcher{}, Options{})
	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{"ListOwnerPinned", func() error {
			out, err := c.ListOwnerPinned(ctx, "octocat")
			if out != nil {
				t.Errorf("ListOwnerPinned = %v; want nil (deferred)", out)
			}
			return err
		}},
		{"ListSponsored", func() error {
			out, err := c.ListSponsored(ctx, "octocat")
			if out != nil {
				t.Errorf("ListSponsored = %v; want nil (deferred)", out)
			}
			return err
		}},
		{"ListSimilarSponsors", func() error {
			out, err := c.ListSimilarSponsors(ctx, "octocat")
			if out != nil {
				t.Errorf("ListSimilarSponsors = %v; want nil (deferred)", out)
			}
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Errorf("%s err = %v; want nil (deferred surface)", tc.name, err)
			}
		})
	}
}

func TestDeferredGraphQLMethods_NameSliceMatches(t *testing.T) {
	want := map[string]bool{
		"ListOwnerPinned":    true,
		"ListSponsored":      true,
		"ListSimilarSponsors": true,
	}
	if len(DeferredGraphQLMethods) != len(want) {
		t.Fatalf("DeferredGraphQLMethods = %v; want %v", DeferredGraphQLMethods, want)
	}
	for _, m := range DeferredGraphQLMethods {
		if !want[m] {
			t.Errorf("unexpected method %q in DeferredGraphQLMethods", m)
		}
	}
}
