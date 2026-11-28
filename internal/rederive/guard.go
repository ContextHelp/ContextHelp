package rederive

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// How a refresh is judged. A fetch that returns 200 with something that is
// not the page (a login wall, a soft 404, a parked domain, an emptied
// repository) must not replace good stored text. What "the same" means
// depends on where the text comes from, so the rule is chosen by the
// fetching step (guardRules).
//
// Page fetches compare the new text with the stored one by their word
// 3-grams (distinct, lower-case alphanumeric tokens of two or more
// characters), not by length: a better cleaner legitimately shrinks a
// page. Measured on pages captured with an old cleaner and captured again
// with the current one:
//
//   - new length / old length went as low as 0.28 (a length rule at 50%
//     rejected 3 of 15 pages);
//   - the share of the new text's 3-grams found in the old text was
//     0.61 to 1.00 for 14 of 15 (0.42 for the page whose content had
//     changed), and at most 0.30 between unrelated pages (99th percentile
//     0.02);
//   - the share of the old text's 3-grams still in the new text was 0.37
//     to 1.00.
//
// So a page is accepted when most of the new text was in the old (it is
// the same content with less noise) and a quarter of the old text is
// still there (a login wall sharing the site's navigation is not). A page
// whose content changed since capture is left as captured.
const (
	minNewInOld = 0.60
	minOldInNew = 0.25
	// minJudgeable is the fewest 3-grams a stored text needs to be judged
	// by them; shorter, it is too short to compare and is not refreshed.
	minJudgeable = 20
	// An API (GitHub) names the repository itself, and READMEs are
	// legitimately rewritten upstream (containment 0.26 to 0.97 on 32
	// repositories), so only an emptied one is refused: under a tenth of
	// the stored text, when that is at least apiMinStoredBytes. The emptied
	// repositories measured 2%.
	apiMinKeptPercent = 10
	apiMinStoredBytes = 500
	metaFinalURL      = "final_url" // url_fetcher's record of where the fetch ended
)

// guardRules chooses the rule by network step; a step not listed is a page
// fetch.
var guardRules = map[string]func(before, after *storage.KnowledgeObject) error{
	"github_repo_fetcher": apiRule,
}

// errTooShortToCompare: the stored text has too few 3-grams to be compared
// with a refresh, so it is left as captured.
var errTooShortToCompare = errors.New("stored text too short to compare with a refresh; left as captured")

// RefreshPrecheck is the service's Before hook for a refresh over the
// network, by the step that fetches: it refuses, before any request is
// made, an object a page refresh could not be checked against. The API rule
// needs no such check: identity comes from the API.
func RefreshPrecheck(fetchStep string) func(stored *storage.KnowledgeObject) error {
	if _, api := guardRules[fetchStep]; api {
		return nil
	}
	return func(stored *storage.KnowledgeObject) error {
		if g, _ := trigrams(projection.BodyText(stored)); len(g) < minJudgeable {
			return errTooShortToCompare
		}
		return nil
	}
}

// RefreshGuard is the service's Accept hook for a refresh over the
// network, by the step that fetches (empty or unlisted: a page).
func RefreshGuard(fetchStep string) func(before, after *storage.KnowledgeObject) error {
	if r, ok := guardRules[fetchStep]; ok {
		return r
	}
	return pageRule
}

// apiRule keeps the stored object when the new text is empty or has shrunk
// to under apiMinKeptPercent of a stored text of apiMinStoredBytes or more.
func apiRule(before, after *storage.KnowledgeObject) error {
	stored, fresh := len(projection.BodyText(before)), len(projection.BodyText(after))
	if fresh == 0 {
		return fmt.Errorf("refresh rejected: the fetched text is empty")
	}
	if stored >= apiMinStoredBytes && fresh*100 < stored*apiMinKeptPercent {
		return fmt.Errorf("refresh rejected: the fetched text is %d bytes, the stored %d", fresh, stored)
	}
	return nil
}

// pageRule keeps the stored object unless the new text is the same content
// (see the constants), fetched from the host it was captured from.
func pageRule(before, after *storage.KnowledgeObject) error {
	old, fresh := projection.BodyText(before), projection.BodyText(after)
	if strings.TrimSpace(fresh) == "" {
		return fmt.Errorf("refresh rejected: the fetched page has no text")
	}
	// The host the stored text came from: where the capture ended when it
	// followed a redirect (a shortener, a custom domain), else the source.
	want := hostOf(metaString(before, metaFinalURL))
	if want == "" {
		want = hostOf(before.Source)
	}
	if got := hostOf(metaString(after, metaFinalURL)); want != "" && got != "" && want != got {
		return fmt.Errorf("refresh rejected: the fetch ended on %s, the stored text came from %s", got, want)
	}
	o, oCount := trigrams(old)
	n, nCount := trigrams(fresh)
	if len(o) < minJudgeable {
		// A stored text this short cannot be compared with anything: a
		// portal that serves its page on the requested host would replace
		// it, and the acceptance would be false evidence. Such an object is
		// not refreshed at all: RefreshPrecheck ends it before any request.
		// Reaching this after a fetch is a plain rejection, not that
		// decision.
		return errors.New("refresh rejected: the stored text is too short to compare")
	}
	// Only the first maxTokens of each text are compared. A page that
	// merely grew past that point has the same prefix, so the sets cannot
	// tell it from the old one: its length does. The share of the new text
	// found in the old cannot exceed oldTokens/newTokens, so a text more
	// than 1/minNewInOld times as long as the old one is refused, as the
	// exact rule would.
	if (oCount > maxTokens || nCount > maxTokens) && float64(oCount) < minNewInOld*float64(nCount) {
		return fmt.Errorf("refresh rejected: the fetched text has %d words, the stored %d", nCount, oCount)
	}
	if in := containment(n, o); in < minNewInOld {
		return fmt.Errorf("refresh rejected: %.2f of the fetched text was in the stored text, need %.2f", in, minNewInOld)
	}
	if in := containment(o, n); in < minOldInNew {
		return fmt.Errorf("refresh rejected: %.2f of the stored text is still in the fetched text, need %.2f", in, minOldInNew)
	}
	return nil
}

func metaString(o *storage.KnowledgeObject, key string) string {
	s, _ := o.Metadata[key].(string)
	return s
}

// maxTokens bounds the work and the memory of a comparison: at most this
// many tokens of a text become 3-grams (20000 is a long article, about
// 100 KB of English, and a set of at most 20000 short strings), so a
// multi-megabyte page costs the same.
const maxTokens = 20000

// spacelessScripts are written without spaces between words: a page of them
// has no word tokens, so each character is a token.
var spacelessScripts = []*unicode.RangeTable{
	unicode.Han, unicode.Hiragana, unicode.Katakana,
	unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar,
}

// trigrams returns the set of distinct 3-grams over the first maxTokens
// tokens of text, and the number of tokens in all. A token is a run of
// letters, digits and combining marks (a Hindi or Bengali word is cut at
// no vowel sign), lower-cased, of two or more characters; or one character
// of a script written without spaces.
func trigrams(text string) (map[string]struct{}, int) {
	var toks []string
	count := 0
	var word []rune
	emit := func(t string) {
		count++
		if len(toks) < maxTokens {
			toks = append(toks, t)
		}
	}
	flush := func() {
		if len(word) >= 2 {
			emit(string(word))
		}
		word = word[:0]
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.In(r, spacelessScripts...):
			flush()
			emit(string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	out := map[string]struct{}{}
	for i := 0; i+3 <= len(toks); i++ {
		out[toks[i]+" "+toks[i+1]+" "+toks[i+2]] = struct{}{}
	}
	return out, count
}

// containment is the share of the members of a that are also in b; 0 for
// an empty a.
func containment(a, b map[string]struct{}) float64 {
	if len(a) == 0 {
		return 0
	}
	n := 0
	for k := range a {
		if _, ok := b[k]; ok {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

// hostOf is the comparable host of an http(s) URL: lower case, without a
// port and a leading "www."; empty for anything else.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
