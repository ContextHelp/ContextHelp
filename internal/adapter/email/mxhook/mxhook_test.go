package mxhook

import "testing"

// TestConfigZeroValueSafe pins the contract that Config{} must be
// usable as-is — backends fill defaults at Start time. Anything that
// breaks the zero-value invariant changes the operator surface; tests
// catch regressions before adopters do.
func TestConfigZeroValueSafe(t *testing.T) {
	var c Config
	if c.CredentialsRef != "" {
		t.Errorf("zero CredentialsRef = %q, want empty", c.CredentialsRef)
	}
	if c.Folder != "" {
		t.Errorf("zero Folder = %q, want empty", c.Folder)
	}
	if c.MaxItems != 0 {
		t.Errorf("zero MaxItems = %d, want 0", c.MaxItems)
	}
	if c.PollIntervalSeconds != 0 {
		t.Errorf("zero PollIntervalSeconds = %d, want 0", c.PollIntervalSeconds)
	}
}

// TestOAuthCredentialsRefIsString sanity-checks the type alias. If
// OAuthCredentialsRef ever becomes a struct, tests that pass strings
// directly to Config.CredentialsRef would silently fail to compile,
// not regress at runtime.
func TestOAuthCredentialsRefIsString(t *testing.T) {
	var ref OAuthCredentialsRef = "env:GMAIL"
	if string(ref) != "env:GMAIL" {
		t.Errorf("OAuthCredentialsRef round-trip: got %q, want env:GMAIL", ref)
	}
}
