package pageshape

import "strings"

// Input is what the heuristic classifier receives. URL is required; HTML may
// be empty (URL-only heuristics still apply).
type Input struct {
	URL  string
	HTML string
}

// Heuristic is the cheapest layer of the page-shape classification pipeline.
// It applies signature-based pattern matching on URL paths and HTML markers
// (Schema.org JSON-LD, RSS link tags, CDN footprints). Returns zero or more
// labels — multiple labels are valid (e.g. a product page on Shopify hits
// both ProductPage and Shopify hints in the same Classify call).
//
// When Classify returns an empty slice, downstream layers (recipe cache, LLM
// fallback) take over.
type Heuristic struct{}

// NewHeuristic constructs a stateless Heuristic. Currently no configuration;
// the constructor exists so future state (compiled regexes, custom rule
// sets) can be added without an API break.
func NewHeuristic() *Heuristic { return &Heuristic{} }

// Classify returns the list of page-shape labels matched by URL or HTML
// signatures. Order is determined by rule firing, not by confidence.
// Caller is responsible for deduping if a label can fire from multiple
// rules (current rules avoid intra-call duplicates).
func (h *Heuristic) Classify(in Input) []string {
	var out []string
	url := strings.ToLower(in.URL)
	html := strings.ToLower(in.HTML)

	if strings.Contains(url, "/pricing") {
		out = append(out, "PricingPage")
	}
	if strings.HasSuffix(url, "/about") || strings.HasSuffix(url, "/team") || strings.HasSuffix(url, "/company") {
		out = append(out, "AboutPage")
	}
	if strings.Contains(html, "cdn.shopify.com") || strings.Contains(html, "myshopify.com") {
		if strings.Contains(url, "/products/") {
			out = append(out, "ProductPage")
		} else {
			out = append(out, "EcommerceStore")
		}
	}
	if strings.Contains(html, `type="application/rss+xml"`) || strings.Contains(html, `type='application/rss+xml'`) {
		out = append(out, "Blog")
	}
	if strings.Contains(html, `"@type":"product"`) || strings.Contains(html, `"@type": "product"`) {
		out = append(out, "ProductPage")
	}
	if strings.Contains(html, `"@type":"jobposting"`) {
		out = append(out, "JobPosting")
	}
	if strings.Contains(html, `"@type":"event"`) {
		out = append(out, "EventPage")
	}
	return out
}
