package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// uiOpener opens the sign-in link. A variable so tests record the URL
// instead of launching a browser.
var uiOpener launch.Opener = launch.System{}

// uiTerminal reports whether a writer is a terminal. A variable so
// tests can stand in for a TTY.
var uiTerminal = isTerminal

// uiLoginTimeout bounds the login-code request.
const uiLoginTimeout = 15 * time.Second

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the dpkms web UI",
	Long: `Open the web UI of the configured dpkms instance in a browser.

Examples:
  # Sign in to the web UI of the configured instance
  ctxt ui open

  # Print the sign-in link instead of opening a browser
  ctxt ui open --no-browser`,
}

var uiOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Sign in to the dpkms web UI in a browser",
	Long: `Open the dpkms web UI signed in as the principal of your API token.

ctxt asks the instance (--server, --instance, CTXT_INSTANCE, the current
instance, the first server.urls entry, or server.url) for
a single-use sign-in link, authenticating with its configured token, and
opens it in your default browser. The link carries the code after '#',
so it never reaches the server's or a proxy's logs; the web UI trades it
for a session cookie within 60 seconds. The token itself never enters
the browser.

The session acts as your token's principal with a reduced scope: reads,
search and the web UI's own actions. It ends after 12 hours without use,
7 days after sign-in, when you sign out, when an operator runs
'dpkms session revoke', or when the token is removed from the server.

A private instance needs no sign-in: this opens its /ui/ as is.

The browser opens only when stdout and stderr are both terminals and
--no-browser is not set; otherwise the link is printed on stdout.

Examples:
  ctxt ui open
  ctxt ui open --server https://dpkms.example.net
  ctxt ui open --no-browser`,
	Args: cobra.NoArgs,
	RunE: runUIOpen,
}

func init() {
	rootCmd.AddCommand(uiCmd)
	uiCmd.AddCommand(uiOpenCmd)

	uiOpenCmd.Flags().String("server", "", serverFlagUsage)
	uiOpenCmd.Flags().Bool("no-browser", false, "print the sign-in link instead of opening a browser")

	// Mints a short-lived login code on the server. Each run mints a new
	// one, so it is not idempotent.
	cliconv.WithSideEffect(uiOpenCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(uiOpenCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(uiOpenCmd, []cliconv.Example{
		{Title: "Sign in to the web UI", Command: "ctxt ui open"},
		{Title: "Sign in to another instance", Command: "ctxt ui open --server https://dpkms.example.net"},
		{Title: "Print the link instead of opening a browser", Command: "ctxt ui open --no-browser"},
	})
	cliconv.WithNextSteps(uiOpenCmd, []cliconv.NextStep{
		{When: "the link expired or was used", Suggest: "ctxt ui open", Reason: "each link is single-use and valid for 60 seconds"},
		{When: "to sign a browser out remotely", Suggest: "dpkms session revoke <session-id>", Reason: "list sessions with dpkms session list"},
	})
}

// uiLink is the result of ctxt ui open.
type uiLink struct {
	// URL opens the web UI; on a protected or public instance it carries
	// the single-use code in its fragment.
	URL string `json:"url"`
	// SessionRequired is false on a private instance.
	SessionRequired bool      `json:"session_required"`
	ExpiresAt       time.Time `json:"expires_at,omitzero"`
	Opened          bool      `json:"opened"`
	DryRun          bool      `json:"dry_run,omitempty"`
}

type loginCodeReply struct {
	SessionRequired bool      `json:"session_required"`
	Code            string    `json:"code"`
	ExpiresAt       time.Time `json:"expires_at"`
	LoginPath       string    `json:"login_path"`
}

func runUIOpen(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := newDpkmsClient(cmd, uiLoginTimeout)
	if err != nil {
		return err
	}
	noBrowser, _ := cmd.Flags().GetBool("no-browser")
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	base := client.URL()

	if kitcli.IsDryRun(cmd) {
		link := uiLink{URL: base + "/ui/", DryRun: true}
		if isJSONOutput() {
			return writeUILink(cmd, stdout, link)
		}
		fmt.Fprintf(stdout, "Would request a single-use sign-in link from %s and open %s/ui/auth\n", base, base)
		return nil
	}

	link, err := mintUILink(ctx, client)
	if err != nil {
		return err
	}
	if !noBrowser && uiTerminal(stdout) && uiTerminal(stderr) {
		if err := uiOpener.Open(ctx, link.URL); err != nil {
			fmt.Fprintf(stderr, "Warning: could not open a browser (%v); open this link instead:\n", err)
		} else {
			link.Opened = true
		}
	}
	if isJSONOutput() {
		if link.Opened {
			// The link was spent on the browser; never print a code twice.
			link.URL = redactLoginCode(link.URL)
		}
		return writeUILink(cmd, stdout, link)
	}
	switch {
	case link.Opened && link.SessionRequired:
		fmt.Fprintf(stderr, "Opened the web UI sign-in for %s in your browser (single-use link, valid until %s).\n",
			base, link.ExpiresAt.Local().Format("15:04:05"))
	case link.Opened:
		fmt.Fprintf(stderr, "Opened %s in your browser (private instance: no sign-in needed).\n", link.URL)
	case link.SessionRequired:
		fmt.Fprintf(stderr, "Single-use sign-in link for %s, valid until %s:\n", base, link.ExpiresAt.Local().Format("15:04:05"))
		fmt.Fprintln(stdout, link.URL)
	default:
		fmt.Fprintln(stderr, "Private instance: no sign-in needed.")
		fmt.Fprintln(stdout, link.URL)
	}
	return nil
}

// mintUILink asks the endpoint for a login code and builds the sign-in
// URL.
func mintUILink(ctx context.Context, client *dpkmsclient.Client) (uiLink, error) {
	base := client.URL()
	var reply loginCodeReply
	err := client.Post(ctx, "/api/v1/ui/login-codes", nil, &reply)
	var remote *dpkmsclient.RemoteError
	var oe *output.Error
	switch {
	case err == nil:
	case errors.As(err, &remote) && remote.StatusCode == gohttp.StatusNotFound:
		e := output.WrapError(fmt.Errorf("dpkms at %s has no web UI sign-in", base),
			output.CodePrerequisite, output.ExitPrerequisite)
		e.SuggestedFix = "upgrade dpkms on that host"
		return uiLink{}, e
	case errors.As(err, &remote) && errors.As(err, &oe) && oe.Code == output.CodeUnauthorized:
		e := output.WrapError(fmt.Errorf("dpkms at %s refused to mint a sign-in link (%d): %s",
			base, remote.StatusCode, remoteMessage(remote)), output.CodeUnauthorized, output.ExitUnauthorized)
		e.SuggestedFix = "set server.token (or the token of this server.urls entry) to one of the server's server.auth.static.tokens"
		return uiLink{}, e
	default:
		return uiLink{}, err
	}
	if !reply.SessionRequired {
		return uiLink{URL: base + "/ui/"}, nil
	}
	if reply.Code == "" || !strings.HasPrefix(reply.LoginPath, "/") {
		return uiLink{}, fmt.Errorf("dpkms at %s sent an unusable login code response", base)
	}
	return uiLink{
		URL:             base + reply.LoginPath + "#code=" + reply.Code,
		SessionRequired: true,
		ExpiresAt:       reply.ExpiresAt,
	}, nil
}

// remoteMessage is the message of dpkms's error envelope, or the trimmed
// body.
func remoteMessage(e *dpkmsclient.RemoteError) string {
	if e.Message != "" {
		return e.Message
	}
	return strings.TrimSpace(string(e.Body))
}

// redactLoginCode drops the code from a sign-in URL's fragment.
func redactLoginCode(u string) string {
	if i := strings.Index(u, "#code="); i >= 0 {
		return u[:i]
	}
	return u
}

func writeUILink(cmd *cobra.Command, w io.Writer, link uiLink) error {
	return cliformat.EncodeTo(cmd, w, link)
}
