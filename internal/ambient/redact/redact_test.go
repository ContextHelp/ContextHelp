package redact

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

func TestRedactor_ApplyRedactsAWSAccessKey(t *testing.T) {
	t.Parallel()
	r := New()
	ev := ambient.RawEvent{
		Payload: []byte("AKIAIOSFODNN7EXAMPLE is the key"),
	}
	got := r.Apply(ev)
	if strings.Contains(string(got.Payload), "AKIA") {
		t.Errorf("payload still contains raw AWS access key: %q", got.Payload)
	}
	if !strings.Contains(string(got.Payload), "AWS_ACCESS_KEY_REDACTED") {
		t.Errorf("payload should contain rule-specific marker: %q", got.Payload)
	}
}

func TestRedactor_ApplyRedactsGithubPAT(t *testing.T) {
	t.Parallel()
	r := New()
	ev := ambient.RawEvent{
		Payload: []byte("token: ghp_abcdefghijklmnopqrstuvwxyz0123456789"),
	}
	got := r.Apply(ev)
	if strings.Contains(string(got.Payload), "ghp_a") {
		t.Errorf("PAT not redacted: %q", got.Payload)
	}
}

func TestRedactor_ApplyRedactsBearerToken(t *testing.T) {
	t.Parallel()
	r := New()
	ev := ambient.RawEvent{
		Payload: []byte("Authorization: Bearer abcdefghijklmnopqrstuvwxyz123"),
	}
	got := r.Apply(ev)
	if strings.Contains(string(got.Payload), "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("bearer token not redacted: %q", got.Payload)
	}
	if !strings.Contains(string(got.Payload), "TOKEN_REDACTED") {
		t.Errorf("expected token marker: %q", got.Payload)
	}
}

func TestRedactor_ApplyRedactsSSN(t *testing.T) {
	t.Parallel()
	r := New()
	ev := ambient.RawEvent{
		Payload: []byte("My SSN is 123-45-6789 don't share"),
	}
	got := r.Apply(ev)
	if strings.Contains(string(got.Payload), "123-45-6789") {
		t.Errorf("SSN not redacted: %q", got.Payload)
	}
}

func TestRedactor_ApplyLeavesCleanPayloadAlone(t *testing.T) {
	t.Parallel()
	r := New()
	original := "This is just regular text with no secrets."
	ev := ambient.RawEvent{Payload: []byte(original)}
	got := r.Apply(ev)
	if string(got.Payload) != original {
		t.Errorf("clean payload should be unchanged; got %q", got.Payload)
	}
}

func TestRedactor_ApplyRedactsStringMetadataFields(t *testing.T) {
	t.Parallel()
	r := New()
	ev := ambient.RawEvent{
		Payload: []byte("clean payload"),
		Metadata: map[string]any{
			"file_path":            "/home/user/secrets/AKIAIOSFODNN7EXAMPLE.txt",
			"file_size":            int64(1024),  // numeric — not redacted
			"foreground_bundle_id": "com.example", // clean — unchanged
		},
	}
	got := r.Apply(ev)
	fp := got.Metadata["file_path"].(string)
	if strings.Contains(fp, "AKIA") {
		t.Errorf("metadata file_path not redacted: %q", fp)
	}
	if got.Metadata["file_size"].(int64) != 1024 {
		t.Errorf("numeric metadata field was modified")
	}
	if got.Metadata["foreground_bundle_id"].(string) != "com.example" {
		t.Errorf("clean metadata field was modified: %q", got.Metadata["foreground_bundle_id"])
	}
}

func TestRedactor_NilSafeApply(t *testing.T) {
	t.Parallel()
	var r *Redactor
	ev := ambient.RawEvent{Payload: []byte("AKIA-something")}
	got := r.Apply(ev)
	// Nil redactor passes events through untouched.
	if string(got.Payload) != "AKIA-something" {
		t.Errorf("nil Redactor should pass through untouched; got %q", got.Payload)
	}
}

func TestRedactor_StatsReportsMatches(t *testing.T) {
	t.Parallel()
	r := New()
	r.Apply(ambient.RawEvent{Payload: []byte("AKIAIOSFODNN7EXAMPLE")})
	r.Apply(ambient.RawEvent{Payload: []byte("ghp_abcdefghijklmnopqrstuvwxyz0123456789")})
	stats := r.Stats()
	if stats.Matches < 2 {
		t.Errorf("expected at least 2 matches in stats, got %d", stats.Matches)
	}
}
