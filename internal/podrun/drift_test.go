//go:build drift

package podrun

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestPodCapabilitiesDrift checks the installed `pod` binary's live
// capabilities document against the leaves ctxt depends on. Skips when `pod`
// is not installed on PATH. Run with: go test -tags 'fts5 drift' ./internal/podrun
func TestPodCapabilitiesDrift(t *testing.T) {
	bin := os.Getenv("PODRUN_POD_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath(BinaryName)
		if err != nil {
			t.Skipf("pod not on PATH: %v", err)
		}
	}
	c := NewClient(bin, NewExecRunner(), DefaultTimeouts())
	caps, err := c.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	required := []string{
		"pod show",
		"pod start",
		"pod stop",
		"pod create",
		"pod destroy",
		"pod exec",
		"pod forward",
		"pod upload",
	}
	if err := RequireLeaves(caps, required...); err != nil {
		t.Fatalf("pod capabilities drift: %v", err)
	}
}
