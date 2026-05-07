package search_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/search"
)

func TestSafeFTSQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single bare term",
			in:   "kubernetes",
			want: `"kubernetes"`,
		},
		{
			name: "two bare terms become AND of two phrases",
			in:   "kubernetes deployment",
			want: `"kubernetes" "deployment"`,
		},
		{
			// T-0565 — the original repro. `credit-eligible` previously
			// crashed with "no such column: eligible" because `-` was
			// read as FTS5 NOT / column qualifier.
			name: "hyphenated term splits into two phrases",
			in:   "credit-eligible",
			want: `"credit" "eligible"`,
		},
		{
			// Defence in depth: a colon could be parsed as
			// `column:term`, which fails when the column name doesn't
			// exist. Treat it as a separator.
			name: "colon-bearing term is sanitised",
			in:   "foo:bar",
			want: `"foo" "bar"`,
		},
		{
			// Asterisks (FTS5 prefix-match operator) and parens
			// (grouping) are stripped by tokenize().
			name: "operator characters are stripped",
			in:   "(foo* OR bar*)",
			want: `"foo" "OR" "bar"`,
		},
		{
			// Embedded double-quotes — defend in depth: tokenize()
			// strips them, but the escape clause must still be safe.
			name: "embedded quotes",
			in:   `say "hello"`,
			want: `"say" "hello"`,
		},
		{
			name: "all-punctuation input returns empty",
			in:   "---:::***",
			want: ``,
		},
		{
			name: "empty input returns empty",
			in:   "",
			want: ``,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := search.SafeFTSQuery(tc.in)
			if got != tc.want {
				t.Errorf("SafeFTSQuery(%q):\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}
