package keys

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	// FieldCiphertextPrefix marks a whole-field AEAD envelope encoded as base64.
	FieldCiphertextPrefix = "ctxt:enc:v1:"
	// SpanCiphertextOpen / SpanCiphertextClose delimit an inline encrypted span.
	SpanCiphertextOpen  = "«ctxt:enc:v1:"
	SpanCiphertextClose = "»"
)

// ErrEncryptScopeUnresolved is returned when a natural-language encrypt_scope
// matches no deterministic category and no SpanLocator is configured. Callers
// fail closed rather than silently storing plaintext when encryption was asked.
var ErrEncryptScopeUnresolved = errors.New("keys: natural-language encrypt scope requires a configured span locator")

// SpanLocator locates exact substrings within text matching a free-form
// natural-language instruction (e.g. "salary figures and client codenames").
// Keeping span identification separate from encryption guarantees key material
// never leaves the local KeyProvider.
type SpanLocator interface {
	LocateSpans(ctx context.Context, text, instruction string) ([]string, error)
}

// EncryptScopePlan describes which fields or spans a natural-language
// encrypt_scope targets.
type EncryptScopePlan struct {
	WholeContent bool             // encrypt RawContent, TextContent, Summaries, InboxNote
	RawContent   bool             // encrypt RawContent + TextContent (keep summaries searchable)
	Summaries    bool             // encrypt Summaries
	InboxNote    bool             // encrypt InboxNote
	Patterns     []*regexp.Regexp // regexes whose matches are sealed inline
	NeedsLLM     bool             // true when free-form NL instruction requires SpanLocator
}

var (
	reEncEmail      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	reEncPhone      = regexp.MustCompile(`(?:\+?\d{1,3}[\s.\-]?)?(?:\(\d{3}\)|\d{3})[\s.\-]?\d{3}[\s.\-]?\d{4}`)
	reEncSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	reEncCreditCard = regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`)
	reEncIPv4       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reEncSecret     = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_\-]{12,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|(?:api[_-]?key|secret|token|password|passwd|bearer)\s*[:=]\s*[^\s,;]{6,})`)
	reInlineToken   = regexp.MustCompile(`«ctxt:enc:v1:([A-Za-z0-9+/=]+)»`)
)

var encryptPatternRules = []struct {
	keywords []string
	patterns []*regexp.Regexp
}{
	{[]string{"pii", "personal info", "personal data", "contact info", "contact details"}, []*regexp.Regexp{reEncEmail, reEncPhone, reEncSSN, reEncCreditCard, reEncIPv4}},
	{[]string{"email"}, []*regexp.Regexp{reEncEmail}},
	{[]string{"phone", "telephone", "mobile"}, []*regexp.Regexp{reEncPhone}},
	{[]string{"ssn", "social security"}, []*regexp.Regexp{reEncSSN}},
	{[]string{"credit card", "card number", "payment card"}, []*regexp.Regexp{reEncCreditCard}},
	{[]string{"ip address", "ipv4"}, []*regexp.Regexp{reEncIPv4}},
	{[]string{"secret", "token", "api key", "apikey", "password", "credential"}, []*regexp.Regexp{reEncSecret}},
}

// ClassifyEncryptScope interprets an optional natural-language encryption scope.
// An empty scope encrypts the whole capture content.
func ClassifyEncryptScope(scope string) EncryptScopePlan {
	lower := strings.ToLower(strings.TrimSpace(scope))
	if isWholeEncryptScope(lower) {
		return EncryptScopePlan{WholeContent: true}
	}
	var plan EncryptScopePlan
	applyEncryptFieldFlags(lower, &plan)
	applyEncryptPatternRules(lower, &plan)
	if !plan.RawContent && !plan.Summaries && !plan.InboxNote && len(plan.Patterns) == 0 {
		plan.NeedsLLM = true
	}
	return plan
}

func isWholeEncryptScope(lower string) bool {
	switch lower {
	case "", "all", "everything", "whole", "full":
		return true
	}
	return false
}

func applyEncryptFieldFlags(lower string, plan *EncryptScopePlan) {
	keepSummaries := containsAnyStr(lower, "keep summary", "keep summaries", "only summary")
	if containsAnyStr(lower, "raw content", "raw body", "raw text", "transcript") || keepSummaries || lower == "raw" || lower == "body" {
		plan.RawContent = true
	}
	if containsAnyStr(lower, "summary", "summaries") && !keepSummaries {
		plan.Summaries = true
	}
	if containsAnyStr(lower, "inbox note", "audit note", "note") && !containsAnyStr(lower, "notes") {
		plan.InboxNote = true
	}
}

func applyEncryptPatternRules(lower string, plan *EncryptScopePlan) {
	for _, rule := range encryptPatternRules {
		if !containsAnyStr(lower, rule.keywords...) {
			continue
		}
		for _, re := range rule.patterns {
			addUniqueEncryptPattern(plan, re)
		}
	}
}

func addUniqueEncryptPattern(plan *EncryptScopePlan, re *regexp.Regexp) {
	for _, existing := range plan.Patterns {
		if existing == re {
			return
		}
	}
	plan.Patterns = append(plan.Patterns, re)
}

// SealFieldText encrypts an entire text field as FieldCiphertextPrefix + base64(envelope).
// Already-sealed values are returned unchanged.
func SealFieldText(ctx context.Context, p Provider, plaintext string, aad []byte) (string, error) {
	if plaintext == "" || strings.HasPrefix(plaintext, FieldCiphertextPrefix) {
		return plaintext, nil
	}
	if p == nil {
		return "", fmt.Errorf("seal field: %w", ErrUnavailable)
	}
	env, err := SealEnvelope(ctx, p, []byte(plaintext), aad)
	if err != nil {
		return "", err
	}
	return FieldCiphertextPrefix + base64.StdEncoding.EncodeToString(env), nil
}

// SealSelectiveText encrypts only the spans matching plan (and optionally
// locator for free-form natural-language instructions) inline as
// «ctxt:enc:v1:<base64>», leaving non-sensitive text intact and searchable.
func SealSelectiveText(ctx context.Context, p Provider, text, scope string, aad []byte, plan EncryptScopePlan, locator SpanLocator) (string, int, error) {
	if text == "" {
		return text, 0, nil
	}
	if p == nil {
		return "", 0, fmt.Errorf("seal selective text: %w", ErrUnavailable)
	}
	var spans []string
	seen := map[string]bool{}
	addSpan := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || strings.Contains(s, SpanCiphertextOpen) {
			return
		}
		seen[s] = true
		spans = append(spans, s)
	}
	for _, re := range plan.Patterns {
		for _, m := range re.FindAllString(text, -1) {
			addSpan(m)
		}
	}
	if plan.NeedsLLM {
		if locator == nil {
			return "", 0, ErrEncryptScopeUnresolved
		}
		found, err := locator.LocateSpans(ctx, text, scope)
		if err != nil {
			return "", 0, err
		}
		for _, s := range found {
			addSpan(s)
		}
	}
	if len(spans) == 0 {
		return text, 0, nil
	}
	// Longest spans first so shorter substrings do not split longer matches.
	sort.Slice(spans, func(i, j int) bool { return len(spans[i]) > len(spans[j]) })

	out := text
	sealedCount := 0
	for _, span := range spans {
		if !strings.Contains(out, span) {
			continue
		}
		env, err := SealEnvelope(ctx, p, []byte(span), aad)
		if err != nil {
			return "", 0, err
		}
		tok := SpanCiphertextOpen + base64.StdEncoding.EncodeToString(env) + SpanCiphertextClose
		sealedCount += strings.Count(out, span)
		out = strings.ReplaceAll(out, span, tok)
	}
	return out, sealedCount, nil
}

// DecryptFieldOrSpans unseals either a whole-field ctxt:enc:v1:... envelope
// or any inline «ctxt:enc:v1:...» tokens inside text. Plaintext with no markers
// is returned unchanged.
func DecryptFieldOrSpans(ctx context.Context, p Provider, text string, fieldAAD, spanAAD []byte) (string, error) {
	if text == "" {
		return "", nil
	}
	if strings.HasPrefix(text, FieldCiphertextPrefix) {
		if p == nil {
			return "", fmt.Errorf("decrypt field: %w", ErrUnavailable)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(text, FieldCiphertextPrefix))
		if err != nil {
			return "", fmt.Errorf("decode sealed field: %w", ErrUnwrap)
		}
		pt, err := OpenEnvelope(ctx, p, raw, fieldAAD)
		if err != nil {
			return "", err
		}
		return string(pt), nil
	}
	if !strings.Contains(text, SpanCiphertextOpen) {
		return text, nil
	}
	if p == nil {
		return "", fmt.Errorf("decrypt inline spans: %w", ErrUnavailable)
	}
	var firstErr error
	out := reInlineToken.ReplaceAllStringFunc(text, func(match string) string {
		if firstErr != nil {
			return match
		}
		sub := reInlineToken.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		raw, err := base64.StdEncoding.DecodeString(sub[1])
		if err != nil {
			firstErr = fmt.Errorf("decode sealed span: %w", ErrUnwrap)
			return match
		}
		pt, err := OpenEnvelope(ctx, p, raw, spanAAD)
		if err != nil {
			firstErr = err
			return match
		}
		return string(pt)
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func containsAnyStr(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
