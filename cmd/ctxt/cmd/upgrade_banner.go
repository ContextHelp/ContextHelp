package cmd

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/banner"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// upgradeBannerTTY reports whether the banner's writer is a terminal.
// Tests replace it.
var upgradeBannerTTY = isTerminal

var (
	// installUpgradeObserver wraps http.DefaultTransport once per
	// process, the way kit's offline policy does, so every dpkms client
	// built without its own Transport reports the upgrade header.
	installUpgradeObserver sync.Once
	// upgradeBannerSink is the current invocation's banner; nil prints
	// nothing.
	upgradeBannerSink atomic.Pointer[banner.Once]
)

// armUpgradeBanner sets up the ADR-070 upgrade banner for the command
// about to run: the first dpkms API response carrying the upgrade header
// prints one line on cmd's stderr, whichever instance, local or remote,
// the command talks to. Like kit's hints, it stays silent under --quiet
// or when stderr is not a terminal.
func armUpgradeBanner(cmd *cobra.Command) {
	installUpgradeObserver.Do(func() {
		http.DefaultTransport = dpkmsclient.ObserveUpgrade(http.DefaultTransport, showUpgradeBanner)
	})
	w := cmd.ErrOrStderr()
	if quiet, _ := cmd.Flags().GetBool("quiet"); quiet || !upgradeBannerTTY(w) {
		upgradeBannerSink.Store(nil)
		return
	}
	upgradeBannerSink.Store(banner.NewOnce(w))
}

func showUpgradeBanner(st upgrade.Status) {
	if o := upgradeBannerSink.Load(); o != nil {
		o.Show(st)
	}
}
