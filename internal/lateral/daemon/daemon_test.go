package daemon

import (
	"errors"
	"strings"
	"testing"
)

// TestErrNotWired_StableMessage pins the sentinel string. Operators
// triaging a partially-wired build grep on the substring; later tasks
// MUST keep this contract so older daemons emit the same diagnostic.
func TestErrNotWired_StableMessage(t *testing.T) {
	if ErrNotWired == nil {
		t.Fatal("ErrNotWired = nil")
	}
	if !strings.Contains(ErrNotWired.Error(), "lateral-daemon-wiring-20260509") {
		t.Errorf("ErrNotWired message missing track ref; got %q", ErrNotWired.Error())
	}
	if !errors.Is(ErrNotWired, ErrNotWired) {
		t.Error("errors.Is(ErrNotWired, ErrNotWired) = false")
	}
}
