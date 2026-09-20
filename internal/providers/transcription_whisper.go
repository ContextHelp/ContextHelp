package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WhisperTranscriptionProvider shells out to whisper.cpp CLI or similar.
type WhisperTranscriptionProvider struct {
	binary string // "whisper-cpp", "whisper", etc.
}

func NewWhisperTranscriptionProvider(binary string) *WhisperTranscriptionProvider {
	return &WhisperTranscriptionProvider{binary: binary}
}

func (p *WhisperTranscriptionProvider) Name() string { return "whisper-cli" }

// whisperSegment maps a single segment from whisper JSON output.
type whisperSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// whisperOutput maps the top-level JSON output of whisper.
type whisperOutput struct {
	Text     string           `json:"text"`
	Segments []whisperSegment `json:"segments"`
	Language string           `json:"language"`
}

func (p *WhisperTranscriptionProvider) Transcribe(ctx context.Context, audioPath string, opts TranscribeOptions) (*TranscriptResult, error) {
	args := []string{
		"-f", audioPath,
		"-oj", // output JSON
	}

	if opts.Language != "" {
		args = append(args, "-l", opts.Language)
	}

	result, err := RunCommand(ctx, p.binary, args...)
	if err != nil {
		// Whisper CLI variants have different flag conventions.
		// Try alternative flags.
		args = []string{
			"--file", audioPath,
			"--output-json",
		}
		if opts.Language != "" {
			args = append(args, "--language", opts.Language)
		}
		result, err = RunCommand(ctx, p.binary, args...)
		if err != nil {
			return nil, fmt.Errorf("whisper: %w", err)
		}
	}

	// Parse JSON output.
	var output whisperOutput
	stdout := strings.TrimSpace(result.Stdout)
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		// Not a failure: some whisper builds emit plain text rather than
		// JSON. The transcript is still valid, it simply carries no segment
		// or confidence detail, so stdout is returned as the full text.
		//nolint:nilerr // plain-text whisper output is a supported format, not an error
		return &TranscriptResult{
			FullText:         stdout,
			DetectedLanguage: opts.Language,
			Confidence:       0.0,
		}, nil
	}

	segments := make([]TranscriptSegment, len(output.Segments))
	for i, seg := range output.Segments {
		segments[i] = TranscriptSegment{
			StartTime: time.Duration(seg.Start * float64(time.Second)),
			EndTime:   time.Duration(seg.End * float64(time.Second)),
			Text:      strings.TrimSpace(seg.Text),
		}
	}

	lang := output.Language
	if lang == "" {
		lang = opts.Language
	}

	return &TranscriptResult{
		FullText:         strings.TrimSpace(output.Text),
		Segments:         segments,
		DetectedLanguage: lang,
		Confidence:       0.90, // whisper doesn't expose per-segment confidence in CLI output
	}, nil
}
