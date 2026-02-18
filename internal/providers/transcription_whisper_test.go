package providers

import (
	"encoding/json"
	"testing"
)

func TestWhisperOutputParsing(t *testing.T) {
	sample := `{
		"text": "Hello world, this is a test.",
		"segments": [
			{"start": 0.0, "end": 2.5, "text": " Hello world,"},
			{"start": 2.5, "end": 5.0, "text": " this is a test."}
		],
		"language": "en"
	}`

	var output whisperOutput
	if err := json.Unmarshal([]byte(sample), &output); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if output.Text != "Hello world, this is a test." {
		t.Errorf("Text: got %q", output.Text)
	}
	if len(output.Segments) != 2 {
		t.Fatalf("Segments: got %d", len(output.Segments))
	}
	if output.Segments[0].Start != 0.0 {
		t.Errorf("Segment[0].Start: got %f", output.Segments[0].Start)
	}
	if output.Segments[1].End != 5.0 {
		t.Errorf("Segment[1].End: got %f", output.Segments[1].End)
	}
	if output.Language != "en" {
		t.Errorf("Language: got %q", output.Language)
	}
}

func TestWhisperProviderName(t *testing.T) {
	p := NewWhisperTranscriptionProvider("whisper-cpp")
	if p.Name() != "whisper-cli" {
		t.Errorf("Name: got %q", p.Name())
	}
}
