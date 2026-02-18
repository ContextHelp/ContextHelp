package providers

import (
	"testing"
	"time"
)

func TestParseFrameRate(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"30/1", 30.0},
		{"30000/1001", 29.97002997002997},
		{"24/1", 24.0},
		{"60", 60.0},
		{"0/0", 0},
	}
	for _, tt := range tests {
		got := parseFrameRate(tt.input)
		if got != tt.want {
			t.Errorf("parseFrameRate(%q) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "00:00:00.000"},
		{1*time.Minute + 30*time.Second, "00:01:30.000"},
		{2*time.Hour + 5*time.Minute + 10*time.Second + 500*time.Millisecond, "02:05:10.500"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFFmpegProbeMetadata(t *testing.T) {
	if _, err := LookupTool("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	// Integration test would require a real video file.
	// This test validates the provider can be created.
	p := NewFFmpegVideoProvider()
	if p.Name() != "ffmpeg" {
		t.Errorf("Name: got %q", p.Name())
	}
}
