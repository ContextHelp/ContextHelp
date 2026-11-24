package providertest

import (
	"embed"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Media fixtures: small real files with known content, so a provider test
// can assert the text a tool must find. Regenerate with
// scripts/gen-media-fixtures.sh; testdata/media/README.md has provenance.
//
//go:embed testdata/media/*.png testdata/media/*.wav testdata/media/*.mp4 testdata/media/*.srt testdata/media/*.vtt
var mediaFS embed.FS

// Fixture names.
const (
	MediaImage  = "text.png"   // PNG, 560x140: two lines of text
	MediaAudio  = "speech.wav" // 16 kHz mono WAV, two speakers, ~8.4 s
	MediaVideo  = "clip.mp4"   // H.264 + AAC, 320x180, text cards + the speech
	MediaSubSRT = "speech.srt" // SubRip captions for the audio
	MediaSubVTT = "speech.vtt" // WebVTT captions with speaker voice tags
)

// Known content of the fixtures.
var (
	// ImageLines are the lines drawn in MediaImage.
	ImageLines = []string{"Cassette fixture", "invoice 4821"}

	// SpeechTurns are the spoken turns of MediaAudio, in order. Speaker is
	// the turn's voice (0 or 1); Start/End are approximate offsets.
	SpeechTurns = []SpeechTurn{
		{Speaker: 0, Text: "Welcome to the weekly review. The launch date moved to March.",
			Start: 0, End: 4410 * time.Millisecond},
		{Speaker: 1, Text: "Understood. I will update the schedule today.",
			Start: 4910 * time.Millisecond, End: 8367 * time.Millisecond},
	}

	// VideoCards are the text shown in MediaVideo: card one for the first
	// half, card two for the second.
	VideoCards = [][]string{{"Scene 1", "code 7305"}, {"Scene 2", "code 1960"}}
)

// SpeechTurn is one known turn of the audio fixture.
type SpeechTurn struct {
	Speaker    int
	Text       string
	Start, End time.Duration
}

// MediaBytes returns a fixture's bytes.
func MediaBytes(t testing.TB, name string) []byte {
	t.Helper()
	b, err := mediaFS.ReadFile("testdata/media/" + name)
	if err != nil {
		t.Fatalf("media fixture %q: %v", name, err)
	}
	return b
}

// MediaFile copies a fixture into a fresh temp dir and returns its path,
// for providers that take a file path. The copy keeps the fixture's name.
func MediaFile(t testing.TB, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, MediaBytes(t, name), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", p, err)
	}
	return p
}
