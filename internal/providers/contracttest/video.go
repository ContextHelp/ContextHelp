package contracttest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Video is the video role's suite: ProbeMetadata reports duration and
// dimensions, ExtractAudio writes audio into the directory it is given. Frame
// selection belongs to the frames role, not here.
func Video(t *testing.T, cases []Case[providers.VideoProvider], videoPath string) {
	Run(t, cases, func(t *testing.T, c Case[providers.VideoProvider], p providers.VideoProvider) {
		requireName(t, p.Name())
		ctx := context.Background()
		meta, err := p.ProbeMetadata(ctx, videoPath)
		if err != nil {
			t.Fatalf("ProbeMetadata: %v", err)
		}
		if meta.Duration <= 0 || meta.Width <= 0 || meta.Height <= 0 {
			t.Errorf("incomplete metadata: %+v", meta)
		}
		outDir := t.TempDir()
		audio, err := p.ExtractAudio(ctx, videoPath, outDir)
		if err != nil {
			t.Fatalf("ExtractAudio: %v", err)
		}
		if audio.Path == "" || audio.Format == "" || audio.ContentType == "" || audio.SampleRate <= 0 {
			t.Errorf("incomplete audio: %+v", audio)
		}
		if filepath.Dir(audio.Path) != outDir {
			t.Errorf("audio written to %s, want inside %s", audio.Path, outDir)
		}
	})
}
