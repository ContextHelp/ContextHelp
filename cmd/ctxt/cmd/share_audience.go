package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt share audience: the audiences of the instance (ADR-080 §1), the
// named groups of principals a share policy is granted to. Every
// command calls the sharing routes under admin:shares.

var shareAudienceCmd = &cobra.Command{
	Use:   "audience",
	Short: "Manage audiences: the groups of people things are shared with",
	Long: `Manage audiences. An audience is a named group of principals, such as
colleagues or partners; a grant gives an audience a share policy, and
every member sees what the policy allows. A principal can be in several
audiences, and a membership can expire.

Examples:
  ctxt share audience create partners
  ctxt share audience add partners prn_01J... --expires 90d
  ctxt share audience list`,
}

var shareAudienceCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an audience",
	Long: `Create an audience. The name is a lowercase slug (letters, digits, dots,
dashes and underscores, 64 characters at most) and must be unique on
the instance. The new audience has no members and no grants.

Examples:
  ctxt share audience create partners`,
	Args: cobra.ExactArgs(1),
	RunE: runShareAudienceCreate,
}

var shareAudienceAddCmd = &cobra.Command{
	Use:   "add <audience> <principal-id>",
	Short: "Add a principal to an audience",
	Long: `Add a principal to an audience, or change the expiry of a membership it
already has. --expires ends the membership after a duration (90d, 720h),
at the end of a date (2026-12-31) or at an RFC 3339 time; without it the
membership lasts until removed. The audience may be named by name or id;
the principal must be active, and the owner joins no audience.

Examples:
  ctxt share audience add partners prn_01J...
  ctxt share audience add partners prn_01J... --expires 90d`,
	Args: cobra.ExactArgs(2),
	RunE: runShareAudienceAdd,
}

var shareAudienceRemoveCmd = &cobra.Command{
	Use:   "remove <audience> <principal-id>",
	Short: "Remove a principal from an audience",
	Long: `Remove a principal from an audience. It stops seeing what the audience's
grants gave it at once; its other memberships are untouched.

Examples:
  ctxt share audience remove partners prn_01J...`,
	Args: cobra.ExactArgs(2),
	RunE: runShareAudienceRemove,
}

var shareAudienceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List audiences with their members",
	Long: `List every audience, by name, with the principals in it. An expired
membership is listed with its expiry; --format json carries every field.

Examples:
  ctxt share audience list
  ctxt share audience list --format json`,
	Args: cobra.NoArgs,
	RunE: runShareAudienceList,
}

func init() {
	shareCmd.AddCommand(shareAudienceCmd)
	shareAudienceCmd.AddCommand(shareAudienceCreateCmd, shareAudienceAddCmd, shareAudienceRemoveCmd, shareAudienceListCmd)
	for _, c := range []*cobra.Command{shareAudienceCreateCmd, shareAudienceAddCmd, shareAudienceRemoveCmd, shareAudienceListCmd} {
		c.Flags().String("server", "", serverFlagUsage)
	}
	shareAudienceAddCmd.Flags().String("expires", "", "when the membership ends: a duration (90d, 720h), a date (2026-12-31) or an RFC 3339 time")

	cliconv.WithSideEffect(shareAudienceCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareAudienceCreateCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(shareAudienceCreateCmd, []cliconv.Example{
		{Title: "An audience for partners", Command: "ctxt share audience create partners"},
	})
	cliconv.WithNextSteps(shareAudienceCreateCmd, []cliconv.NextStep{
		{When: "to put people in it", Suggest: "ctxt share audience add <audience> <principal-id>", Reason: "a membership is what a grant applies to"},
		{When: "to give it a policy", Suggest: "ctxt share grant <audience> <policy>", Reason: "members see nothing until a grant applies"},
	})

	// Adding replaces an existing membership: the same call twice leaves
	// one row.
	cliconv.WithSideEffect(shareAudienceAddCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareAudienceAddCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareAudienceAddCmd, []cliconv.Example{
		{Title: "Add a person", Command: "ctxt share audience add partners prn_01J..."},
		{Title: "For ninety days", Command: "ctxt share audience add partners prn_01J... --expires 90d"},
	})
	cliconv.WithNextSteps(shareAudienceAddCmd, []cliconv.NextStep{
		{When: "to check who is in", Suggest: "ctxt share audience list", Reason: "members and their expiry per audience"},
	})

	cliconv.WithSideEffect(shareAudienceRemoveCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareAudienceRemoveCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareAudienceRemoveCmd, []cliconv.Example{
		{Title: "Remove a person", Command: "ctxt share audience remove partners prn_01J..."},
	})
	cliconv.WithNextSteps(shareAudienceRemoveCmd, []cliconv.NextStep{
		{When: "after removing", Suggest: "ctxt share audience list", Reason: "confirm the member is gone"},
		{When: "to shut the person out entirely", Suggest: "ctxt principal revoke <principal-id> --confirm-token=<sha>", Reason: "revoking ends every credential too"},
	})

	cliconv.WithSideEffect(shareAudienceListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(shareAudienceListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareAudienceListCmd, []cliconv.Example{
		{Title: "Every audience", Command: "ctxt share audience list"},
	})
}

// sharePath is a path under /api/v1/share, each segment escaped.
func sharePath(segments ...string) string {
	parts := make([]string, 0, len(segments))
	for _, s := range segments {
		parts = append(parts, url.PathEscape(strings.TrimSpace(s)))
	}
	return "/api/v1/share/" + strings.Join(parts, "/")
}

// audienceReply is an audience with its memberships.
type audienceReply struct {
	storage.Audience
	Members []storage.AudienceMember `json:"members"`
}

// expiryBody resolves --expires into the body field the sharing routes
// read; nil when unset.
func expiryBody(cmd *cobra.Command) (map[string]any, error) {
	expires, _ := cmd.Flags().GetString("expires")
	if strings.TrimSpace(expires) == "" {
		return map[string]any{}, nil
	}
	now := time.Now().UTC()
	at, err := cli.ParseExpiry(expires, now)
	if err != nil {
		return nil, err
	}
	if !at.After(now) {
		return nil, output.UsageError(fmt.Sprintf("--expires %q is not in the future", expires))
	}
	return map[string]any{"expires_at": at.Format(time.RFC3339)}, nil
}

func runShareAudienceCreate(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if err := storage.ValidateSharingName("audience", name); err != nil {
		return output.UsageError(err.Error())
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would create audience %q on %s\n", name, client.URL())
		return nil
	}
	var a audienceReply
	if err := client.Post(cmdContext(cmd), sharePath("audiences"), map[string]any{"name": name}, &a); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), a)
	}
	fmt.Fprintln(cmd.OutOrStdout(), a.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created audience %s (%s).\n", a.Name, a.ID)
	return nil
}

func runShareAudienceAdd(cmd *cobra.Command, args []string) error {
	body, err := expiryBody(cmd)
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	audience, principal := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	path := sharePath("audiences", audience, "members", principal)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would add principal %s to audience %s through %s on %s\n", principal, audience, path, client.URL())
		return nil
	}
	var m storage.AudienceMember
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "PUT", Path: path, Body: body}, &m); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), m)
	}
	until := ""
	if m.ExpiresAt != nil {
		until = ", until " + fmtStamp(*m.ExpiresAt)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added %s to %s%s.\n", m.PrincipalID, audience, until)
	return nil
}

func runShareAudienceRemove(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	audience, principal := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	path := sharePath("audiences", audience, "members", principal)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would remove principal %s from audience %s through %s on %s\n", principal, audience, path, client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"audience": audience, "principal_id": principal, "removed": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s.\n", principal, audience)
	return nil
}

// audienceRow is the row shape the tabular formats project from.
type audienceRow struct {
	ID      string `table:"ID"`
	Name    string `table:"Name"`
	Members string `table:"Members"`
	Created string `table:"Created"`
}

func renderAudiences(cmd *cobra.Command, w io.Writer, list []audienceReply) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"audiences": list, "total": len(list)})
	}
	now := time.Now()
	rows := make([]audienceRow, 0, len(list))
	for _, a := range list {
		members := make([]string, 0, len(a.Members))
		for _, m := range a.Members {
			label := m.PrincipalID
			if m.ExpiresAt != nil {
				if m.ExpiresAt.Before(now) {
					label += " (expired)"
				} else {
					label += " (until " + fmtStamp(*m.ExpiresAt) + ")"
				}
			}
			members = append(members, label)
		}
		rows = append(rows, audienceRow{ID: a.ID, Name: a.Name, Members: strings.Join(members, ", "), Created: fmtStamp(a.CreatedAt)})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Name, r.Members, r.Created})
	}
	printTable(w, []string{"ID", "NAME", "MEMBERS", "CREATED"}, table)
	return nil
}

func runShareAudienceList(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[audienceReply]
	if err := client.Get(cmdContext(cmd), sharePath("audiences"), nil, &reply); err != nil {
		return err
	}
	return renderAudiences(cmd, cmd.OutOrStdout(), reply.Data)
}
