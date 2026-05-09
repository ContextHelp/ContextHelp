package pageshape

import (
	"slices"
	"testing"
)

func TestHeuristic_PricingURL(t *testing.T) {
	h := NewHeuristic()
	got := h.Classify(Input{URL: "https://example.com/pricing"})
	if !slices.Contains(got, LabelPricingPage) {
		t.Fatalf("expected PricingPage, got %v", got)
	}
}

func TestHeuristic_ShopifyProduct(t *testing.T) {
	h := NewHeuristic()
	html := `<html><script src="//cdn.shopify.com/x.js"></script></html>`
	got := h.Classify(Input{URL: "https://store.com/products/widget", HTML: html})
	if !slices.Contains(got, LabelProductPage) {
		t.Fatalf("expected ProductPage, got %v", got)
	}
}

func TestHeuristic_RSSImpliesBlog(t *testing.T) {
	h := NewHeuristic()
	html := `<head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head>`
	got := h.Classify(Input{URL: "https://blog.example.com/", HTML: html})
	if !slices.Contains(got, LabelBlog) {
		t.Fatalf("expected Blog, got %v", got)
	}
}

func TestHeuristic_DedupesProductPageLabel(t *testing.T) {
	h := NewHeuristic()
	html := `<html>
<script src="//cdn.shopify.com/x.js"></script>
<script type="application/ld+json">{"@type":"Product","name":"x"}</script>
</html>`
	got := h.Classify(Input{URL: "https://store.com/products/widget", HTML: html})
	count := 0
	for _, l := range got {
		if l == LabelProductPage {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 ProductPage label, got %d (full: %v)", count, got)
	}
}
