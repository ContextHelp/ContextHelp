package pageshape

import (
	"context"
	"slices"
	"testing"
)

type fakeLLM struct {
	calls   int
	verdict []Label
}

func (l *fakeLLM) Classify(_ context.Context, _ Input) ([]Label, error) {
	l.calls++
	return l.verdict, nil
}

func TestLLMClassifier_FirstCallHitsLLM_SecondHitsRecipe(t *testing.T) {
	llm := &fakeLLM{verdict: []Label{LabelDocsPage}}
	cache := NewMemoryRecipeCache()
	c := NewLLMClassifier(llm, cache)

	in := Input{URL: "https://docs.example.com/guide", HTML: "<html>"}
	v1, err := c.Classify(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(v1, LabelDocsPage) || llm.calls != 1 {
		t.Fatalf("first call: verdict=%v, llm calls=%d", v1, llm.calls)
	}

	v2, err := c.Classify(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(v2, LabelDocsPage) || llm.calls != 1 {
		t.Fatalf("second call should hit cache: verdict=%v, llm calls=%d", v2, llm.calls)
	}
}

func TestLLMClassifier_DifferentURLPatternsBypassCache(t *testing.T) {
	// Different domains should hit the LLM separately.
	llm := &fakeLLM{verdict: []Label{LabelDocsPage}}
	cache := NewMemoryRecipeCache()
	c := NewLLMClassifier(llm, cache)

	_, _ = c.Classify(context.Background(), Input{URL: "https://docs.a.com/g", HTML: ""})
	_, _ = c.Classify(context.Background(), Input{URL: "https://docs.b.com/g", HTML: ""})
	if llm.calls != 2 {
		t.Fatalf("different domains should each trigger LLM: got %d calls", llm.calls)
	}
}

func TestLLMClassifier_EmptyVerdictNotCached(t *testing.T) {
	llm := &fakeLLM{verdict: nil} // returns nil verdict, nil err
	cache := NewMemoryRecipeCache()
	c := NewLLMClassifier(llm, cache)

	in := Input{URL: "https://docs.example.com/guide", HTML: "<html>"}
	_, _ = c.Classify(context.Background(), in)
	_, _ = c.Classify(context.Background(), in)
	if llm.calls != 2 {
		t.Fatalf("expected LLM called 2x (empty not cached), got %d", llm.calls)
	}
}

func TestNormalize_VersionStyleSlugsSurvive(t *testing.T) {
	cases := []struct {
		url     string
		pattern string
	}{
		{"https://docs.example.com/blog/windows-10", "/blog/windows-10"},
		{"https://docs.example.com/blog/react-19", "/blog/react-19"},
		{"https://docs.example.com/blog/gpt-4", "/blog/gpt-4"},
		{"https://docs.example.com/blog/claude-4-7", "/blog/claude-4-7"},
		// But long numeric tails still collapse:
		{"https://docs.example.com/posts/post-12345", "/posts/*"},
	}
	for _, c := range cases {
		_, got := normalize(c.url)
		if got != c.pattern {
			t.Errorf("normalize(%q) = %q, want %q", c.url, got, c.pattern)
		}
	}
}

func TestNormalize_StripsNumericIDsToWildcard(t *testing.T) {
	domain, pattern := normalize("https://example.com/posts/12345/comments")
	if domain != "example.com" {
		t.Fatalf("domain: got %q", domain)
	}
	// "12345" is a numeric ID; replaced with "*". "posts" and "comments" keep.
	if pattern != "/posts/*/comments" {
		t.Fatalf("pattern: got %q want /posts/*/comments", pattern)
	}
}

func TestRecipeCache_InvalidateRemovesEntry(t *testing.T) {
	cache := NewMemoryRecipeCache()
	cache.Put(context.Background(), "x.com", "/y", []Label{"Blog"})
	if _, ok := cache.Get(context.Background(), "x.com", "/y"); !ok {
		t.Fatal("expected hit before invalidate")
	}
	cache.Invalidate(context.Background(), "x.com", "/y")
	if _, ok := cache.Get(context.Background(), "x.com", "/y"); ok {
		t.Fatal("expected miss after invalidate")
	}
}
