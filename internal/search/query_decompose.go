package search

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
)

// searchStopwords is the set of common English words to strip before embedding.
var searchStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "what": true, "how": true, "why": true, "when": true,
	"where": true, "which": true, "who": true, "it": true, "this": true, "that": true,
	"these": true, "those": true, "i": true, "you": true, "we": true, "they": true,
	"my": true, "your": true, "best": true, "good": true, "way": true, "ways": true,
}

// DecomposeQuery strips stopwords and short tokens from a natural-language query.
// Returns filtered terms joined by space, or the original query if fewer than
// minTerms remain after filtering.
func DecomposeQuery(query string, minTerms int) string {
	tokens := tokenize(query)
	var terms []string
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		if len(tok) >= 3 && !searchStopwords[lower] {
			terms = append(terms, tok)
		}
	}
	if len(terms) < minTerms {
		return query
	}
	return strings.Join(terms, " ")
}

// tokenize splits s on non-alphanumeric rune boundaries, preserving original case.
func tokenize(s string) []string {
	return ftsq.Tokenize(s)
}
