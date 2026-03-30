package ranking

import (
	"strings"
	"unicode"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// WordOverlapScorer scores a KnowledgeObject against a query using word overlap.
// It always scores against the shared IndexProjection (FTSBody + tags + mentions),
// never against flat KO fields directly.
type WordOverlapScorer struct{}

// NewWordOverlapScorer returns a ready-to-use WordOverlapScorer.
func NewWordOverlapScorer() *WordOverlapScorer {
	return &WordOverlapScorer{}
}

// Score returns the fraction of unique query tokens that appear in the object's
// index projection (FTSBody + tag labels + mention strings). Returns 0.0 when
// the query is empty or there is no overlap; 1.0 when all query terms match.
func (s *WordOverlapScorer) Score(query string, ko *storage.KnowledgeObject) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 0.0
	}

	idx := projection.ProjectIndex(ko)

	// Build corpus from FTSBody, tag labels, and mention strings.
	var corpusParts []string
	if idx.FTSBody != "" {
		corpusParts = append(corpusParts, idx.FTSBody)
	}
	for _, t := range idx.Tags {
		if t.Label != "" {
			corpusParts = append(corpusParts, t.Label)
		}
	}
	for _, m := range idx.Mentions {
		if m != "" {
			corpusParts = append(corpusParts, m)
		}
	}

	if len(corpusParts) == 0 {
		return 0.0
	}

	corpusTokens := tokenizeSet(strings.Join(corpusParts, " "))
	if len(corpusTokens) == 0 {
		return 0.0
	}

	matched := 0
	for _, qt := range queryTokens {
		if _, ok := corpusTokens[qt]; ok {
			matched++
		}
	}

	return float64(matched) / float64(len(queryTokens))
}

// tokenize lowercases s, splits on non-alphanumeric characters, deduplicates,
// and returns the unique tokens in stable order (first-occurrence).
func tokenize(s string) []string {
	s = strings.ToLower(s)
	seen := make(map[string]struct{})
	var tokens []string
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if f == "" {
			continue
		}
		if _, exists := seen[f]; !exists {
			seen[f] = struct{}{}
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// tokenizeSet is like tokenize but returns a set (map) for O(1) lookup.
func tokenizeSet(s string) map[string]struct{} {
	s = strings.ToLower(s)
	result := make(map[string]struct{})
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if f != "" {
			result[f] = struct{}{}
		}
	}
	return result
}
