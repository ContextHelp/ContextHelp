package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Diarization is the diarization role's suite: Diarize keeps every input
// segment's text and timing, and reports at least one speaker.
func Diarization(t *testing.T, cases []Case[providers.DiarizationProvider], audioPath string) {
	Run(t, cases, func(t *testing.T, c Case[providers.DiarizationProvider], p providers.DiarizationProvider) {
		requireName(t, p.Name())
		in := []providers.TranscriptSegment{
			{StartTime: 0, EndTime: 4 * time.Second, Text: "hello"},
			{StartTime: 4 * time.Second, EndTime: 9 * time.Second, Text: "hi there"},
		}
		res, err := p.Diarize(context.Background(), audioPath, in)
		if err != nil {
			t.Fatalf("Diarize: %v", err)
		}
		if res.SpeakerCount < 1 {
			t.Errorf("SpeakerCount = %d", res.SpeakerCount)
		}
		if len(res.Segments) != len(in) {
			t.Fatalf("got %d segments, want %d", len(res.Segments), len(in))
		}
		for i, s := range res.Segments {
			if s.Text != in[i].Text || s.StartTime != in[i].StartTime || s.EndTime != in[i].EndTime {
				t.Errorf("segment %d changed: %+v", i, s)
			}
		}
	})
}
