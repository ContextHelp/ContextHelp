package search

import (
	"regexp"
	"strings"
	"unicode"
)

// QueryMode represents the detected intent/style of a search query.
type QueryMode string

const (
	QueryModeKeyword   QueryMode = "keyword"
	QueryModeQuestion  QueryMode = "question"
	QueryModeTechnical QueryMode = "technical"
	QueryModeDefault   QueryMode = "default"
)

var (
	questionWords = map[string]bool{
		"what": true, "how": true, "why": true, "when": true,
		"where": true, "which": true, "who": true, "is": true,
		"are": true, "does": true, "can": true, "should": true,
		"would": true,
	}

	// Query-mode stopwords. The list differs from searchStopwords in
	// query_decompose.go, so folding the two together would change which
	// queries read as keyword searches.
	stopwords = wordSet(`
		a an the and or but in on at to for of with by from as is it its
		be was are were been has have had do does did will would could
		should may might can that this these those i we you he she they
		my our your his her their
	`)

	versionRe  = regexp.MustCompile(`^v\d+(\.\d+)+$`)
	filePathRe = regexp.MustCompile(`^(/[\w.\-]+)+$`)
	allCapsRe  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)

	// Well-known tech terms that count as a technical indicator each.
	techTerms = map[string]bool{
		"api": true, "jwt": true, "http": true, "https": true, "grpc": true,
		"rest": true, "graphql": true, "sql": true, "nosql": true,
		"json": true, "yaml": true, "toml": true, "xml": true,
		"tcp": true, "udp": true, "tls": true, "ssl": true,
		"redis":  false, // not a tech indicator on its own (would be keyword)
		"docker": true, "k8s": true, "kubernetes": true, "helm": true,
		"terraform": true, "ci": true, "cd": true, "oauth": true,
		"saml": true, "ldap": true, "dns": true, "cdn": true,
	}
)

// DetectQueryMode analyses query signals and returns the most appropriate QueryMode.
// Rules applied in order:
//  1. Trailing '?' → question
//  2. Starts with question word AND len(words) > 5 → question
//  3. ≥ 3 technical indicators → technical
//  4. len(meaningfulWords) ≤ 2 → keyword
//  5. else → default
func DetectQueryMode(query string) QueryMode {
	q := strings.TrimSpace(query)
	if q == "" {
		return QueryModeDefault
	}

	// Rule 1: trailing '?'
	if strings.HasSuffix(q, "?") {
		return QueryModeQuestion
	}

	words := strings.Fields(q)

	// Rule 2: starts with question word and sentence is long enough
	if len(words) > 5 {
		first := strings.ToLower(words[0])
		if questionWords[first] {
			return QueryModeQuestion
		}
	}

	// Rule 3: technical indicator count
	if countTechnicalIndicators(words) >= 3 {
		return QueryModeTechnical
	}

	// Rule 4: few meaningful words → keyword
	if len(filterStopwordsLocal(words)) <= 2 {
		return QueryModeKeyword
	}

	return QueryModeDefault
}

// countTechnicalIndicators counts how many technical signals exist in the token list.
// Each token contributes at most 1 count, via one of:
//   - file path pattern
//   - version number (vN.N)
//   - camelCase token
//   - ALL_CAPS token
//   - known tech term
func countTechnicalIndicators(words []string) int {
	count := 0
	for _, w := range words {
		if isTechnicalToken(w) {
			count++
		}
	}
	return count
}

func isTechnicalToken(w string) bool {
	if filePathRe.MatchString(w) {
		return true
	}
	lower := strings.ToLower(w)
	if versionRe.MatchString(lower) {
		return true
	}
	if allCapsRe.MatchString(w) {
		return true
	}
	if isCamelCase(w) {
		return true
	}
	if v, ok := techTerms[lower]; ok && v {
		return true
	}
	return false
}

// isCamelCase returns true for tokens like handleHTTPRequest or parseJSON.
// A token is camelCase when it contains at least one lowercase letter followed
// by an uppercase letter (or vice-versa in mixed sequences).
func isCamelCase(w string) bool {
	if len(w) < 2 {
		return false
	}
	runes := []rune(w)
	for i := 0; i < len(runes)-1; i++ {
		if unicode.IsLower(runes[i]) && unicode.IsUpper(runes[i+1]) {
			return true
		}
	}
	return false
}

// filterStopwordsLocal returns only the meaningful (non-stopword) words.
// TODO: replace with filterStopwords from query_decompose.go once that lands.
func filterStopwordsLocal(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if !stopwords[strings.ToLower(w)] {
			out = append(out, w)
		}
	}
	return out
}
