package contracttest

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// LLM is the llm role's suite: Generate returns text for a prompt and
// honours context cancellation.
func LLM(t *testing.T, cases []Case[providers.LLMProvider]) {
	Run(t, cases, func(t *testing.T, c Case[providers.LLMProvider], p providers.LLMProvider) {
		requireName(t, p.Name())
		out, err := p.Generate(context.Background(), "Say hello.")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("empty output")
		}
		if !c.Stub {
			if _, err := p.Generate(canceled(), "Say hello."); err == nil {
				t.Error("canceled context: want error")
			}
		}
	})
}
