package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// serverFlagUsage is the --server help text of the single-target client
// commands that route through serverEndpoint.
const serverFlagUsage = "dpkms server URL; overrides server.url and server.urls (default " +
	idxbridge.DefaultBaseURL + " when nothing is configured)"

// serverEndpoint resolves the one dpkms instance a single-target client
// command (status, log, upgrade status) talks to, the same way analyze and
// capture route: an explicit --server pins that URL, reusing its configured
// token; otherwise the primary of server.urls, then server.url (config
// file or -c), then the client default.
func serverEndpoint(cmd *cobra.Command) idxbridge.Endpoint {
	if f := cmd.Flags().Lookup("server"); f != nil && f.Changed && f.Value.String() != "" {
		return pinnedEndpoint(f.Value.String())
	}
	return clientEndpoints()[0]
}

// serverGet issues GET <ep.URL><path> with ep's bearer token, if any. A
// request nothing answered (connection refused, unknown host, dial
// timeout) comes back as kit's PREREQUISITE (exit 70): the invocation was
// right, the daemon is not there.
func serverGet(ctx context.Context, ep idxbridge.Endpoint, path string, timeout time.Duration) (*gohttp.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	url := strings.TrimRight(ep.URL, "/") + path
	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if ep.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ep.Token)
	}
	client := &gohttp.Client{Timeout: timeout}
	resp, err := client.Do(req) // #nosec G107 -- operator-configured server URL
	if err != nil {
		return nil, contactError(ep.URL, err)
	}
	return resp, nil
}

// contactError classifies a failed request. Dial and DNS failures mean
// nothing answered at url, which is PREREQUISITE; anything else (a timeout
// after connecting, a TLS failure) is returned unchanged for the central
// classifier.
func contactError(url string, err error) error {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if (errors.As(err, &opErr) && opErr.Op == "dial") || errors.As(err, &dnsErr) {
		e := output.WrapError(fmt.Errorf("dpkms at %s unreachable: %w", url, err),
			output.CodePrerequisite, output.ExitPrerequisite)
		e.SuggestedFix = "start dpkms (`dpkms serve`), or point server.url or --server at a running instance"
		return e
	}
	return err
}
