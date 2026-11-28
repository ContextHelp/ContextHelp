package rederive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// words builds n deterministic words from a vocabulary prefix, so two
// texts with different prefixes share no 3-gram.
func words(prefix string, from, n int) string {
	var b strings.Builder
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, "%s%03d ", prefix, (i*7+i/5)%997)
	}
	return b.String()
}

func page(source, text, finalURL string) *storage.KnowledgeObject {
	o := &storage.KnowledgeObject{Source: source, RawContent: text, TextContent: text}
	if finalURL != "" {
		o.Metadata = map[string]any{"final_url": finalURL}
	}
	return o
}

func TestTrigramsAndContainment(t *testing.T) {
	a, _ := trigrams("The quick brown fox, the QUICK brown fox! x jumps")
	// "x" is dropped (one character); distinct 3-grams of: the quick brown fox the quick brown fox jumps
	for _, g := range []string{"the quick brown", "quick brown fox", "brown fox the", "fox the quick", "brown fox jumps"} {
		if _, ok := a[g]; !ok {
			t.Errorf("missing 3-gram %q in %v", g, a)
		}
	}
	if len(a) != 5 {
		t.Errorf("distinct 3-grams = %d, want 5: %v", len(a), a)
	}
	if got := containment(a, a); got != 1 {
		t.Errorf("self containment = %v", got)
	}
	if got := containment(nil, a); got != 0 {
		t.Errorf("empty containment = %v", got)
	}
	b, _ := trigrams("the quick brown fox jumps over")
	if got := containment(b, a); got != 0.5 { // the quick brown, quick brown fox, brown fox jumps | fox jumps over
		// 2 of 4 grams of b are in a: "the quick brown", "quick brown fox", "brown fox jumps" = 3 of 4
		if got != 0.75 {
			t.Errorf("containment = %v", got)
		}
	}
}

// Three real-world shrinks (a cleaner bump removed boilerplate: lengths
// down to 0.28 to 0.39 of the old) must be accepted; changed content, a
// login wall sharing the navigation, and an unrelated page must not.
func TestPageRule(t *testing.T) {
	body := words("body", 0, 400) // the page's content
	nav := words("nav", 0, 150)   // navigation and boilerplate, a quarter of the old capture
	old := nav + body + words("nav", 150, 100)
	src := "https://example.com/a"
	cases := []struct {
		name   string
		before *storage.KnowledgeObject
		after  *storage.KnowledgeObject
		reject bool
	}{
		{"boilerplate removed, length ratio ~0.6", page(src, old, ""), page("", body+words("nav", 0, 40), src), false},
		{"boilerplate removed, length ratio ~0.4", page(src, old, ""), page("", body, src), false},
		{"boilerplate removed, length ratio ~0.28", page(src, old+words("more", 0, 200), ""), page("", body, src), false},
		{"same page", page(src, old, ""), page("", old, src), false},
		{"content changed", page(src, old, ""), page("", words("fresh", 0, 400), src), true},
		{"a login wall sharing the navigation", page(src, old, ""), page("", nav+" please log in to continue", src), true},
		{"mostly new content around the old", page(src, old, ""), page("", old+words("extra", 0, 1500), src), true},
		{"an unrelated page", page(src, old, ""), page("", words("other", 0, 300), src), true},
		{"empty", page(src, old, ""), page("", "", src), true},
		{"another host", page(src, old, ""), page("", old, "https://login.example.org/a"), true},
		{"www and case ignored", page("https://Example.COM/a", old, ""), page("", old, "https://www.example.com/b"), false},
		{"a port is not a host", page(src, old, ""), page("", old, "https://example.com:8443/a"), false},
		{"no recorded final url", page(src, old, ""), page("", old, ""), false},
		// A stored text too short to compare is never refreshed: the rule
		// cannot judge it, so it accepts nothing.
		{"short stored text, non-empty, same host", page(src, "a short stored text", ""), page("", "x1 y2", src), true},
		{"short stored text, the same text", page(src, "a short stored text", ""), page("", "a short stored text", src), true},
		{"short stored text, empty", page(src, "a short stored text", ""), page("", "  ", src), true},
		{"short stored text, other host", page(src, "a short stored text", ""), page("", "x1 y2", "https://other.example/"), true},
	}
	for _, c := range cases {
		err := RefreshGuard("url_fetcher")(c.before, c.after)
		if (err != nil) != c.reject {
			t.Errorf("%s: err = %v, want reject=%v", c.name, err, c.reject)
		}
	}
}

// A repository's README is rewritten upstream: any new text is accepted
// unless the repository was emptied.
func TestAPIRule(t *testing.T) {
	long := strings.Repeat("x", 800)
	cases := []struct {
		name   string
		before string
		after  string
		reject bool
	}{
		{"README rewritten", words("old", 0, 200), words("new", 0, 200), false},
		{"README shortened to a quarter", long, long[:200], false},
		{"exactly a tenth is kept", long, long[:80], false},
		{"under a tenth: emptied repository", long, long[:79], true},
		{"empty", long, "", true},
		{"short stored text may shrink", "short", "x", false},
	}
	for _, c := range cases {
		err := RefreshGuard("github_repo_fetcher")(page("https://github.com/o/r", c.before, ""), page("", c.after, ""))
		if (err != nil) != c.reject {
			t.Errorf("%s: err = %v, want reject=%v", c.name, err, c.reject)
		}
	}
}

func TestGuardRuleIsChosenByStep(t *testing.T) {
	// A step not listed is a page fetch: the host check applies.
	before, after := page("https://example.com/a", "t1 t2", ""), page("", "t1 t2", "https://other.example/")
	if err := RefreshGuard("some_new_fetcher")(before, after); err == nil {
		t.Error("an unlisted fetch step must be judged as a page")
	}
	if err := RefreshGuard("github_repo_fetcher")(before, after); err != nil {
		t.Errorf("an API fetch has no host check: %v", err)
	}
}

// han builds n characters of text without spaces from a pool of 2000
// ideographs: no two runs from different seeds share a 3-gram.
func han(seed, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(rune(0x4E00 + (seed*104729+i*7919+i*i)%2000))
	}
	return b.String()
}

func TestTrigramsOfTextWithoutSpaces(t *testing.T) {
	g, n := trigrams("请登录以继续访问")
	if n != 8 || len(g) != 6 {
		t.Errorf("tokens = %d, 3-grams = %d; want 8 and 6", n, len(g))
	}
	g, n = trigrams("ログインしてください")
	if n != 10 || len(g) != 8 {
		t.Errorf("kana tokens = %d, 3-grams = %d", n, len(g))
	}
	if _, n = trigrams("Login ログイン to the 站点 now"); n != 10 {
		t.Errorf("mixed tokens = %d, want 10", n)
	}
	if _, n := trigrams("a b c"); n != 0 {
		t.Errorf("one-letter words are not tokens: %d", n)
	}
}

func TestPageRule_CJKLoginWallIsRejected(t *testing.T) {
	src := "https://example.cn/a"
	article := han(1, 900)
	cases := []struct {
		name   string
		before string
		after  string
		reject bool
	}{
		{"Chinese article, short login wall", article, "请登录以继续访问此页面。", true},
		{"Japanese article, short login wall", han(2, 900) + "です。ます。でした。", "ログインしてください", true},
		{"Chinese article with boilerplate removed", han(3, 300) + article, article, false},
		{"Chinese article, unrelated article", article, han(9, 900), true},
		{"Chinese article unchanged", article, article, false},
		{"mixed Latin and Chinese, login wall", words("w", 0, 40) + article, "Please log in 请登录以继续访问此页面", true},
		{"mixed Latin and Chinese, boilerplate removed", words("nav", 0, 60) + words("w", 0, 40) + article, words("w", 0, 40) + article, false},
	}
	for _, c := range cases {
		err := RefreshGuard("url_fetcher")(page(src, c.before, ""), page("", c.after, src))
		if (err != nil) != c.reject {
			t.Errorf("%s: err = %v, want reject=%v", c.name, err, c.reject)
		}
	}
}

// Only the first maxTokens are compared, so a huge page costs a bounded
// amount; a page that merely grew past that point must still be refused.
func TestPageRule_LongPagesAreBoundedAndGrowthIsStillRefused(t *testing.T) {
	src := "https://example.com/a"
	var uniq strings.Builder
	for i := 0; i < 3*maxTokens; i++ {
		fmt.Fprintf(&uniq, "u%d ", i)
	}
	if g, n := trigrams(uniq.String()); len(g) > maxTokens || n != 3*maxTokens {
		t.Errorf("3-grams = %d (cap %d), tokens counted = %d", len(g), maxTokens, n)
	}
	big := words("big", 0, maxTokens+5000)
	if err := RefreshGuard("url_fetcher")(page(src, big, ""), page("", big, src)); err != nil {
		t.Errorf("an unchanged huge page: %v", err)
	}
	grown := big + words("more", 0, 3*maxTokens)
	if err := RefreshGuard("url_fetcher")(page(src, big, ""), page("", grown, src)); err == nil {
		t.Error("a page that grew several times over past the compared prefix was accepted")
	}
	slightly := big + words("more", 0, 1000)
	if err := RefreshGuard("url_fetcher")(page(src, big, ""), page("", slightly, src)); err != nil {
		t.Errorf("a slightly longer huge page: %v", err)
	}
}

// The host a link ended on at capture (a shortener, a custom domain) is the
// stored text's host.
func TestPageRule_HostIsTheStoredFinalURLsWhenThereIsOne(t *testing.T) {
	old := words("body", 0, 100)
	stored := page("https://short.example/x", old, "https://www.realsite.com/article")
	stored.Metadata = map[string]any{"final_url": "https://www.realsite.com/article"}
	if err := RefreshGuard("url_fetcher")(stored, page("", old, "https://realsite.com/article")); err != nil {
		t.Errorf("same final host as at capture: %v", err)
	}
	if err := RefreshGuard("url_fetcher")(stored, page("", old, "https://short.example/x")); err == nil {
		t.Error("ending on the shortener's host, not the captured one, was accepted")
	}
	// Without a stored final_url, the source's host is the reference.
	if err := RefreshGuard("url_fetcher")(page("https://a.example/x", old, ""), page("", old, "https://b.example/x")); err == nil {
		t.Error("another host than the source was accepted")
	}
}

// Combining marks belong to the word: a Hindi or Bengali word is not cut at
// its vowel signs into pieces too short to count.
func TestTrigramsKeepCombiningMarksInTheWord(t *testing.T) {
	hindi := "मेरा नाम राहुल है और मैं दिल्ली में रहता हूँ यहाँ"
	if _, n := trigrams(hindi); n != 11 {
		t.Errorf("Hindi tokens = %d, want 11 words", n)
	}
	bengali := "আমি ভাত খাই এবং মাছ খাই কিন্তু মাংস খাই না"
	if _, n := trigrams(bengali); n != 10 {
		t.Errorf("Bengali tokens = %d, want 10 words", n)
	}
	// Latin with a combining accent is one word, as its precomposed form is.
	if _, n := trigrams("café au lait"); n != 3 {
		t.Errorf("NFD tokens = %d, want 3", n)
	}
	if _, n := trigrams("café au lait"); n != 3 {
		t.Errorf("NFC tokens = %d, want 3", n)
	}
}

// A Hindi article is judged by its words, so a short login wall replaces
// nothing.
func TestPageRule_HindiLoginWallIsRejected(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "शब्द%d कहानी%d ", i%97, i%89)
	}
	article := b.String()
	src := "https://example.in/a"
	if err := RefreshGuard("url_fetcher")(page(src, article, ""), page("", "कृपया जारी रखने के लिए लॉग इन करें", src)); err == nil {
		t.Error("a Hindi login wall replaced an article")
	}
	if err := RefreshGuard("url_fetcher")(page(src, article, ""), page("", article, src)); err != nil {
		t.Errorf("the same Hindi article: %v", err)
	}
}

// The precheck refuses, before any request, a page that cannot be compared;
// the API rule has none.
func TestRefreshPrecheck(t *testing.T) {
	long := page("https://example.com/a", words("w", 0, 100), "")
	short := page("https://example.com/a", "a short stored text", "")
	if pre := RefreshPrecheck("url_fetcher"); pre == nil || pre(long) != nil || !errors.Is(pre(short), errTooShortToCompare) {
		t.Error("a page precheck must accept a long stored text and refuse a short one")
	}
	if RefreshPrecheck("github_repo_fetcher") != nil {
		t.Error("the API rule has no precheck")
	}
}
