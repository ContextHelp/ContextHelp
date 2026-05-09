package pageshape

import "testing"

func TestHeuristic_PricingURL(t *testing.T) {
	h := NewHeuristic()
	got := h.Classify(Input{URL: "https://example.com/pricing"})
	if !contains(got, "PricingPage") {
		t.Fatalf("expected PricingPage, got %v", got)
	}
}

func TestHeuristic_ShopifyProduct(t *testing.T) {
	h := NewHeuristic()
	html := `<html><script src="//cdn.shopify.com/x.js"></script></html>`
	got := h.Classify(Input{URL: "https://store.com/products/widget", HTML: html})
	if !contains(got, "ProductPage") {
		t.Fatalf("expected ProductPage, got %v", got)
	}
}

func TestHeuristic_RSSImpliesBlog(t *testing.T) {
	h := NewHeuristic()
	html := `<head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head>`
	got := h.Classify(Input{URL: "https://blog.example.com/", HTML: html})
	if !contains(got, "Blog") {
		t.Fatalf("expected Blog, got %v", got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
