package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// ctxt share: sharing the instance with other people (ADR-080). The
// invite brings a person into the instance's principal registry
// through a link (ADR-079 §2), into an audience when --audience says
// so; audiences, policies, grants, ceilings and templates live in
// share_audience.go, share_policy.go and share_grant.go.

// defaultInviteExpiry is the invite lifetime when --expires is unset.
const defaultInviteExpiry = 7 * 24 * time.Hour

var shareCmd = &cobra.Command{
	Use:   "share",
	Short: "Share the instance with other people",
	Long: `Share the dpkms instance with other people.

An invite link brings a person in: they open it in a browser and arrive
in the web UI signed in as a new principal of kind person, with reader
roles. What they see comes from audiences and share policies: an
audience is a group of principals, a policy says what may be seen, a
grant gives an audience a policy, and an invite minted with --audience
puts the new person straight into that audience. Every command calls
the instance (--server, --instance, CTXT_INSTANCE, the current instance,
the first server.urls entry, or server.url) with its configured API
key, which must hold admin:shares: the owner's.

Examples:
  ctxt share template install partners-network
  ctxt share audience create partners
  ctxt share grant partners partners-network
  ctxt share invite --name "Dana Partner" --audience partners`,
}

var shareInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "Mint an invite link for a new person",
	Long: `Mint an invite link for a person and print it. --name is what they will
be called once they accept. The link works once, within --expires (a
duration such as 7d or 48h; 7 days by default), and the secret rides
after '#', so it never reaches a server log or a proxy. Hand it over on
a channel you trust. Accepting it creates the person principal and signs
their browser in. With --audience the person joins that audience as
they accept, so the audience's grants apply from their first request;
without it they see nothing until something is shared with them.

Examples:
  ctxt share invite --name "Dana Partner" --audience partners
  ctxt share invite --name "Sam Colleague" --expires 48h`,
	Args: cobra.NoArgs,
	RunE: runShareInvite,
}

func init() {
	rootCmd.AddCommand(shareCmd)
	shareCmd.AddCommand(shareInviteCmd)

	shareInviteCmd.Flags().String("name", "", "display name of the person invited")
	shareInviteCmd.Flags().String("expires", "", "how long the link can be accepted, as a duration (7d, 48h); 7 days by default")
	shareInviteCmd.Flags().String("audience", "", "audience (name or id) the person joins on accepting")
	markFlagsRequired(shareInviteCmd, "name")

	// Every run mints a different link.
	cliconv.WithSideEffect(shareInviteCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareInviteCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(shareInviteCmd, []cliconv.Example{
		{Title: "Invite a partner into the partners audience", Command: "ctxt share invite --name \"Dana Partner\" --audience partners"},
		{Title: "A link that lasts two days", Command: "ctxt share invite --name \"Sam Colleague\" --expires 48h"},
	})
	cliconv.WithNextSteps(shareInviteCmd, []cliconv.NextStep{
		{When: "once they accepted", Suggest: "ctxt principal list --kind person", Reason: "the new principal appears there"},
		{When: "to see what they can reach", Suggest: "ctxt share audience list", Reason: "their audience and its grants"},
		{When: "the link expired or was used", Suggest: "ctxt share invite --name <name>", Reason: "each link is single-use"},
	})
}

func runShareInvite(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	expires, _ := cmd.Flags().GetString("expires")
	audience, _ := cmd.Flags().GetString("audience")
	audience = strings.TrimSpace(audience)
	if strings.TrimSpace(name) == "" {
		return output.UsageError("--name is required")
	}
	ttl := defaultInviteExpiry
	if strings.TrimSpace(expires) != "" {
		now := time.Now().UTC()
		at, err := cli.ParseExpiry(expires, now)
		if err != nil {
			return err
		}
		if ttl = at.Sub(now); ttl <= 0 {
			return output.UsageError(fmt.Sprintf("--expires %q is not in the future", expires))
		}
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	into := ""
	if audience != "" {
		into = " into audience " + audience
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would mint an invite for %q%s valid %s on %s\n", name, into, ttl.Round(time.Second), client.URL())
		return nil
	}
	body := map[string]any{"display_name": name, "expires_in": int(ttl.Round(time.Second).Seconds())}
	if audience != "" {
		body["audience"] = audience
	}
	var link linkReply
	if err := client.Post(cmdContext(cmd), "/api/v1/share/invites", body, &link); err != nil {
		return err
	}
	return printLink(cmd, link, "Single-use invite link for "+strings.TrimSpace(name))
}
