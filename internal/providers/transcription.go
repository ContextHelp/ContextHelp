package providers

import (
	"context"
	"time"
)

// TranscribeOptions controls transcription behavior.
type TranscribeOptions struct {
	Language string // ISO 639-1 code (e.g., "en", "fr")
	Format   string // MIME type of the audio
}

// TranscriptSegment is a timestamped piece of transcript.
type TranscriptSegment struct {
	StartTime time.Duration
	EndTime   time.Duration
	Text      string
	Speaker   string // populated after diarization
}

// TranscriptResult holds the output of a transcription.
type TranscriptResult struct {
	FullText         string
	Segments         []TranscriptSegment
	DetectedLanguage string
	Confidence       float64
}

// TranscriptionProvider converts audio to text.
type TranscriptionProvider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error)
}

// StubTranscriptionProvider returns placeholder transcript.
type StubTranscriptionProvider struct{}

func NewStubTranscriptionProvider() *StubTranscriptionProvider {
	return &StubTranscriptionProvider{}
}

func (p *StubTranscriptionProvider) Name() string { return "stub" }

func (p *StubTranscriptionProvider) Transcribe(_ context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error) {
	return &TranscriptResult{
		FullText: "[Transcribed audio content]",
		Segments: []TranscriptSegment{
			{StartTime: 0, EndTime: 10 * time.Second, Text: "[Transcribed audio content]"},
		},
		DetectedLanguage: "en",
		Confidence:       0.94,
	}, nil
}

// DiarizedResult holds speaker-attributed segments.
type DiarizedResult struct {
	SpeakerCount  int
	SpeakerLabels []string
	Segments      []TranscriptSegment
}

// DiarizationProvider attributes transcript segments to speakers.
type DiarizationProvider interface {
	Name() string
	Diarize(ctx context.Context, audioPath string, segments []TranscriptSegment) (*DiarizedResult, error)
}

// StubDiarizationProvider returns segments unchanged (no speaker attribution).
type StubDiarizationProvider struct{}

func NewStubDiarizationProvider() *StubDiarizationProvider {
	return &StubDiarizationProvider{}
}

func (p *StubDiarizationProvider) Name() string { return "stub" }

func (p *StubDiarizationProvider) Diarize(_ context.Context, _ string, segments []TranscriptSegment) (*DiarizedResult, error) {
	return &DiarizedResult{
		SpeakerCount:  1,
		SpeakerLabels: []string{"Speaker_1"},
		Segments:      segments,
	}, nil
}
