package cmd

import (
	"maps"
	"testing"
)

// A --confirm=no invocation that kit refuses before runDelete leaves
// delete's gate annotations suspended. The next invocation in the same
// process must start with both annotations back in place, or it would
// skip the confirmation and typed-token gates.
func TestPreviewDeleteReArmsGatesLeftSuspended(t *testing.T) {
	orig := maps.Clone(deleteCmd.Annotations)
	t.Cleanup(func() {
		deleteCmd.Annotations = orig
		restoreTokenGate = nil
		previewRendered = false
	})
	for _, key := range []string{"kit/side-effect", "kit/destructive-token"} {
		if _, ok := orig[key]; !ok {
			t.Fatalf("delete has no %s annotation to suspend", key)
		}
	}

	deleteCmd.Annotations = maps.Clone(orig)
	restoreTokenGate = suspendGatesForAnsweredInspection(deleteCmd)
	// Absent is not neutral: kit reads an untagged leaf named delete as
	// destructive by name, so the suspended tier must be an explicit read.
	if got := deleteCmd.Annotations["kit/side-effect"]; got != "read" {
		t.Fatalf("suspended kit/side-effect = %q, want read", got)
	}
	if _, ok := deleteCmd.Annotations["kit/destructive-token"]; ok {
		t.Fatal("suspension left kit/destructive-token in place")
	}

	// A committing invocation: neither --dry-run nor --confirm=no.
	if err := previewDelete(deleteCmd, nil); err != nil {
		t.Fatalf("previewDelete: %v", err)
	}
	for _, key := range []string{"kit/side-effect", "kit/destructive-token"} {
		if got, want := deleteCmd.Annotations[key], orig[key]; got != want {
			t.Errorf("%s = %q after previewDelete, want %q re-armed", key, got, want)
		}
	}
	if restoreTokenGate != nil {
		t.Error("restoreTokenGate still set after re-arming")
	}
}
