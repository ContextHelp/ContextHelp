package expiry

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
)

// ErrScopeUnresolved is returned when a natural-language scope does not match
// any deterministic rule and no LLM SpanLocator is configured. Callers fail
// closed (leaving ExpiresAt intact) rather than silently clearing expiry.
var ErrScopeUnresolved = errors.New("expiry: natural-language scope requires a configured span locator")

// SpanLocator locates exact substrings within text matching a free-form
// natural-language instruction (e.g. "salary figures and client names").
type SpanLocator interface {
	LocateSpans(ctx context.Context, text, instruction string) ([]string, error)
}

// ScopePlan describes what a natural-language scope targets.
type ScopePlan struct {
	RawContent bool             // clear/redact RawContent + TextContent (keep summaries/tags)
	Metadata   bool             // clear Metadata
	InboxNote  bool             // clear InboxNote
	Patterns   []*regexp.Regexp // regexes whose matches should be redacted inline
	NeedsLLM   bool             // true when free-form NL instructions remain beyond deterministic rules
}

var (
	reEmail      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	rePhone      = regexp.MustCompile(`(?:\+?\d{1,3}[\s.\-]?)?(?:\(\d{3}\)|\d{3})[\s.\-]?\d{3}[\s.\-]?\d{4}`)
	reSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	reCreditCard = regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`)
	reIPv4       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reSecret     = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_\-]{12,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|(?:api[_-]?key|secret|token|password|passwd|bearer)\s*[:=]\s*[^\s,;]{6,})`)
)

var scopePatternRules = []struct {
	keywords []string
	patterns []*regexp.Regexp
}{
	{[]string{"pii", "personal info", "personal data", "contact info", "contact details"}, []*regexp.Regexp{reEmail, rePhone, reSSN, reCreditCard, reIPv4}},
	{[]string{"email"}, []*regexp.Regexp{reEmail}},
	{[]string{"phone", "telephone", "mobile"}, []*regexp.Regexp{rePhone}},
	{[]string{"ssn", "social security"}, []*regexp.Regexp{reSSN}},
	{[]string{"credit card", "card number", "payment card"}, []*regexp.Regexp{reCreditCard}},
	{[]string{"ip address", "ipv4"}, []*regexp.Regexp{reIPv4}},
	{[]string{"secret", "token", "api key", "apikey", "password", "credential"}, []*regexp.Regexp{reSecret}},
}

// ClassifyScope interprets a natural-language scope into deterministic field
// and entity rules. If the instruction contains no recognized deterministic
// categories, NeedsLLM is set to true so a configured SpanLocator can handle it.
func ClassifyScope(scope string) ScopePlan {
	lower := strings.ToLower(strings.TrimSpace(scope))
	if lower == "" {
		return ScopePlan{}
	}
	var plan ScopePlan
	add := func(re *regexp.Regexp) {
		for _, existing := range plan.Patterns {
			if existing == re {
				return
			}
		}
		plan.Patterns = append(plan.Patterns, re)
	}

	if containsAny(lower, "raw content", "raw body", "raw text", "transcript", "keep summary", "keep summaries", "only summary", "only summaries") || lower == "raw" || lower == "body" {
		plan.RawContent = true
	}
	if containsAny(lower, "metadata") {
		plan.Metadata = true
	}
	if containsAny(lower, "inbox note", "audit note", "note") && !containsAny(lower, "notes") {
		plan.InboxNote = true
	}
	for _, rule := range scopePatternRules {
		if containsAny(lower, rule.keywords...) {
			for _, re := range rule.patterns {
				add(re)
			}
		}
	}

	if !plan.RawContent && !plan.Metadata && !plan.InboxNote && len(plan.Patterns) == 0 {
		plan.NeedsLLM = true
	}
	return plan
}

// RedactText replaces every substring matching plan (and optionally locator)
// with placeholder, returning the redacted text and number of replacements.
func RedactText(ctx context.Context, text, scope, placeholder string, plan ScopePlan, locator SpanLocator) (string, int, error) {
	if text == "" {
		return text, 0, nil
	}
	out := text
	count := 0
	for _, re := range plan.Patterns {
		matches := re.FindAllString(out, -1)
		if len(matches) > 0 {
			count += len(matches)
			out = re.ReplaceAllString(out, placeholder)
		}
	}
	if plan.NeedsLLM {
		if locator == nil {
			return "", 0, ErrScopeUnresolved
		}
		spans, err := locator.LocateSpans(ctx, out, scope)
		if err != nil {
			return "", 0, err
		}
		// Replace longest spans first so substrings don't shadow longer matches.
		sort.Slice(spans, func(i, j int) bool { return len(spans[i]) > len(spans[j]) })
		for _, span := range spans {
			span = strings.TrimSpace(span)
			if span == "" || span == placeholder {
				continue
			}
			if n := strings.Count(out, span); n > 0 {
				count += n
				out = strings.ReplaceAll(out, span, placeholder)
			}
		}
	}
	return out, count, nil
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
