package contracttest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Transcription is the transcription role's suite: Transcribe returns
// full text, and any segments are ordered with end not before start.
func Transcription(t *testing.T, cases []Case[providers.TranscriptionProvider], audioPath string) {
	Run(t, cases, func(t *testing.T, c Case[providers.TranscriptionProvider], p providers.TranscriptionProvider) {
		requireName(t, p.Name())
		res, err := p.Transcribe(context.Background(), audioPath, providers.TranscribeOptions{Language: "en"})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		if res.FullText == "" {
			t.Error("empty full text")
		}
		for i, s := range res.Segments {
			if s.EndTime < s.StartTime {
				t.Errorf("segment %d ends before it starts", i)
			}
			if i > 0 && s.StartTime < res.Segments[i-1].StartTime {
				t.Errorf("segment %d starts before segment %d", i, i-1)
			}
		}
	})
}
