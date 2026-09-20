package sshfs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

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

// TestDefaultKnownHostsFile checks Config.KnownHostsFile defaults to the
// OpenSSH location, so host key verification is on without extra config.
func TestDefaultKnownHostsFile(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if a.cfg.KnownHostsFile == "" {
		t.Error("KnownHostsFile should default to ~/.ssh/known_hosts, got empty")
	}
}

// TestHostKeyVerificationOnByDefault is the security regression guard:
// an adapter built from bare config must NOT skip host key verification.
func TestHostKeyVerificationOnByDefault(t *testing.T) {
	a := New(Config{Host: "h", User: "u", RemotePath: "/p"})
	if a.cfg.InsecureSkipHostKeyVerification {
		t.Error("host key verification must default to enabled")
	}
}

// TestHostKeyCallbackRejectsMissingKnownHosts confirms a nonexistent
// known_hosts file is a hard error rather than a silent fallback to an
// unverified connection.
func TestHostKeyCallbackRejectsMissingKnownHosts(t *testing.T) {
	a := New(Config{
		Host:           "h",
		User:           "u",
		RemotePath:     "/p",
		KnownHostsFile: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	cb, err := a.hostKeyCallback("h:22")
	if err == nil {
		t.Fatal("want error for missing known_hosts, got nil")
	}
	if cb != nil {
		t.Error("callback must be nil when known_hosts cannot be loaded")
	}
}

// TestHostKeyCallbackUsesKnownHosts confirms a valid known_hosts file
// yields a working callback that accepts the pinned key and rejects a
// different one (the MITM case).
func TestHostKeyCallbackUsesKnownHosts(t *testing.T) {
	goodKey, goodPub := newTestHostKey(t)
	_, otherPub := newTestHostKey(t)

	khPath := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"127.0.0.1:22"}, goodPub)
	if err := os.WriteFile(khPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	_ = goodKey

	a := New(Config{Host: "127.0.0.1", User: "u", RemotePath: "/p", KnownHostsFile: khPath})
	cb, err := a.hostKeyCallback("127.0.0.1:22")
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	if err := cb("127.0.0.1:22", addr, goodPub); err != nil {
		t.Errorf("pinned host key should be accepted, got %v", err)
	}
	if err := cb("127.0.0.1:22", addr, otherPub); err == nil {
		t.Error("unknown host key must be rejected (MITM guard)")
	}
}

// TestHostKeyCallbackInsecureOptIn confirms the escape hatch works when
// explicitly enabled — and only then.
func TestHostKeyCallbackInsecureOptIn(t *testing.T) {
	_, pub := newTestHostKey(t)
	a := New(Config{
		Host:                            "h",
		User:                            "u",
		RemotePath:                      "/p",
		KnownHostsFile:                  filepath.Join(t.TempDir(), "does-not-exist"),
		InsecureSkipHostKeyVerification: true,
	})
	cb, err := a.hostKeyCallback("h:22")
	if err != nil {
		t.Fatalf("insecure opt-in should not error: %v", err)
	}
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	if err := cb("h:22", addr, pub); err != nil {
		t.Errorf("insecure callback should accept any key, got %v", err)
	}
}

// newTestHostKey generates an ed25519 SSH host key for callback tests.
func newTestHostKey(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer, signer.PublicKey()
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
