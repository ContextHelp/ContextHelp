package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt share grant | revoke | ceiling | template: grants bind an
// audience to a policy (ADR-080 §1), a ceiling is the classes a
// principal may never see whatever its grants say (§2), and the
// templates are the built-in policies of §2.

var shareGrantCmd = &cobra.Command{
	Use:   "grant <audience> <policy>",
	Short: "Give an audience a share policy",
	Long: `Grant a share policy to an audience: every member of the audience sees
what the policy allows, from this moment until the grant expires or is
revoked. --expires takes a duration (30d, 720h), a date (2026-12-31) or
an RFC 3339 time. An audience with several grants gives its members the
union of what they allow. The audience and the policy may be named by
name or id.

Examples:
  ctxt share grant partners partners-network
  ctxt share grant colleagues colleagues-network --expires 90d`,
	Args: cobra.ExactArgs(2),
	RunE: runShareGrant,
}

var shareRevokeCmd = &cobra.Command{
	Use:   "revoke <grant-id>",
	Short: "Revoke a grant",
	Long: `Revoke a grant: the audience stops seeing what the policy gave it at
once. The grant stays on record as revoked. Grant ids come from
'ctxt share grant' and from 'ctxt share audience list --format json'.

Examples:
  ctxt share revoke grt_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runShareRevoke,
}

var shareCeilingCmd = &cobra.Command{
	Use:   "ceiling <principal-id>",
	Short: "Set the classes a principal may never see",
	Long: `Set a principal's ceiling: the content classes it never sees, whatever
its grants say. A partner who is also a colleague keeps both views and
still never sees a class listed here. --deny-classes replaces the list;
an empty value clears it.

Examples:
  ctxt share ceiling prn_01J... --deny-classes personal
  ctxt share ceiling prn_01J... --deny-classes personal,financial
  ctxt share ceiling prn_01J... --deny-classes ""`,
	Args: cobra.ExactArgs(1),
	RunE: runShareCeiling,
}

var shareTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "The built-in share policies",
	Long: `The built-in share policies: colleagues-network (a contact network
without contact details) and partners-network (with them). Neither shows
personal notes. Install one as a policy, then edit it.

Examples:
  ctxt share template list
  ctxt share template install partners-network`,
}

var shareTemplateListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List the built-in share policies",
	Long: `List the built-in share policies with their data filter. --format json
carries each whole document.

Examples:
  ctxt share template list`,
	Args: cobra.NoArgs,
	RunE: runShareTemplateList,
}

var shareTemplateInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install a built-in share policy",
	Long: `Install a built-in share policy as a policy of the same name, at version
1. A policy of that name already on the instance is left alone and the
install is refused: edit it instead, or create your own under another
name from 'ctxt share policy show'.

Examples:
  ctxt share template install partners-network`,
	Args: cobra.ExactArgs(1),
	RunE: runShareTemplateInstall,
}

func init() {
	shareCmd.AddCommand(shareGrantCmd, shareRevokeCmd, shareCeilingCmd, shareTemplateCmd)
	shareTemplateCmd.AddCommand(shareTemplateListCmd, shareTemplateInstallCmd)
	for _, c := range []*cobra.Command{shareGrantCmd, shareRevokeCmd, shareCeilingCmd, shareTemplateListCmd, shareTemplateInstallCmd} {
		c.Flags().String("server", "", serverFlagUsage)
	}
	shareGrantCmd.Flags().String("expires", "", "when the grant ends: a duration (30d, 720h), a date (2026-12-31) or an RFC 3339 time")
	shareCeilingCmd.Flags().StringSlice("deny-classes", nil, "classes the principal never sees, comma-separated; empty clears the ceiling")
	markFlagsRequired(shareCeilingCmd, "deny-classes")

	// Each grant is a new row.
	cliconv.WithSideEffect(shareGrantCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareGrantCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(shareGrantCmd, []cliconv.Example{
		{Title: "Partners see the partner view", Command: "ctxt share grant partners partners-network"},
		{Title: "For ninety days", Command: "ctxt share grant colleagues colleagues-network --expires 90d"},
	})
	cliconv.WithNextSteps(shareGrantCmd, []cliconv.NextStep{
		{When: "to see who is covered", Suggest: "ctxt share audience list", Reason: "the audience's members are the viewers"},
		{When: "to end it", Suggest: "ctxt share revoke <grant-id>", Reason: "a revoked grant stops applying at once"},
	})

	cliconv.WithSideEffect(shareRevokeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareRevokeCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareRevokeCmd, []cliconv.Example{
		{Title: "Revoke a grant", Command: "ctxt share revoke grt_01J..."},
	})
	cliconv.WithNextSteps(shareRevokeCmd, []cliconv.NextStep{
		{When: "to grant again", Suggest: "ctxt share grant <audience> <policy>", Reason: "a revoked grant never comes back; a new one does"},
	})

	cliconv.WithSideEffect(shareCeilingCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareCeilingCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareCeilingCmd, []cliconv.Example{
		{Title: "Never personal notes", Command: "ctxt share ceiling prn_01J... --deny-classes personal"},
		{Title: "Clear the ceiling", Command: "ctxt share ceiling prn_01J... --deny-classes \"\""},
	})
	cliconv.WithNextSteps(shareCeilingCmd, []cliconv.NextStep{
		{When: "to see the principal", Suggest: "ctxt principal show <principal-id>", Reason: "its kind, roles and status"},
	})

	cliconv.WithSideEffect(shareTemplateListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(shareTemplateListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareTemplateListCmd, []cliconv.Example{
		{Title: "The built-in policies", Command: "ctxt share template list"},
	})
	cliconv.WithSideEffect(shareTemplateInstallCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(shareTemplateInstallCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(shareTemplateInstallCmd, []cliconv.Example{
		{Title: "Install the partner view", Command: "ctxt share template install partners-network"},
	})
	cliconv.WithNextSteps(shareTemplateInstallCmd, []cliconv.NextStep{
		{When: "to adapt it", Suggest: "ctxt share policy show <name>", Reason: "edit the YAML and send it back with policy edit"},
		{When: "to use it", Suggest: "ctxt share grant <audience> <name>", Reason: "a policy applies through a grant"},
	})
}

// grantReply is a grant with the names of what it binds.
type grantReply struct {
	storage.ShareGrant
	Audience      string `json:"audience"`
	Policy        string `json:"policy"`
	PolicyVersion int    `json:"policy_version"`
}

// ceilingReply is a principal's ceiling.
type ceilingReply struct {
	PrincipalID string   `json:"principal_id"`
	DenyClasses []string `json:"deny_classes"`
}

// templateReply is a built-in policy.
type templateReply struct {
	Name string         `json:"name"`
	Spec sharing.Policy `json:"spec"`
}

func runShareGrant(cmd *cobra.Command, args []string) error {
	body, err := expiryBody(cmd)
	if err != nil {
		return err
	}
	audience, policy := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	body["audience"], body["policy"] = audience, policy
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would grant policy %s to audience %s on %s\n", policy, audience, client.URL())
		return nil
	}
	var g grantReply
	if err := client.Post(cmdContext(cmd), sharePath("grants"), body, &g); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), g)
	}
	until := ""
	if g.ExpiresAt != nil {
		until = ", until " + fmtStamp(*g.ExpiresAt)
	}
	fmt.Fprintln(cmd.OutOrStdout(), g.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Granted %s (version %d) to %s%s.\n", g.Policy, g.PolicyVersion, g.Audience, until)
	return nil
}

func runShareRevoke(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := sharePath("grants", id)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would revoke grant %s through %s on %s\n", id, path, client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"id": id, "revoked": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked grant %s.\n", id)
	return nil
}

func runShareCeiling(cmd *cobra.Command, args []string) error {
	raw, _ := cmd.Flags().GetStringSlice("deny-classes")
	classes := make([]string, 0, len(raw))
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if err := storage.ValidateClassName(c); err != nil {
			return output.UsageError("--deny-classes: " + err.Error())
		}
		classes = append(classes, c)
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := "/api/v1/principals/" + url.PathEscape(id) + "/ceiling"
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would set the ceiling of principal %s to [%s] through %s on %s\n", id, strings.Join(classes, ", "), path, client.URL())
		return nil
	}
	var c ceilingReply
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "PUT", Path: path, Body: map[string]any{"deny_classes": classes}}, &c); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), c)
	}
	if len(c.DenyClasses) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Principal %s has no ceiling.\n", c.PrincipalID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Principal %s never sees: %s.\n", c.PrincipalID, strings.Join(c.DenyClasses, ", "))
	return nil
}

// templateRow is the row shape the tabular formats project from.
type templateRow struct {
	Name    string `table:"Name"`
	Data    string `table:"Data"`
	Exclude string `table:"Excludes"`
}

func renderTemplates(cmd *cobra.Command, w io.Writer, list []templateReply) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"templates": list, "total": len(list)})
	}
	rows := make([]templateRow, 0, len(list))
	for _, t := range list {
		rows = append(rows, templateRow{Name: t.Name, Data: t.Spec.Data, Exclude: strings.Join(t.Spec.ExcludeClasses, ", ")})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.Name, r.Data, r.Exclude})
	}
	printTable(w, []string{"NAME", "DATA", "EXCLUDES"}, table)
	return nil
}

func runShareTemplateList(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[templateReply]
	if err := client.Get(cmdContext(cmd), sharePath("templates"), nil, &reply); err != nil {
		return err
	}
	return renderTemplates(cmd, cmd.OutOrStdout(), reply.Data)
}

func runShareTemplateInstall(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if _, ok := sharing.Template(name); !ok {
		return output.UsageError(fmt.Sprintf("no such template %q: want %s or %s", name, sharing.TemplateColleagues, sharing.TemplatePartners))
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would install template %s as a share policy on %s\n", name, client.URL())
		return nil
	}
	var p storage.SharePolicy
	if err := client.Post(cmdContext(cmd), sharePath("templates", name), nil, &p); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintln(cmd.OutOrStdout(), p.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Installed template %s as policy %s at version %d.\n", name, p.ID, p.Version)
	return nil
}
