package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"hop.top/kit/go/core/netpolicy"
)

// fakeNet stands in for the network: a request to a loopback host goes
// to the real transport (in-process test servers), anything else is
// recorded and fails without dialing.
type fakeNet struct {
	loopback http.RoundTripper
	mu       sync.Mutex
	hits     []string
}

var errFakeNet = errors.New("fake network: request reached the transport")

func (f *fakeNet) RoundTrip(req *http.Request) (*http.Response, error) {
	if ip := net.ParseIP(req.URL.Hostname()); ip != nil && ip.IsLoopback() || req.URL.Hostname() == "localhost" {
		return f.loopback.RoundTrip(req)
	}
	f.mu.Lock()
	f.hits = append(f.hits, req.URL.Redacted())
	f.mu.Unlock()
	return nil, errFakeNet
}

func (f *fakeNet) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

// useFakeNet routes http.DefaultTransport through fakeNet, under kit's
// offline guard as in a real run, for the rest of the test. reset are
// the commands whose context the test's runs stamp; cobra keeps a
// subcommand's context across in-process runs, so the offline marker is
// dropped between them.
func useFakeNet(t *testing.T, reset ...*cobra.Command) *fakeNet {
	t.Helper()
	prev := http.DefaultTransport
	f := &fakeNet{loopback: prev}
	http.DefaultTransport = netpolicy.Guard(f)
	t.Cleanup(func() {
		http.DefaultTransport = prev
		for _, c := range reset {
			c.SetContext(context.Background())
		}
	})
	return f
}

// offlineRemote is a non-loopback URL; only fakeNet ever sees it.
const offlineRemote = "http://192.0.2.10:9"

// offlineCase is one command that reaches the network.
type offlineCase struct {
	name  string
	cmd   *cobra.Command
	setup func(t *testing.T, db *testDB)
	args  []string
}

func offlineCases() []offlineCase {
	return []offlineCase{
		{
			name: "registry sync",
			cmd:  registrySyncCmd,
			setup: func(t *testing.T, db *testDB) {
				withRegistry(t, db, "remote", offlineRemote, "")
			},
			args: []string{"registry", "sync", "remote"},
		},
		{
			name: "registry sync --dry-run",
			cmd:  registrySyncCmd,
			setup: func(t *testing.T, db *testDB) {
				withRegistry(t, db, "remote", offlineRemote, "")
			},
			args: []string{"registry", "sync", "--dry-run", "remote"},
		},
		{name: "upgrade check", cmd: upgradeCheckCmd, args: []string{"upgrade", "check"}},
		{
			name: "import github",
			cmd:  importGitHubCmd,
			args: []string{"import", "github", "--username", "octocat", "--github-base-url", offlineRemote, "--dry-run"},
		},
	}
}

// TestOfflineBlocksNetworkPaths: under --offline each network command
// fails with netpolicy.ErrOffline and sends nothing; without it the
// same command does reach the transport, so the path is really covered.
func TestOfflineBlocksNetworkPaths(t *testing.T) {
	for _, tc := range offlineCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			if tc.setup != nil {
				tc.setup(t, db)
			}
			fake := useFakeNet(t, tc.cmd)

			_, err := db.exec(append([]string{"--offline"}, tc.args...)...)
			if !errors.Is(err, netpolicy.ErrOffline) {
				t.Fatalf("--offline %v: err = %v, want netpolicy.ErrOffline", tc.args, err)
			}
			if n := fake.count(); n != 0 {
				t.Fatalf("--offline %v sent %d request(s): %v", tc.args, n, fake.hits)
			}

			tc.cmd.SetContext(context.Background())
			_, _ = db.exec(tc.args...)
			if fake.count() == 0 {
				t.Fatalf("%v without --offline never reached the transport; the test covers nothing", tc.args)
			}
		})
	}
}

// TestOfflineKeepsLoopbackDpkms: --offline still reaches a dpkms on
// loopback.
func TestOfflineKeepsLoopbackDpkms(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()
	fake := useFakeNet(t, feedListCmd)

	if _, err := executeCommand("--offline", "feed", "list", "--server", srv.URL); err != nil {
		t.Fatalf("--offline feed list against loopback dpkms: %v", err)
	}
	if n := fake.count(); n != 0 {
		t.Fatalf("loopback run sent %d non-loopback request(s): %v", n, fake.hits)
	}
}
