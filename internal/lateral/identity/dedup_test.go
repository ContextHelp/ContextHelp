package identity

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// TestResolver_DedupAcrossMobileAndCustomDomain captures the canonical
// real-world dedup case: one logical Medium article surfaced via three
// URL skews (mobile host, www host, custom domain) all carrying the same
// identity_key. Resolver must collapse them onto a single canonical.
func TestResolver_DedupAcrossMobileAndCustomDomain(t *testing.T) {
	key := identitykey.Build("medium", identitykey.EntityArticle, "user", "article-slug")
	if key == "" {
		t.Fatal("identitykey.Build returned empty for non-degenerate inputs")
	}
	g := &fakeGraph{byKey: map[string]string{key: "o-medium-article"}}
	r := NewResolver(g)

	urls := []string{
		"https://m.medium.com/@user/article-slug",
		"https://medium.com/@user/article-slug",
		"https://customblog.example.com/article-slug",
	}
	for _, u := range urls {
		res, err := r.Resolve(context.Background(), Candidate{
			URL:     u,
			Preview: preview(key),
		})
		if err != nil {
			t.Fatalf("resolve %s: %v", u, err)
		}
		if !res.EdgeOnly || res.CanonicalID != "o-medium-article" {
			t.Fatalf("URL %q: expected o-medium-article, got %+v", u, res)
		}
	}
}

// TestResolver_DistinctLocalesDoNotMerge mirrors the dedup case in the
// negative direction: two Wikipedia articles about the same concept but
// in different language editions are different entities — the locale
// segment of the identity_key MUST keep them apart even when the canonical
// English title equals the URL slug across both.
func TestResolver_DistinctLocalesDoNotMerge(t *testing.T) {
	en := identitykey.BuildLocalised("wikipedia", identitykey.EntityArticle, "en", "turing_machine")
	fr := identitykey.BuildLocalised("wikipedia", identitykey.EntityArticle, "fr", "machine_de_turing")
	if en == "" || fr == "" || en == fr {
		t.Fatalf("identitykey.BuildLocalised malformed: en=%q fr=%q", en, fr)
	}
	g := &fakeGraph{byKey: map[string]string{
		en: "o-en-article",
		fr: "o-fr-article",
	}}
	r := NewResolver(g)

	resEN, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://en.wikipedia.org/wiki/Turing_machine",
		Preview: preview(en),
	})
	if err != nil {
		t.Fatal(err)
	}
	resFR, err := r.Resolve(context.Background(), Candidate{
		URL:     "https://fr.wikipedia.org/wiki/Machine_de_Turing",
		Preview: preview(fr),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resEN.CanonicalID != "o-en-article" || resFR.CanonicalID != "o-fr-article" {
		t.Fatalf("locale split broken: en=%+v fr=%+v", resEN, resFR)
	}
	if resEN.CanonicalID == resFR.CanonicalID {
		t.Fatalf("different localised identity_keys must not merge")
	}
}

// TestResolver_GitHubOwnerAcrossHostSkew covers a second platform shape:
// owner profile URLs reached via three host variants (api.github, mobile,
// canonical web) all carrying the github/owner/<login> key. Confirms the
// dedup pattern is platform-agnostic.
func TestResolver_GitHubOwnerAcrossHostSkew(t *testing.T) {
	key := identitykey.Build("github", identitykey.EntityOwner, "samber")
	g := &fakeGraph{byKey: map[string]string{key: "o-samber"}}
	r := NewResolver(g)

	urls := []string{
		"https://github.com/samber",
		"https://api.github.com/users/samber",
		"https://github.com/samber/",
	}
	for _, u := range urls {
		res, err := r.Resolve(context.Background(), Candidate{
			URL:     u,
			Preview: preview(key),
		})
		if err != nil {
			t.Fatalf("resolve %s: %v", u, err)
		}
		if !res.EdgeOnly || res.CanonicalID != "o-samber" {
			t.Fatalf("URL %q: expected o-samber, got %+v", u, res)
		}
	}
}
