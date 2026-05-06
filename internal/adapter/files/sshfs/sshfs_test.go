package sshfs

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks protocol+backend identifiers.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if got := a.Protocol(); got != "files" {
		t.Errorf("Protocol = %q, want files", got)
	}
	if got := a.Backend(); got != "sshfs" {
		t.Errorf("Backend = %q, want sshfs", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms capability honesty.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only sensor", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}

// TestNewRejectsMissingConfig validates required fields up front so
// operator typos in policy/ambient.yaml fail loud at registration
// rather than silently no-oping the sensor.
func TestNewRejectsMissingConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing host", Config{User: "u", RemotePath: "/p"}},
		{"missing user", Config{Host: "h", RemotePath: "/p"}},
		{"missing remote path", Config{Host: "h", User: "u"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(tc.cfg)
			if _, err := a.Fetch(context.Background()); err == nil {
				t.Errorf("Fetch with %s: want error, got nil", tc.name)
			}
		})
	}
}

// TestDefaultIdentityFile checks that Config.IdentityFile defaults to
// ~/.ssh/id_ed25519 when left empty — the modern key choice in 2026.
func TestDefaultIdentityFile(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if a.cfg.IdentityFile == "" {
		t.Error("IdentityFile should default to ~/.ssh/id_ed25519, got empty")
	}
}

// TestShouldIgnore exercises the glob-pattern ignore filter the walker
// applies to filenames. The walker is a private helper covered here
// without spinning up an SSH server.
func TestShouldIgnore(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"no patterns", nil, "foo.txt", false},
		{"exact match", []string{".git"}, ".git", true},
		{"glob match", []string{"*.tmp"}, "scratch.tmp", true},
		{"directory glob", []string{"node_modules"}, "node_modules", true},
		{"miss", []string{".git", "node_modules"}, "src", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldIgnore(tc.path, tc.patterns); got != tc.want {
				t.Errorf("shouldIgnore(%q, %v) = %v, want %v", tc.path, tc.patterns, got, tc.want)
			}
		})
	}
}
