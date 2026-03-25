package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	internalversion "github.com/ideacrafterslabs/ctxt/internal/version"
)

func TestVersionFlag(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("--version should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt version 1.2.3") {
		t.Errorf("output should contain 'ctxt version 1.2.3', got: %s", out)
	}
	if !strings.Contains(out, "(2026-03-15)") {
		t.Errorf("output should contain '(2026-03-15)', got: %s", out)
	}
}

func TestVersionFlagShort(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("-v")
	if err != nil {
		t.Fatalf("-v should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt version 1.2.3") {
		t.Errorf("output should contain 'ctxt version 1.2.3', got: %s", out)
	}
}

func TestVersionFlagLong(t *testing.T) {
	SetVersionInfo("0.1.0-dirty", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")

	out, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("--version should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt version 0.1.0-dirty (2026-03-15)") {
		t.Errorf("output format mismatch, got: %s", out)
	}
}

func withMockFetcher(t *testing.T, f internalversion.Fetcher) {
	t.Helper()
	orig := internalversion.DefaultFetcher
	SetVersionFetcher(f)
	t.Cleanup(func() { SetVersionFetcher(orig) })
}

func TestVersionCheckUpToDate(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")
	withMockFetcher(t, func(_ context.Context) (string, error) { return "v1.2.3", nil })

	out, err := executeCommand("--version", "--check")
	if err != nil {
		t.Fatalf("--version --check should succeed: %v", err)
	}
	if !strings.Contains(out, "Up to date") {
		t.Errorf("expected 'Up to date', got: %s", out)
	}
}

func TestVersionCheckUpdateAvailable(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")
	withMockFetcher(t, func(_ context.Context) (string, error) { return "v1.5.0", nil })

	out, err := executeCommand("--version", "--check")
	if err != nil {
		t.Fatalf("--version --check should succeed: %v", err)
	}
	if !strings.Contains(out, "Update available") {
		t.Errorf("expected 'Update available', got: %s", out)
	}
	if !strings.Contains(out, "v1.5.0") {
		t.Errorf("expected latest version in output, got: %s", out)
	}
}

func TestVersionCheckNetworkFailure(t *testing.T) {
	SetVersionInfo("1.2.3", "2026-03-15_10:00:00", "abc1234")
	defer SetVersionInfo("", "", "")
	withMockFetcher(t, func(_ context.Context) (string, error) {
		return "", fmt.Errorf("connection refused")
	})

	out, err := executeCommand("--version", "--check")
	if err != nil {
		t.Fatalf("network failure should be a soft warning, not an error: %v", err)
	}
	if !strings.Contains(out, "warning") {
		t.Errorf("expected soft warning on network failure, got: %s", out)
	}
}
