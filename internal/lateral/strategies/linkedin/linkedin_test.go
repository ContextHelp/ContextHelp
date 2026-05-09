package linkedin

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

func TestApplies(t *testing.T) {
	cases := []struct {
		url     string
		matches bool
	}{
		{"https://www.linkedin.com/in/foo", true},
		{"https://linkedin.com/company/bar", true},
		{"https://business.linkedin.com/talent", true},
		{"https://example.com/in/foo", false},
	}
	s := New()
	for _, tc := range cases {
		if got := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url}); got.Matches != tc.matches {
			t.Errorf("%s: matches=%v want=%v", tc.url, got.Matches, tc.matches)
		}
	}
}

func TestProbe(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantType string
	}{
		{"profile", "https://www.linkedin.com/in/jadbitar", CandidateTypeProfile},
		{"company", "https://www.linkedin.com/company/anthropic", CandidateTypeOrg},
		{"school", "https://www.linkedin.com/school/mit", CandidateTypeSchool},
		{"post", "https://www.linkedin.com/posts/jadbitar_hello-activity-1234", CandidateTypePost},
		{"pulse", "https://www.linkedin.com/pulse/some-slug", CandidateTypeArticle},
	}
	s := New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cands, err := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: tc.url}, lateral.ActiveContext{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range cands {
				if c.CandidateType == tc.wantType {
					found = true
				}
				if c.Preview["identity_key"] == nil || c.Preview["identity_key"].(string) == "" {
					t.Fatalf("candidate %s missing identity_key", c.CandidateType)
				}
			}
			if !found {
				t.Fatalf("expected %s in %v", tc.wantType, types(cands))
			}
		})
	}
}

func TestProbe_PostEmitsAuthorProfile(t *testing.T) {
	s := New()
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://www.linkedin.com/posts/jadbitar_some-activity-9999",
	}, lateral.ActiveContext{})
	hasProfile := false
	for _, c := range cands {
		if c.CandidateType == CandidateTypeProfile {
			hasProfile = true
		}
	}
	if !hasProfile {
		t.Fatal("post probe should also propose author profile")
	}
}

func TestProbe_UnknownPathReturnsEmpty(t *testing.T) {
	s := New()
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://www.linkedin.com/jobs/view/12345",
	}, lateral.ActiveContext{})
	if len(cands) != 0 {
		t.Fatalf("unsupported path should be no-op; got %d", len(cands))
	}
}

func types(cs []lateral.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CandidateType)
	}
	return out
}
