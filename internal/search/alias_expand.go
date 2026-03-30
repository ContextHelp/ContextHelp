package search

import (
	"context"
	"strings"
)

// AliasResolver resolves query terms to their known aliases.
// Implementations should look up alias records and return canonical aliases per term.
type AliasResolver interface {
	// ResolveAliases maps each term to a (possibly empty) list of alias strings.
	// On error, callers fall back to the original query.
	ResolveAliases(ctx context.Context, terms []string) (map[string][]string, error)
}

// ExpandQuery expands a free-text query by appending aliases for each term.
// Tokenises query on whitespace, looks up aliases for meaningful tokens, and
// appends unique alias terms. Original token order and casing are preserved.
// Returns the original query unchanged when:
//   - resolver is nil
//   - resolver returns an error
//   - no aliases are found
func ExpandQuery(ctx context.Context, query string, resolver AliasResolver) string {
	if resolver == nil {
		return query
	}

	terms := tokenize(query)
	if len(terms) == 0 {
		return query
	}

	aliasMap, err := resolver.ResolveAliases(ctx, terms)
	if err != nil {
		return query
	}

	// Build a set of existing lowercased tokens to deduplicate.
	existing := make(map[string]struct{}, len(terms))
	for _, t := range terms {
		existing[strings.ToLower(t)] = struct{}{}
	}

	var extra []string
	for _, term := range terms {
		aliases, ok := aliasMap[term]
		if !ok {
			continue
		}
		for _, a := range aliases {
			lower := strings.ToLower(a)
			if _, dup := existing[lower]; dup {
				continue
			}
			existing[lower] = struct{}{}
			extra = append(extra, a)
		}
	}

	if len(extra) == 0 {
		return query
	}

	return query + " " + strings.Join(extra, " ")
}

