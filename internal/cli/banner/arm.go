package banner

import (
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

var (
	// installObserver wraps http.DefaultTransport once per process, the
	// way kit's offline policy does, so every client built without its
	// own Transport reports the upgrade header.
	installObserver sync.Once
	// current is the running invocation's banner; nil prints nothing.
	current atomic.Pointer[Once]
)

// Arm sets up the upgrade banner for the command about to run: the first
// dpkms API response carrying upgrade.HeaderName prints one line on cmd's
// stderr, whichever instance, local or remote, the command talks to.
// Like kit's hints it stays silent under --quiet or --no-hints, or when
// tty reports that stderr is not a terminal. Call it from the root
// command's persistent pre-run.
func Arm(cmd *cobra.Command, tty func(io.Writer) bool) {
	installObserver.Do(func() {
		http.DefaultTransport = dpkmsclient.ObserveUpgrade(http.DefaultTransport, show)
	})
	w := cmd.ErrOrStderr()
	quiet, _ := cmd.Flags().GetBool("quiet")
	noHints, _ := cmd.Flags().GetBool("no-hints")
	if quiet || noHints || !tty(w) {
		current.Store(nil)
		return
	}
	current.Store(NewOnce(w))
}

func show(st upgrade.Status) {
	if o := current.Load(); o != nil {
		o.Show(st)
	}
}

// IsTerminal reports whether w is a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) // #nosec G115 -- file descriptors fit in int
}
