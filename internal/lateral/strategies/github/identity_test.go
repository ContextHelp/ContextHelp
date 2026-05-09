package github

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestExtractIdentityKey_PresentAndAbsent(t *testing.T) {
	c := lateral.Candidate{Preview: map[string]any{PreviewKeyIdentityKey: "@github.user.jadb"}}
	if got, want := ExtractIdentityKey(c), "@github.user.jadb"; got != want {
		t.Errorf("ExtractIdentityKey = %q, want %q", got, want)
	}

	if got := ExtractIdentityKey(lateral.Candidate{}); got != "" {
		t.Errorf("absent identity key should yield empty; got %q", got)
	}

	if got := ExtractIdentityKey(lateral.Candidate{Preview: map[string]any{"identity_key": 42}}); got != "" {
		t.Errorf("non-string preview value should yield empty; got %q", got)
	}
}

func TestParseIdentityKey(t *testing.T) {
	cases := []struct {
		key  string
		want ParsedIdentityKey
	}{
		{"@github.repo.samber/lo", ParsedIdentityKey{Kind: IdentityKeyKindRepo, Owner: "samber", Name: "lo"}},
		{"@github.user.jadb", ParsedIdentityKey{Kind: IdentityKeyKindUser, Name: "jadb", Login: "jadb"}},
		{"@github.org.acme", ParsedIdentityKey{Kind: IdentityKeyKindOrg, Name: "acme", Login: "acme"}},
		{"@github.repo.no-slash", ParsedIdentityKey{Kind: IdentityKeyKindUnknown}},
		{"@github.user.", ParsedIdentityKey{Kind: IdentityKeyKindUnknown}},
		{"random", ParsedIdentityKey{Kind: IdentityKeyKindUnknown}},
		{"", ParsedIdentityKey{Kind: IdentityKeyKindUnknown}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			got := ParseIdentityKey(tc.key)
			if got != tc.want {
				t.Errorf("ParseIdentityKey(%q) = %+v, want %+v", tc.key, got, tc.want)
			}
		})
	}
}

func TestIdentityKeyKind_String(t *testing.T) {
	cases := []struct {
		k    IdentityKeyKind
		want string
	}{
		{IdentityKeyKindRepo, "repo"},
		{IdentityKeyKindUser, "user"},
		{IdentityKeyKindOrg, "org"},
		{IdentityKeyKindUnknown, "unknown"},
	}
	for _, tc := range cases {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("kind %d = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestIdentityKeyEmittedByEveryProbeType(t *testing.T) {
	// Property: every Candidate emitted by github strategies that names
	// a github resource (repo/user/org) carries an identity_key. The
	// resolver depends on this contract.
	api := &stubAPIClient{
		listRepoSiblingsFn: func(_ context.Context, _, _ string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: "samber", Name: "do"}}, nil
		},
		listOwnerPinnedFn: func(_ context.Context, _ string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: "samber", Name: "lo"}}, nil
		},
		hasSponsorPageFn: func(_ context.Context, _ string) (bool, error) { return true, nil },
		listOwnerStarredFn: func(_ context.Context, _ string) ([]RepoSummary, error) {
			return []RepoSummary{{Owner: "thanos-io", Name: "thanos"}}, nil
		},
	}
	s := NewGitHubStrategy(Dependencies{APIClient: api})
	got, err := s.Probe(context.Background(),
		lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"},
		lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range got {
		switch c.CandidateType {
		case TypeOwnerProfile, TypeSiblingRepo, TypePinnedRepo, TypeStarredRepo, TypeSponsorPage:
			key := ExtractIdentityKey(c)
			if key == "" {
				t.Errorf("candidate type %q missing identity_key in Preview", c.CandidateType)
			}
			parsed := ParseIdentityKey(key)
			if parsed.Kind == IdentityKeyKindUnknown {
				t.Errorf("candidate type %q identity_key %q parses as unknown", c.CandidateType, key)
			}
		}
	}
}
