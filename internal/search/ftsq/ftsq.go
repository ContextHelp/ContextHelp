// Package ftsq holds the dialect-specific FTS query sanitizers and the
// tokenizer they share. It is a leaf package (stdlib only) so storage
// drivers can apply their own dialect's quoting at the driver boundary
// without importing the search compiler — the compiler and the drivers
// would otherwise form an import cycle through the driver factory.
//
// Both sanitizers reduce raw user text to the same token set; only the
// rendering differs per dialect. That keeps match semantics aligned across
// drivers: implicit AND over identical terms.
package ftsq

import (
	"strings"
	"unicode"
)

// PostgresRegconfig is the text-search configuration for the Postgres
// driver's generated tsvector column and every tsquery built against it.
// 'simple' is deliberate: no stemming and no stop-word removal, matching
// SQLite FTS5's default unicode61 tokenizer semantics so both drivers agree
// on what matches. This is the tokenizer-analog decision and feeds the FTS
// index signature.
const PostgresRegconfig = "simple"

// Tokenize splits s on non-alphanumeric boundaries (keeping '.'), the
// shared token rule for query decomposition, alias expansion, and both FTS
// sanitizers.
func Tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// ForSQLite converts free-text user input into a syntactically-safe SQLite
// FTS5 MATCH expression: each token becomes a quoted phrase (neutralizing
// every operator character inside), joined by FTS5's implicit AND. Raw user
// input previously reached MATCH directly, where FTS5 operators (-, :, ",
// *, AND, OR, NOT, NEAR, parens) acted as injection-style surface —
// `credit-eligible` errored with "no such column: eligible".
//
// Returns "" when the input has no usable tokens; callers treat that as
// match-nothing.
func ForSQLite(q string) string {
	tokens := Tokenize(q)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		// Embedded double-quotes are escaped per FTS5 spec by doubling
		// them. Tokenize already strips most punctuation but defend in
		// depth.
		escaped := strings.ReplaceAll(t, `"`, `""`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return strings.Join(parts, " ")
}

// ForPostgres converts free-text user input into the text handed to
// websearch_to_tsquery. websearch neutralizes hostile syntax on its own,
// but raw hyphenated input ("credit-eligible") parses there as a strict
// `<->` phrase over compound-plus-part tokens, which text holding
// "credit eligible" does not satisfy — semantic drift from the SQLite leg.
// Space-joining the same token set ForSQLite quotes yields websearch's
// implicit AND over identical terms instead.
//
// Returns "" when the input has no usable tokens; callers treat that as
// match-nothing.
func ForPostgres(q string) string {
	return strings.Join(Tokenize(q), " ")
}
