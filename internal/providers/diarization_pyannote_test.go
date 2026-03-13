package providers

import (
	"testing"
	"time"
)

func TestParseRTTM(t *testing.T) {
	sample := `SPEAKER file1 1 0.500 2.300 <NA> <NA> SPEAKER_00 <NA> <NA>
SPEAKER file1 1 3.100 1.500 <NA> <NA> SPEAKER_01 <NA> <NA>
SPEAKER file1 1 5.000 3.000 <NA> <NA> SPEAKER_00 <NA> <NA>
`
	regions := parseRTTM(sample)
	if len(regions) != 3 {
		t.Fatalf("expected 3 regions, got %d", len(regions))
	}

	if regions[0].speaker != "SPEAKER_00" {
		t.Errorf("region[0].speaker: got %q", regions[0].speaker)
	}
	if regions[0].start != 500*time.Millisecond {
		t.Errorf("region[0].start: got %v", regions[0].start)
	}
	if regions[0].end != 2800*time.Millisecond {
		t.Errorf("region[0].end: got %v", regions[0].end)
	}

	if regions[1].speaker != "SPEAKER_01" {
		t.Errorf("region[1].speaker: got %q", regions[1].speaker)
	}
}

func TestComputeOverlap(t *testing.T) {
	tests := []struct {
		aStart, aEnd, bStart, bEnd time.Duration
		want                       time.Duration
	}{
		{0, 10 * time.Second, 5 * time.Second, 15 * time.Second, 5 * time.Second},
		{0, 5 * time.Second, 10 * time.Second, 15 * time.Second, 0},
		{0, 10 * time.Second, 0, 10 * time.Second, 10 * time.Second},
		{2 * time.Second, 8 * time.Second, 3 * time.Second, 6 * time.Second, 3 * time.Second},
	}
	for _, tt := range tests {
		got := computeOverlap(tt.aStart, tt.aEnd, tt.bStart, tt.bEnd)
		if got != tt.want {
			t.Errorf("computeOverlap(%v,%v,%v,%v) = %v, want %v", tt.aStart, tt.aEnd, tt.bStart, tt.bEnd, got, tt.want)
		}
	}
}

func TestPyannoteProviderName(t *testing.T) {
	p := NewPyannoteDiarizationProvider("pyannote")
	if p.Name() != "pyannote" {
		t.Errorf("Name: got %q", p.Name())
	}
}
