package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"hop.top/kit/go/core/netpolicy"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// offlineProbeURL is a non-loopback address that never leaves the host:
// a dial to 0.0.0.0 reaches the local stack, and nothing serves port 9,
// so a request that gets past the policy fails with connection refused.
const offlineProbeURL = "http://0.0.0.0:9"

// --offline is kit's global. kit guards http.DefaultTransport and stamps
// the command context; a dpkms client request honours it only when it is
// built on cmd.Context(). One case per client command family.
func TestOffline_KitRefusesClientCommands(t *testing.T) {
	content := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(content, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		leaf *cobra.Command
		args []string
	}{
		{"healthcheck", healthcheckCmd, []string{"healthcheck"}},
		{"pipeline list", pipelineListCmd, []string{"pipeline", "list"}},
		{"pipeline show", pipelineShowCmd, []string{"pipeline", "show", "p"}},
		{"pipeline enqueue", pipelineEnqueueCmd, []string{"pipeline", "enqueue", "--wait", content}},
		{"step list", stepListCmd, []string{"pipeline", "step", "list"}},
		{"step registry list", stepRegistryListCmd, []string{"pipeline", "step", "registry", "list"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hermeticConfigEnv(t)
			// cobra keeps a subcommand's context across in-process
			// runs, so drop the marker an --offline run stamped on it.
			reset := func() { tc.leaf.SetContext(context.Background()) }
			reset()
			t.Cleanup(reset)

			args := append([]string{"--offline", "--server-url", offlineProbeURL}, tc.args...)
			_, err := runWithConfigFlag(t, args...)
			if !errors.Is(err, netpolicy.ErrOffline) {
				t.Fatalf("--offline %s: err = %v, want netpolicy.ErrOffline", tc.name, err)
			}

			reset()
			args = append([]string{"--server-url", offlineProbeURL}, tc.args...)
			_, err = runWithConfigFlag(t, args...)
			if err == nil || errors.Is(err, netpolicy.ErrOffline) {
				t.Fatalf("%s without --offline: err = %v, want a dial error, not ErrOffline", tc.name, err)
			}
		})
	}
}

// Every APIClient call builds its request on the ctx it is given, with
// and without a bearer token: an offline-marked ctx is refused by kit, a
// cancelled one aborts, and a plain one reaches the dialer. Covers the
// calls no read-only CLI case can reach on its own (writes, GetJob behind
// enqueue --wait).
func TestAPIClient_RequestsCarryContext(t *testing.T) {
	calls := map[string]func(context.Context, *APIClient) error{
		"CreatePipeline": func(ctx context.Context, c *APIClient) error {
			_, err := c.CreatePipeline(ctx, service.CreatePipelineRequest{Name: "p"}, "")
			return err
		},
		"DeletePipeline":    func(ctx context.Context, c *APIClient) error { return c.DeletePipeline(ctx, "p", "") },
		"ArchivePipeline":   func(ctx context.Context, c *APIClient) error { return c.ArchivePipeline(ctx, "p", "") },
		"UnarchivePipeline": func(ctx context.Context, c *APIClient) error { return c.UnarchivePipeline(ctx, "p", "") },
		"ListPipelines": func(ctx context.Context, c *APIClient) error {
			_, _, err := c.ListPipelines(ctx, storage.PipelineFilter{})
			return err
		},
		"GetPipeline": func(ctx context.Context, c *APIClient) error {
			_, err := c.GetPipeline(ctx, "p")
			return err
		},
		"Enqueue": func(ctx context.Context, c *APIClient) error {
			_, err := c.Enqueue(ctx, service.AnalyzeRequest{Content: "x"})
			return err
		},
		"ListSteps": func(ctx context.Context, c *APIClient) error {
			_, _, err := c.ListSteps(ctx, "")
			return err
		},
		"GetStep": func(ctx context.Context, c *APIClient) error {
			_, err := c.GetStep(ctx, "s")
			return err
		},
		"InstallStep":    func(ctx context.Context, c *APIClient) error { return c.InstallStep(ctx, "s", "r") },
		"UninstallStep":  func(ctx context.Context, c *APIClient) error { return c.UninstallStep(ctx, "s") },
		"FetchRegistry":  func(ctx context.Context, c *APIClient) error { return c.FetchRegistry(ctx, "r") },
		"UpdateRegistry": func(ctx context.Context, c *APIClient) error { return c.UpdateRegistry(ctx, "r") },
		"ListRegistries": func(ctx context.Context, c *APIClient) error {
			_, _, err := c.ListRegistries(ctx)
			return err
		},
		"GetJob": func(ctx context.Context, c *APIClient) error {
			_, err := c.GetJob(ctx, "j")
			return err
		},
		"Healthz": func(ctx context.Context, c *APIClient) error {
			_, _, err := c.Healthz(ctx)
			return err
		},
	}
	clients := map[string]func() *APIClient{
		"plain":  func() *APIClient { return NewAPIClient(offlineProbeURL) },
		"bearer": func() *APIClient { return newEndpointClient(dpkmsclient.Endpoint{URL: offlineProbeURL, Token: "t"}) },
	}
	for cname, mk := range clients {
		for name, call := range calls {
			t.Run(cname+"/"+name, func(t *testing.T) {
				c := mk()
				if err := call(netpolicy.WithOffline(t.Context(), true), c); !errors.Is(err, netpolicy.ErrOffline) {
					t.Errorf("offline ctx: err = %v, want netpolicy.ErrOffline", err)
				}
				cancelled, cancel := context.WithCancel(t.Context())
				cancel()
				if err := call(cancelled, c); !errors.Is(err, context.Canceled) {
					t.Errorf("cancelled ctx: err = %v, want context.Canceled", err)
				}
				if err := call(t.Context(), c); err == nil || errors.Is(err, netpolicy.ErrOffline) {
					t.Errorf("plain ctx: err = %v, want a dial error, not ErrOffline", err)
				}
			})
		}
	}
}
