package retrieval

import (
	"context"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// SufficiencyChecker evaluates whether retrieved content is sufficient to
// answer a query, and optionally rewrites the query for the next tier.
type SufficiencyChecker struct {
	llm providers.LLMProvider
}

// NewSufficiencyChecker creates a SufficiencyChecker backed by llm.
func NewSufficiencyChecker(llm providers.LLMProvider) *SufficiencyChecker {
	return &SufficiencyChecker{llm: llm}
}

// Check evaluates sufficiency. It returns:
//   - needsMore: true when additional retrieval is required
//   - rewrittenQuery: a refined query for the next tier (or the original if sufficient)
func (c *SufficiencyChecker) Check(
	ctx context.Context,
	query string,
	conversationHistory []string,
	retrievedContent string,
) (needsMore bool, rewrittenQuery string, err error) {
	prompt := buildSufficiencyPrompt(query, conversationHistory, retrievedContent, SufficiencySystemPrompt)

	response, err := c.llm.Generate(ctx, prompt)
	if err != nil {
		return true, query, err
	}

	decision := extractDecision(response)
	rewritten := extractRewrittenQuery(response)
	if rewritten == "" {
		rewritten = query
	}

	return decision == "RETRIEVE", rewritten, nil
}

// buildSufficiencyPrompt combines the system + user templates into a single
// prompt string compatible with a Generate-style LLM interface.
func buildSufficiencyPrompt(query string, history []string, content, systemPrompt string) string {
	historyText := "No prior context."
	if len(history) > 0 {
		historyText = strings.Join(history, "\n")
	}

	userPart := strings.ReplaceAll(SufficiencyUserPrompt, "{conversation_history}", historyText)
	userPart = strings.ReplaceAll(userPart, "{query}", query)
	userPart = strings.ReplaceAll(userPart, "{retrieved_content}", content)

	return systemPrompt + "\n\n" + userPart
}

// extractDecision parses the <decision>…</decision> tag from the LLM response.
func extractDecision(response string) string {
	re := regexp.MustCompile(`(?i)<decision>\s*(.*?)\s*</decision>`)
	if match := re.FindStringSubmatch(response); match != nil {
		decision := strings.ToUpper(strings.TrimSpace(match[1]))
		if strings.Contains(decision, "NO_RETRIEVE") {
			return "NO_RETRIEVE"
		}
		if strings.Contains(decision, "RETRIEVE") {
			return "RETRIEVE"
		}
	}

	// Fallback: scan the raw text.
	upper := strings.ToUpper(response)
	if strings.Contains(upper, "NO_RETRIEVE") {
		return "NO_RETRIEVE"
	}
	return "RETRIEVE"
}

// extractRewrittenQuery parses the <rewritten_query>…</rewritten_query> tag.
func extractRewrittenQuery(response string) string {
	re := regexp.MustCompile(`(?i)<rewritten_query>\s*(.*?)\s*</rewritten_query>`)
	if match := re.FindStringSubmatch(response); match != nil {
		return strings.TrimSpace(match[1])
	}
	return ""
}
