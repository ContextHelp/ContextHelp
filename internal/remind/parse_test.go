package remind

import (
	"testing"
	"time"
)

// anchor: 2026-03-25 10:00:00 UTC Wednesday
var anchor = time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)

func TestParseTime_InDuration(t *testing.T) {
	tests := []struct {
		expr    string
		wantAdd time.Duration
	}{
		{"in 2h", 2 * time.Hour},
		{"in 30m", 30 * time.Minute},
		{"in 1h30m", 90 * time.Minute},
		{"in 45", 45 * time.Minute}, // plain int → minutes
	}
	for _, tc := range tests {
		got, err := ParseTime(tc.expr, anchor)
		if err != nil {
			t.Errorf("ParseTime(%q) error: %v", tc.expr, err)
			continue
		}
		want := anchor.Add(tc.wantAdd)
		if !got.Equal(want) {
			t.Errorf("ParseTime(%q) = %v, want %v", tc.expr, got, want)
		}
	}
}

func TestParseTime_Tomorrow(t *testing.T) {
	got, err := ParseTime("tomorrow 9am", anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 26, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseTime_Today(t *testing.T) {
	got, err := ParseTime("today 14:30", anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 25, 14, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseTime_ISODate(t *testing.T) {
	got, err := ParseTime("2026-04-01", anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseTime_ISODatetime(t *testing.T) {
	got, err := ParseTime("2026-04-01 10:00", anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseTime_Weekday(t *testing.T) {
	// anchor is Wednesday 2026-03-25; next Monday = 2026-03-30
	got, err := ParseTime("monday 9am", anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseTime_Invalid(t *testing.T) {
	_, err := ParseTime("not a time", anchor)
	if err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}

func TestParseTime_CaseInsensitive(t *testing.T) {
	got1, err1 := ParseTime("Tomorrow 9AM", anchor)
	got2, err2 := ParseTime("tomorrow 9am", anchor)
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v / %v", err1, err2)
	}
	if !got1.Equal(got2) {
		t.Errorf("case sensitivity mismatch: %v != %v", got1, got2)
	}
}
