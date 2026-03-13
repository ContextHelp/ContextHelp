package providers

import (
	"context"
	"testing"
	"time"
)

func TestStubOCR(t *testing.T) {
	p := NewStubOCRProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	result, err := p.Extract(context.Background(), []byte("fake"), "image/png")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Confidence < 0.5 {
		t.Errorf("Confidence too low: %f", result.Confidence)
	}
	if result.Text == "" {
		t.Error("Text is empty")
	}
}

func TestStubTranscription(t *testing.T) {
	p := NewStubTranscriptionProvider()
	if p.Name() != "stub" {
		t.Errorf("Name: got %q", p.Name())
	}
	result, err := p.Transcribe(context.Background(), "/fake.mp3", TranscribeOptions{Language: "en"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.FullText == "" {
		t.Error("FullText is empty")
	}
	if len(result.Segments) == 0 {
		t.Error("no segments")
	}
}

func TestStubDiarization(t *testing.T) {
	p := NewStubDiarizationProvider()
	segments := []TranscriptSegment{
		{StartTime: 0, EndTime: 5 * time.Second, Text: "hello"},
	}
	result, err := p.Diarize(context.Background(), "/fake.mp3", segments)
	if err != nil {
		t.Fatalf("Diarize: %v", err)
	}
	if result.SpeakerCount != 1 {
		t.Errorf("SpeakerCount: got %d", result.SpeakerCount)
	}
	if len(result.Segments) != 1 {
		t.Errorf("Segments: got %d", len(result.Segments))
	}
}

