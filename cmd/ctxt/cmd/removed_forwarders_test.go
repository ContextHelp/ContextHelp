package cmd

import (
	"strings"
	"testing"
)

// ctxt is unreleased, so it carries no forwarders for commands that live
// in dpkms (dev, detector, job, key, secret) and no aliases for renamed
// profile verbs (show is view, delete is rm). Each is gone from the
// tree, and invoking one is an unknown command, not a "moved" notice.
func TestCompatForwardersRemoved(t *testing.T) {
	children := func(names []string) map[string]bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	gone := children([]string{"dev", "detector", "job", "key", "secret"})
	for _, c := range rootCmd.Commands() {
		if gone[c.Name()] {
			t.Errorf("ctxt %s still registered: it lives in dpkms", c.Name())
		}
	}
	goneProfile := children([]string{"show", "delete"})
	for _, c := range profileCmd.Commands() {
		if goneProfile[c.Name()] {
			t.Errorf("ctxt profile %s still registered: use view / rm", c.Name())
		}
	}
	if _, ok := commandGroups["dev"]; ok {
		t.Error(`commandGroups still maps "dev"`)
	}

	for _, args := range [][]string{
		{"dev", "validate-registry", "foo.yaml"},
		{"dev", "gen-docs"},
		{"job", "status", "job_123"},
		{"key", "init"},
		{"secret", "list"},
		{"detector", "list"},
	} {
		_, err := executeCommand(args...)
		if err == nil {
			t.Errorf("ctxt %s succeeded; want an unknown command", strings.Join(args, " "))
			continue
		}
		if strings.Contains(err.Error(), "moved to dpkms") {
			t.Errorf("ctxt %s still forwards: %v", strings.Join(args, " "), err)
		}
	}
}

// find stays signature-clean (kept from the dev forwarder tests, which
// checked it alongside the dev aliases).
func TestFindSignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	for _, v := range root.ValidateSignature().Violations {
		if strings.HasSuffix(v.Path, " find") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}
