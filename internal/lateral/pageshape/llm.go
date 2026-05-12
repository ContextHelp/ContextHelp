package pageshape

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// LLM is the lateral-side abstraction for an LLM that maps a page Input to a
// list of Labels. Implementations adapt a concrete client (kit/ai/llm.Completer
// in production, a fake in tests). Decoupled from the kit client so the
// pageshape package stays domain-focused.
type LLM interface {
	Classify(ctx context.Context, in Input) ([]Label, error)
}

// LLMClassifier composes an LLM with a RecipeCache: cache hit short-circuits
// the LLM call; cache miss calls the LLM and stores the verdict keyed by the
// URL's normalized (domain, pattern) tuple. The Heuristic layer (T21) is
// expected to run BEFORE this — when its result is empty, the caller
// delegates here.
type LLMClassifier struct {
	llm   LLM
	cache RecipeCache
}

// NewLLMClassifier wires a classifier to its LLM and cache.
func NewLLMClassifier(llm LLM, cache RecipeCache) *LLMClassifier {
	return &LLMClassifier{llm: llm, cache: cache}
}

// Classify returns the labels for in. On cache miss the LLM is consulted and
// its verdict is stored before returning. LLM errors propagate; cache errors
// don't exist (the in-memory adapter is infallible; a future on-disk adapter
// would surface IO errors via Get/Put which currently swallow them). Empty
// verdicts are NOT cached — the LLM may have failed transiently or genuinely
// had no opinion; either way, we want the next call to retry rather than
// short-circuit on a sticky empty result.
func (c *LLMClassifier) Classify(ctx context.Context, in Input) ([]Label, error) {
	domain, pattern := normalize(in.URL)
	if v, ok := c.cache.Get(ctx, domain, pattern); ok {
		return v, nil
	}
	v, err := c.llm.Classify(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(v) > 0 {
		c.cache.Put(ctx, domain, pattern, v)
	}
	return v, nil
}

// idRe matches path segments that look like opaque IDs: pure numeric, hex
// hashes 16+ chars, or slug-with-trailing-numeric where the numeric tail is
// at least 3 digits (so version-style slugs like "windows-10" or "gpt-4"
// survive). Such segments collapse to "*" so distinct objects under the
// same template share a recipe entry.
var idRe = regexp.MustCompile(`^[0-9]+$|^[0-9a-f-]{16,}$|^[a-z0-9-]+-[0-9]{3,}$`)

// normalize splits rawURL into (host, id-stripped path pattern). Path
// segments matching idRe become "*" so /posts/123/comments and
// /posts/456/comments share the cache key.
func normalize(rawURL string) (domain, pattern string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, ""
	}
	domain = u.Host
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if p != "" && idRe.MatchString(strings.ToLower(p)) {
			parts[i] = "*"
		}
	}
	pattern = strings.Join(parts, "/")
	return domain, pattern
}
