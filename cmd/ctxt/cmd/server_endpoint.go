package cmd

import (
	"context"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/spf13/cobra"
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

// serverGet issues GET <ep.URL><path> with ep's bearer token, if any.
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
		return nil, err
	}
	return resp, nil
}
