package search

import "strings"

// SafeFTSQuery converts a free-text user query into a syntactically-safe
// SQLite FTS5 MATCH expression. It exists because the previous code passed
// raw user input straight into `objects_fts MATCH ?`, which exposed FTS5
// operators (-, :, ", *, AND, OR, NOT, NEAR, parens) as injection-style
// surface. T-0565 reproduced this with `ctxt find credit-eligible` →
// "no such column: eligible" — FTS5 read the hyphen as a column qualifier.
//
// Strategy:
//   - tokenize on non-alphanumeric boundaries (already used by tokenize());
//   - wrap each token as an FTS5 phrase ("token"), which neutralises every
//     operator character inside;
//   - join with a single space (FTS5 implicit AND).
//
// FTS5 phrases match exactly the indexed token, so multi-token user input
// like "credit-eligible" becomes "credit" "eligible" — two phrases AND'd
// together, which behaves the way a user expects.
//
// Returns "" when the input has no usable tokens. Callers should treat that
// as a clear "empty query" rather than passing it to MATCH.
func SafeFTSQuery(q string) string {
	tokens := tokenize(q)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		// Embedded double-quotes are escaped per FTS5 spec by doubling them.
		// tokenize() already strips most punctuation but defend in depth.
		escaped := strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return strings.Join(parts, " ")
}
