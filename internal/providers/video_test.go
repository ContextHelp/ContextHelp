package providers

import (
	"context"
	"testing"
)

func TestStubVideoProbe(t *testing.T) {
	p := NewStubVideoProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	meta, err := p.ProbeMetadata(context.Background(), "/tmp/test.mp4")
	if err != nil {
		t.Fatalf("ProbeMetadata: %v", err)
	}
	if meta.Width != 1920 || meta.Height != 1080 {
		t.Errorf("Resolution: got %dx%d", meta.Width, meta.Height)
	}
	if meta.FPS != 30.0 {
		t.Errorf("FPS: got %f", meta.FPS)
	}
	if meta.Codec != "h264" {
		t.Errorf("Codec: got %q", meta.Codec)
	}
	if meta.Duration == 0 {
		t.Error("Duration is zero")
	}
}

func TestStubVideoExtractAudio(t *testing.T) {
	p := NewStubVideoProvider()
	audio, err := p.ExtractAudio(context.Background(), "/tmp/test.mp4")
	if err != nil {
		t.Fatalf("ExtractAudio: %v", err)
	}
	if audio.Path == "" {
		t.Error("Path is empty")
	}
	if audio.Format != "wav" {
		t.Errorf("Format: got %q", audio.Format)
	}
	if audio.SampleRate != 44100 {
		t.Errorf("SampleRate: got %d", audio.SampleRate)
	}
}

func TestStubVideoSampleFrames(t *testing.T) {
	p := NewStubVideoProvider()
	frames, err := p.SampleFrames(context.Background(), "/tmp/test.mp4", 5)
	if err != nil {
		t.Fatalf("SampleFrames: %v", err)
	}
	if len(frames) != 5 {
		t.Fatalf("frame count: got %d, want 5", len(frames))
	}
	for i, f := range frames {
		if f.Index != i {
			t.Errorf("frame %d: Index=%d", i, f.Index)
		}
		if f.ContentType != "image/png" {
			t.Errorf("frame %d: ContentType=%q", i, f.ContentType)
		}
	}
}
