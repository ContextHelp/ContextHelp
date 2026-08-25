package search

import "github.com/ideacrafterslabs/ctxt/internal/search/ftsq"

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
	return ftsq.ForSQLite(q)
}

// SanitizeFTSQueryFor converts raw user text into the dialect-appropriate
// FTS expression — the sanitizer half of the seam where CompileFor already
// branches. Drivers apply their own leg (package ftsq) on the raw text they
// receive; callers above the driver boundary must not pre-bake any
// dialect's rules (FTS5 quoting sent to websearch_to_tsquery, or vice
// versa, is exactly the drift this seam removes).
//
// Returns "" when the input has no usable tokens; drivers treat that as
// match-nothing, not an error.
func SanitizeFTSQueryFor(d Dialect, q string) string {
	if d == DialectPostgres {
		return ftsq.ForPostgres(q)
	}
	return ftsq.ForSQLite(q)
}
