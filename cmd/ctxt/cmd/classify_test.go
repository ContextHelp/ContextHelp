package cmd

import (
	"strings"
	"testing"
)

func TestClassifyHelp(t *testing.T) {
	out, err := executeCommand("classify", "--help")
	if err != nil {
		t.Fatalf("classify --help should succeed: %v", err)
	}
	for _, want := range []string{"classify", "--pipeline", "--output", "object_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output should contain %q", want)
		}
	}
}

func TestClassifyNoArgs(t *testing.T) {
	_, err := executeCommand("classify")
	if err == nil {
		t.Error("classify with no arguments should fail")
	}
}

func TestClassifyGracefulNoCgo(t *testing.T) {
	out, err := executeCommand("classify", "hello world")
	if err == nil {
		// cgo available — pipeline ran; nothing else to assert here
		return
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "classification unavailable") ||
		strings.Contains(errMsg, "cgo") {
		return // expected graceful message
	}
	// Storage errors are also acceptable in test env (no config).
	if strings.Contains(errMsg, "pipeline") || strings.Contains(errMsg, "init") {
		return
	}
	t.Errorf("unexpected error: %v\noutput: %s", err, out)
}
