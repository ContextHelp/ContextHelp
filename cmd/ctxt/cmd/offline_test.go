package cmd

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/core/netpolicy"
)

// offlineProbeURL is a non-loopback address that never leaves the host:
// a dial to 0.0.0.0 reaches the local stack, and nothing serves port 9,
// so a request that gets past the policy fails with connection refused.
const offlineProbeURL = "http://0.0.0.0:9"

// --offline is kit's global. kit guards http.DefaultTransport and stamps
// the command context, so a ctxt client request to a non-loopback host
// is refused before it dials; ctxt adds no binding of its own.
func TestOffline_KitRefusesOutboundHTTP(t *testing.T) {
	hermeticConfigEnv(t)
	t.Cleanup(func() { statusCmd.SetContext(context.Background()) })

	_, err := runWithConfigFlag(t, "--offline", "status", "--server", offlineProbeURL)
	if !errors.Is(err, netpolicy.ErrOffline) {
		t.Fatalf("--offline status: err = %v, want netpolicy.ErrOffline", err)
	}

	// Same request without --offline reaches the dialer. cobra keeps a
	// subcommand's context across in-process runs, so drop the marker
	// the first run stamped on it.
	statusCmd.SetContext(context.Background())
	_, err = runWithConfigFlag(t, "status", "--server", offlineProbeURL)
	if err == nil || errors.Is(err, netpolicy.ErrOffline) {
		t.Fatalf("status without --offline: err = %v, want a dial error, not ErrOffline", err)
	}
}
