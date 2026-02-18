package providers

import (
	"context"
	"fmt"
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

// SampledFrame holds a single extracted keyframe.
type SampledFrame struct {
	Index       int
	Timestamp   time.Duration
	Path        string
	ContentType string
}

// VideoProvider handles video file operations.
type VideoProvider interface {
	Name() string
	ProbeMetadata(ctx context.Context, videoPath string) (*VideoMetadata, error)
	ExtractAudio(ctx context.Context, videoPath string) (*ExtractedAudio, error)
	SampleFrames(ctx context.Context, videoPath string, count int) ([]SampledFrame, error)
}

// StubVideoProvider returns placeholder results for testing.
type StubVideoProvider struct{}

func NewStubVideoProvider() *StubVideoProvider { return &StubVideoProvider{} }

func (p *StubVideoProvider) Name() string { return "stub" }

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

func (p *StubVideoProvider) ExtractAudio(_ context.Context, videoPath string) (*ExtractedAudio, error) {
	return &ExtractedAudio{
		Path:        videoPath + ".audio.wav",
		Format:      "wav",
		Duration:    2*time.Minute + 30*time.Second,
		SampleRate:  44100,
		ContentType: "audio/wav",
	}, nil
}

func (p *StubVideoProvider) SampleFrames(_ context.Context, videoPath string, count int) ([]SampledFrame, error) {
	frames := make([]SampledFrame, count)
	interval := (2*time.Minute + 30*time.Second) / time.Duration(count)
	for i := range frames {
		frames[i] = SampledFrame{
			Index:       i,
			Timestamp:   time.Duration(i) * interval,
			Path:        fmt.Sprintf("%s.frame.%d.png", videoPath, i),
			ContentType: "image/png",
		}
	}
	return frames, nil
}
