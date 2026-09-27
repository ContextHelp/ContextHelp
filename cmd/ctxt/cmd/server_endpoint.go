package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/spf13/cobra"
)

// serverFlagUsage is the --server help text of every command that talks
// to dpkms over its API.
const serverFlagUsage = "dpkms server URL; overrides --instance, CTXT_INSTANCE, the current instance, " +
	"server.urls and server.url (default " + dpkmsclient.DefaultURL + " when nothing is configured)"

// resolveEndpoint picks the one dpkms endpoint this invocation talks to
// (ADR-077 §3): --server, then --instance or CTXT_INSTANCE, then the
// current-instance state, then the first server.urls entry, then
// server.url, then the default. An unknown or stale name is a
// PREREQUISITE error (exit 70). There is no failover.
func resolveEndpoint(cmd *cobra.Command) (dpkmsclient.Resolved, error) {
	var sel dpkmsclient.Selection
	if f := cmd.Flag("server"); f != nil && f.Changed {
		sel.Server = f.Value.String()
	}
	sel.Instance, sel.InstanceLayer = instanceSelector(cmd)
	sel.Current = currentInstance()
	var sc config.ServerConfig
	if cfg != nil {
		sc = cfg.Server
	}
	return dpkmsclient.Resolve(sc, sel, localInstances)
}

// instanceSelector returns the --instance flag, else CTXT_INSTANCE, and
// the layer it came from.
func instanceSelector(cmd *cobra.Command) (string, dpkmsclient.Layer) {
	if f := cmd.Flag("instance"); f != nil && f.Changed {
		return strings.TrimSpace(f.Value.String()), dpkmsclient.LayerInstanceFlag
	}
	if v := strings.TrimSpace(os.Getenv("CTXT_INSTANCE")); v != "" {
		return v, dpkmsclient.LayerInstanceEnv
	}
	return "", ""
}

// currentInstance returns the selection `ctxt instance use` persisted,
// or "" when there is none.
func currentInstance() string {
	stateFile, err := config.CurrentInstanceFile()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// localInstances lists the dpkms instances running on this machine, from
// their pidfiles.
func localInstances() ([]pidfile.Info, error) {
	runDir, err := config.RunDir()
	if err != nil {
		return nil, err
	}
	return pidfile.Scan(runDir)
}

// newDpkmsClient resolves this invocation's endpoint and returns a client
// for it. timeout bounds each request; 0 means none.
func newDpkmsClient(cmd *cobra.Command, timeout time.Duration) (*dpkmsclient.Client, error) {
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return nil, err
	}
	return dpkmsclient.New(r.Endpoint, dpkmsclient.WithTimeout(timeout))
}

// bridgeEndpoints resolves this invocation's endpoint for the commands
// still routed through idxbridge. The list always holds exactly one
// endpoint, so the bridge never walks to another instance.
func bridgeEndpoints(cmd *cobra.Command) ([]idxbridge.Endpoint, error) {
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return nil, err
	}
	return []idxbridge.Endpoint{{URL: r.URL, Token: r.Token}}, nil
}
