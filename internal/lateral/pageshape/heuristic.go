package pageshape

import "strings"

// Label is a page-shape classification label. Values correspond to the
// shape-keyed strategies in the lateral design (ProductPageStrategy etc.,
// minus the "Strategy" suffix). T22 and T23 consume these constants
// directly; never compare against bare string literals.
type Label string

const (
	LabelPricingPage    Label = "PricingPage"
	LabelAboutPage      Label = "AboutPage"
	LabelProductPage    Label = "ProductPage"
	LabelEcommerceStore Label = "EcommerceStore"
	LabelBlog           Label = "Blog"
	LabelJobPosting     Label = "JobPosting"
	LabelEventPage      Label = "EventPage"
	// LLM- or recipe-cache-only labels (no heuristic rule today;
	// produced by classifier.LLM or future heuristics).
	LabelDocsPage      Label = "DocsPage"
	LabelLandingPage   Label = "LandingPage"
	LabelBlogPost      Label = "BlogPost"
	LabelBusiness      Label = "Business"
	LabelResearchPaper Label = "ResearchPaper"
)

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

// Classify returns the deduplicated list of page-shape labels matched by
// URL or HTML signatures. Order is determined by rule firing, not by
// confidence. A label is included at most once even if multiple rules
// fire for it (e.g. a Shopify product page with JSON-LD @type:product
// hits both rule #3 and rule #5 but appears once).
//
// When Classify returns an empty slice, downstream layers (recipe cache,
// LLM fallback) take over.
func (h *Heuristic) Classify(in Input) []Label {
	var out []Label
	seen := map[Label]bool{}
	add := func(l Label) {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	url := strings.ToLower(in.URL)
	html := strings.ToLower(in.HTML)

	if strings.Contains(url, "/pricing") {
		add(LabelPricingPage)
	}
	if strings.HasSuffix(url, "/about") || strings.HasSuffix(url, "/team") || strings.HasSuffix(url, "/company") {
		add(LabelAboutPage)
	}
	if strings.Contains(html, "cdn.shopify.com") || strings.Contains(html, "myshopify.com") {
		if strings.Contains(url, "/products/") {
			add(LabelProductPage)
		} else {
			add(LabelEcommerceStore)
		}
	}
	if strings.Contains(html, `type="application/rss+xml"`) || strings.Contains(html, `type='application/rss+xml'`) {
		add(LabelBlog)
	}
	if strings.Contains(html, `"@type":"product"`) || strings.Contains(html, `"@type": "product"`) {
		add(LabelProductPage)
	}
	if strings.Contains(html, `"@type":"jobposting"`) {
		add(LabelJobPosting)
	}
	if strings.Contains(html, `"@type":"event"`) {
		add(LabelEventPage)
	}
	return out
}
