package contracttest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Summarization is the summarization role's suite. Backends must: refuse
// empty text, return non-empty text with provider and model provenance
// for every medium, and honour context cancellation.
func Summarization(t *testing.T, cases []Case[providers.SummarizationProvider]) {
	Run(t, cases, func(t *testing.T, c Case[providers.SummarizationProvider], p providers.SummarizationProvider) {
		requireName(t, p.Name())
		ctx := context.Background()

		t.Run("refuses empty text", func(t *testing.T) {
			for _, text := range []string{"", "  \n\t"} {
				if _, err := p.Summarize(ctx, providers.SummarizeRequest{Text: text}); !errors.Is(err, providers.ErrEmptySummaryInput) {
					t.Errorf("text %q: err = %v, want ErrEmptySummaryInput", text, err)
				}
			}
		})

		t.Run("summarizes every medium", func(t *testing.T) {
			for _, m := range []string{providers.MediumGeneric, providers.MediumAudio, providers.MediumVideo, providers.MediumMeeting, "unknown-medium"} {
				res, err := p.Summarize(ctx, providers.SummarizeRequest{Medium: m, Text: "Ana: ship on Friday. Bo: I will write the notes."})
				if err != nil {
					t.Fatalf("medium %q: %v", m, err)
				}
				if strings.TrimSpace(res.Text) == "" {
					t.Errorf("medium %q: empty summary", m)
				}
				if res.Provider == "" || res.Model == "" {
					t.Errorf("medium %q: missing provenance %+v", m, res)
				}
			}
		})

		if !c.Stub {
			t.Run("honours canceled context", func(t *testing.T) {
				if _, err := p.Summarize(canceled(), providers.SummarizeRequest{Text: "text"}); err == nil {
					t.Error("want error")
				}
			})
		}
	})
}
