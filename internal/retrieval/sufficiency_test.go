package retrieval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLLM is a test double for providers.LLMProvider.
type stubLLM struct {
	response string
	err      error
}

func (s *stubLLM) Generate(_ context.Context, _ string) (string, error) {
	return s.response, s.err
}

func (s *stubLLM) Name() string { return "stub" }

func TestExtractDecision(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "RETRIEVE tag",
			input:    "<decision>RETRIEVE</decision>",
			expected: "RETRIEVE",
		},
		{
			name:     "NO_RETRIEVE tag",
			input:    "<decision>NO_RETRIEVE</decision>",
			expected: "NO_RETRIEVE",
		},
		{
			name:     "whitespace around decision",
			input:    "<decision>  NO_RETRIEVE  </decision>",
			expected: "NO_RETRIEVE",
		},
		{
			name:     "lowercase decision",
			input:    "<decision>retrieve</decision>",
			expected: "RETRIEVE",
		},
		{
			name:     "fallback NO_RETRIEVE in raw text",
			input:    "After reviewing the content I conclude NO_RETRIEVE is appropriate.",
			expected: "NO_RETRIEVE",
		},
		{
			name:     "fallback RETRIEVE in raw text",
			input:    "We need to RETRIEVE more information.",
			expected: "RETRIEVE",
		},
		{
			name:     "malformed response defaults to RETRIEVE",
			input:    "Some gibberish response without tags",
			expected: "RETRIEVE",
		},
		{
			name:     "no_retrieve lowercase in raw text",
			input:    "no_retrieve is the right call here",
			expected: "NO_RETRIEVE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDecision(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractRewrittenQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "present tag",
			input:    "<rewritten_query>What are Go interfaces used for?</rewritten_query>",
			expected: "What are Go interfaces used for?",
		},
		{
			name:     "whitespace stripped",
			input:    "<rewritten_query>  trimmed  </rewritten_query>",
			expected: "trimmed",
		},
		{
			name:     "absent tag returns empty",
			input:    "No tags in this response",
			expected: "",
		},
		{
			name:     "case insensitive",
			input:    "<REWRITTEN_QUERY>case insensitive</REWRITTEN_QUERY>",
			expected: "case insensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRewrittenQuery(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSufficiencyChecker_Check_Sufficient(t *testing.T) {
	llm := &stubLLM{
		response: "<decision>NO_RETRIEVE</decision><rewritten_query>what is Go?</rewritten_query>",
	}
	checker := NewSufficiencyChecker(llm)

	needsMore, rewritten, err := checker.Check(
		context.Background(),
		"what is Go?",
		nil,
		"Go is an open source programming language.",
	)

	require.NoError(t, err)
	assert.False(t, needsMore, "should not need more retrieval")
	assert.Equal(t, "what is Go?", rewritten)
}

func TestSufficiencyChecker_Check_NeedsMore(t *testing.T) {
	llm := &stubLLM{
		response: "<decision>RETRIEVE</decision><rewritten_query>Go generics introduced in version</rewritten_query>",
	}
	checker := NewSufficiencyChecker(llm)

	needsMore, rewritten, err := checker.Check(
		context.Background(),
		"When were generics added to Go?",
		[]string{"user: tell me about Go"},
		"Go is an open source language.",
	)

	require.NoError(t, err)
	assert.True(t, needsMore, "should need more retrieval")
	assert.Equal(t, "Go generics introduced in version", rewritten)
}

func TestSufficiencyChecker_Check_LLMError_FallsBackToNeedsMore(t *testing.T) {
	llm := &stubLLM{err: assert.AnError}
	checker := NewSufficiencyChecker(llm)

	needsMore, rewritten, err := checker.Check(
		context.Background(),
		"original query",
		nil,
		"some content",
	)

	require.Error(t, err)
	assert.True(t, needsMore, "error should assume more retrieval needed")
	assert.Equal(t, "original query", rewritten, "original query returned on error")
}

func TestSufficiencyChecker_Check_EmptyRewrittenQueryFallsBack(t *testing.T) {
	llm := &stubLLM{
		// Response has decision but no rewritten_query tag.
		response: "<decision>RETRIEVE</decision>",
	}
	checker := NewSufficiencyChecker(llm)

	needsMore, rewritten, err := checker.Check(
		context.Background(),
		"original query",
		nil,
		"some content",
	)

	require.NoError(t, err)
	assert.True(t, needsMore)
	assert.Equal(t, "original query", rewritten, "should fall back to original query when tag absent")
}

func TestBuildSufficiencyPrompt(t *testing.T) {
	prompt := buildSufficiencyPrompt(
		"my query",
		[]string{"turn 1", "turn 2"},
		"content here",
		SufficiencySystemPrompt,
	)

	assert.Contains(t, prompt, "my query")
	assert.Contains(t, prompt, "turn 1\nturn 2")
	assert.Contains(t, prompt, "content here")
	assert.Contains(t, prompt, SufficiencySystemPrompt)
}

func TestBuildSufficiencyPrompt_NoHistory(t *testing.T) {
	prompt := buildSufficiencyPrompt("query", nil, "content", SufficiencySystemPrompt)
	assert.Contains(t, prompt, "No prior context.")
}
