package search

import (
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
)

// searchStopwords is the set of common English words to strip before embedding.
var searchStopwords = wordSet(`
	the a an and or but in on at to for of with by from is are was
	were be been have has had do does did will would could should
	what how why when where which who it this that these those i you
	we they my your best good way ways
`)

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

// wordSet builds a lookup set from whitespace-separated words.
func wordSet(words string) map[string]bool {
	fields := strings.Fields(words)
	set := make(map[string]bool, len(fields))
	for _, w := range fields {
		set[w] = true
	}
	return set
}
