package banner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// upgradingServer answers every request with the header of an in-flight
// selective re-ingest.
func upgradingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(upgrade.HeaderName, upgrade.EncodeHeader(inFlight))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// invocation is a command with kit's --quiet and --no-hints globals whose
// stderr is a buffer.
func invocation(args ...string) (*cobra.Command, *bytes.Buffer) {
	var buf bytes.Buffer
	c := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	c.Flags().Bool("quiet", false, "")
	c.Flags().Bool("no-hints", false, "")
	_ = c.Flags().Parse(args)
	c.SetErr(&buf)
	return c, &buf
}

func get(t *testing.T, url string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func alwaysTTY(io.Writer) bool { return true }
func neverTTY(io.Writer) bool  { return false }

// An armed invocation prints the banner once from the first API response
// carrying the header, on the command's stderr.
func TestArmPrintsOncePerInvocation(t *testing.T) {
	base := upgradingServer(t)
	c, buf := invocation()
	Arm(c, alwaysTTY)
	t.Cleanup(func() { q, _ := invocation("--quiet"); Arm(q, alwaysTTY) })

	get(t, base+"/api/v1/objects")
	get(t, base+"/api/v1/search")
	if got := buf.String(); got != Format(inFlight)+"\n" {
		t.Fatalf("stderr = %q, want the banner once", got)
	}

	// The next invocation gets its own line.
	c2, buf2 := invocation()
	Arm(c2, alwaysTTY)
	get(t, base+"/api/v1/objects")
	if strings.Count(buf2.String(), "\n") != 1 || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("second invocation: first %q, second %q", buf.String(), buf2.String())
	}
}

// --quiet, --no-hints and a non-terminal stderr each silence it.
func TestArmSuppressed(t *testing.T) {
	base := upgradingServer(t)
	for _, tc := range []struct {
		name string
		args []string
		tty  func(io.Writer) bool
	}{
		{"quiet", []string{"--quiet"}, alwaysTTY},
		{"no-hints", []string{"--no-hints"}, alwaysTTY},
		{"not a terminal", nil, neverTTY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, buf := invocation(tc.args...)
			Arm(c, tc.tty)
			get(t, base+"/api/v1/objects")
			if buf.Len() != 0 {
				t.Fatalf("banner printed: %q", buf.String())
			}
		})
	}
}

// IsTerminal is false for anything but a terminal file.
func TestIsTerminal(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("buffer reported as a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("regular file reported as a terminal")
	}
}
