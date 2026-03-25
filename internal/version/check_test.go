package version

import (
	"context"
	"fmt"
	"testing"
)

func mockFetcher(tag string, err error) Fetcher {
	return func(_ context.Context) (string, error) {
		return tag, err
	}
}

func TestCheck_UpToDate(t *testing.T) {
	r := Check("1.2.3", mockFetcher("v1.2.3", nil))
	if !r.UpToDate {
		t.Errorf("expected UpToDate=true, got false (current=%s latest=%s)", r.Current, r.Latest)
	}
	if r.FetchErr != nil {
		t.Errorf("unexpected FetchErr: %v", r.FetchErr)
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	r := Check("1.2.3", mockFetcher("v1.3.0", nil))
	if r.UpToDate {
		t.Errorf("expected UpToDate=false, got true")
	}
	if r.Latest != "v1.3.0" {
		t.Errorf("expected Latest=v1.3.0, got %s", r.Latest)
	}
}

func TestCheck_NetworkFailure(t *testing.T) {
	r := Check("1.2.3", mockFetcher("", fmt.Errorf("connection refused")))
	if r.FetchErr == nil {
		t.Error("expected FetchErr to be set on network failure")
	}
}

func TestCheck_NilFetcherUsesDefault(t *testing.T) {
	// Just ensure Check doesn't panic with nil fetcher — swap default temporarily.
	orig := DefaultFetcher
	DefaultFetcher = mockFetcher("v9.9.9", nil)
	defer func() { DefaultFetcher = orig }()

	r := Check("9.9.9", nil)
	if !r.UpToDate {
		t.Errorf("expected UpToDate=true with mock default fetcher")
	}
}

func TestFormatResult_UpToDate(t *testing.T) {
	r := CheckResult{Current: "1.2.3", Latest: "v1.2.3", UpToDate: true}
	if got := FormatResult(r); got != "Up to date" {
		t.Errorf("expected 'Up to date', got %q", got)
	}
}

func TestFormatResult_UpdateAvailable(t *testing.T) {
	r := CheckResult{Current: "1.2.3", Latest: "v1.3.0", UpToDate: false}
	got := FormatResult(r)
	expected := "Update available: v1.3.0 (you have 1.2.3)"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestFormatResult_FetchError(t *testing.T) {
	r := CheckResult{Current: "1.2.3", FetchErr: fmt.Errorf("timeout")}
	got := FormatResult(r)
	if got == "" || got == "Up to date" {
		t.Errorf("expected warning message, got %q", got)
	}
}

func TestCheck_VersionWithoutV(t *testing.T) {
	// Both without 'v' prefix.
	r := Check("2.0.0", mockFetcher("2.0.0", nil))
	if !r.UpToDate {
		t.Errorf("expected UpToDate=true for matching versions without v prefix")
	}
}
