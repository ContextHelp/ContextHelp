package providers

import (
	"context"
	"path/filepath"
	"time"
)

// VideoMetadata holds probe results for a video file.
type VideoMetadata struct {
	Duration   time.Duration
	Width      int
	Height     int
	FPS        float64
	Codec      string
	AudioCodec string
	FileSize   int64
}

// ExtractedAudio holds the result of extracting audio from video.
type ExtractedAudio struct {
	Path        string
	Format      string
	Duration    time.Duration
	SampleRate  int
	ContentType string
}

// VideoProvider handles video file operations.
type VideoProvider interface {
	Name() string
	ProbeMetadata(ctx context.Context, videoPath string) (*VideoMetadata, error)
	// ExtractAudio writes the audio track into outDir, which the caller
	// owns and removes.
	ExtractAudio(ctx context.Context, videoPath, outDir string) (*ExtractedAudio, error)
}

// StubVideoProvider returns placeholder results for testing.
type StubVideoProvider struct{}

func NewStubVideoProvider() *StubVideoProvider { return &StubVideoProvider{} }

func (p *StubVideoProvider) Name() string { return backendStub }

func (p *StubVideoProvider) ProbeMetadata(_ context.Context, videoPath string) (*VideoMetadata, error) {
	return &VideoMetadata{
		Duration:   2*time.Minute + 30*time.Second,
		Width:      1920,
		Height:     1080,
		FPS:        30.0,
		Codec:      "h264",
		AudioCodec: "aac",
		FileSize:   50 * 1024 * 1024,
	}, nil
}

func (p *StubVideoProvider) ExtractAudio(_ context.Context, videoPath, outDir string) (*ExtractedAudio, error) {
	return &ExtractedAudio{
		Path:        filepath.Join(outDir, filepath.Base(videoPath)+".audio.wav"),
		Format:      "wav",
		Duration:    2*time.Minute + 30*time.Second,
		SampleRate:  44100,
		ContentType: "audio/wav",
	}, nil
}
