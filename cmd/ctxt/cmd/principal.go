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

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt principal: the principal registry of the dpkms instance, over
// the API under admin:principals (ADR-079 §5). The owner's admin key
// creates people, devices and federation peers, binds identities to
// them by hand, mints sign-in links, and suspends or revokes them.

// principalKinds a principal may be created as: never owner.
var principalKinds = []string{storage.PrincipalKindPerson, storage.PrincipalKindDevice, storage.PrincipalKindPeer}

// manualIdentityMethods are the methods 'principal identity add' binds.
var manualIdentityMethods = []string{storage.IdentityMethodProxy, storage.IdentityMethodOIDC}

var principalCmd = &cobra.Command{
	Use:   "principal",
	Short: "Manage the principals of the dpkms instance (admin)",
	Long: `Manage who the dpkms instance knows: people invited to it, capture
devices and federation peers, each a principal with its own keys,
identities and sessions.

Every command calls the instance (--server, --instance, CTXT_INSTANCE,
the current instance, the first server.urls entry, or server.url) with
its configured API key, which must hold admin:principals: the owner's.
Keys for a principal come from 'ctxt auth key create --principal <id>'.

Examples:
  # A federation peer and its push key
  ctxt principal create --kind peer --name "partner instance"
  ctxt auth key create --principal prn_01J... --name "partner push"

  # A sign-in link for a person who lost their session
  ctxt principal link prn_01J...`,
}

var principalListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List principals",
	Long: `List the principals of the instance, oldest first. --kind and --status
narrow the list.

Examples:
  ctxt principal list
  ctxt principal list --kind person --status active`,
	Args: cobra.NoArgs,
	RunE: runPrincipalList,
}

var principalShowCmd = &cobra.Command{
	Use:   "show <principal-id>",
	Short: "Show a principal with its identities and keys",
	Long: `Show one principal: its kind, roles and status, then every identity
bound to it and every key, revoked ones included.

Examples:
  ctxt principal show prn_01J...
  ctxt principal show owner --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalShow,
}

var principalCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a person, device or peer principal",
	Long: `Create a principal. --kind is person, device or peer; --name is how it
is shown. Without --role a person reads, a device and a peer write
(repeat --role to set admin, writer or reader). The new principal has
no credential yet: create a key with 'ctxt auth key create --principal',
or send a person a sign-in link with 'ctxt principal link'. People you
invite with 'ctxt share invite' are created when they accept.

Examples:
  ctxt principal create --kind peer --name "partner instance"
  ctxt principal create --kind device --name "phone" --role writer`,
	Args: cobra.NoArgs,
	RunE: runPrincipalCreate,
}

var principalSuspendCmd = &cobra.Command{
	Use:   "suspend <principal-id>",
	Short: "Suspend a principal",
	Long: `Suspend a principal: its keys, links and sessions stop working until it
is reinstated, and its credentials are kept. The owner cannot be
suspended.

Examples:
  ctxt principal suspend prn_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalSuspend,
}

var principalRevokeCmd = &cobra.Command{
	Use:   "revoke <principal-id>",
	Short: "Revoke a principal and every credential it holds",
	Long: `Revoke a principal for good: every identity, key and browser session it
holds is revoked in the same step, and nothing brings it back. The
owner cannot be revoked.

` + cliconv.DestructiveTokenHelp + `

Examples:
  ctxt principal revoke prn_01J... --confirm-token=<sha>`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalRevoke,
}

var principalLinkCmd = &cobra.Command{
	Use:   "link <principal-id>",
	Short: "Mint a single-use sign-in link for a principal",
	Long: `Mint a sign-in link for an existing principal and print it. The link
opens the web UI signed in as that principal; it works once, within 10
minutes, and is refused for a suspended or revoked principal. The
secret rides after '#', so it never reaches a server log or a proxy.
Hand it over on a channel you trust.

Examples:
  ctxt principal link prn_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalLink,
}

var principalIdentityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Bind, list and remove a principal's identities",
	Long: `Bind, list and remove the identities of a principal. Keys and links
bind themselves; 'add' binds a proxy login or an OIDC subject by hand.`,
}

var principalIdentityAddCmd = &cobra.Command{
	Use:   "add <principal-id>",
	Short: "Bind a proxy login or OIDC subject to a principal",
	Long: `Bind an identity to a principal by hand: --method proxy for a login a
trusted proxy will assert (--issuer names the preset, tailscale or
forward_auth), or --method oidc for an issuer and subject. The
principal must be active.

Examples:
  ctxt principal identity add prn_01J... --method proxy --issuer tailscale --subject alice@example.com
  ctxt principal identity add prn_01J... --method oidc --issuer https://login.example.com --subject 2f3a...`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalIdentityAdd,
}

var principalIdentityListCmd = &cobra.Command{
	Use:   "list <principal-id>",
	Short: "List a principal's identities",
	Long: `List every identity bound to a principal, revoked ones included.

Examples:
  ctxt principal identity list prn_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runPrincipalIdentityList,
}

var principalIdentityRemoveCmd = &cobra.Command{
	Use:   "remove <principal-id> <identity-id>",
	Short: "Remove an identity from a principal",
	Long: `Remove an identity from a principal. The credential behind it stops
working at once, with every browser session it signed in.

Examples:
  ctxt principal identity remove prn_01J... idn_01J...`,
	Args: cobra.ExactArgs(2),
	RunE: runPrincipalIdentityRemove,
}

func init() {
	rootCmd.AddCommand(principalCmd)
	principalCmd.AddCommand(principalListCmd, principalShowCmd, principalCreateCmd, principalSuspendCmd,
		principalRevokeCmd, principalLinkCmd, principalIdentityCmd)
	principalIdentityCmd.AddCommand(principalIdentityAddCmd, principalIdentityListCmd, principalIdentityRemoveCmd)

	principalListCmd.Flags().String("kind", "", "keep principals of one kind ("+strings.Join(storage.PrincipalKinds, "|")+")")
	principalListCmd.Flags().String("status", "", "keep principals in one status ("+strings.Join(storage.PrincipalStatuses, "|")+")")
	principalCreateCmd.Flags().String("kind", "", "kind of principal ("+strings.Join(principalKinds, "|")+")")
	principalCreateCmd.Flags().String("name", "", "display name")
	principalCreateCmd.Flags().StringSlice("role", nil, "role bundle (repeatable: admin, writer, reader); default by kind")
	markFlagsRequired(principalCreateCmd, "kind", "name")
	principalIdentityAddCmd.Flags().String("method", "", "identity method ("+strings.Join(manualIdentityMethods, "|")+")")
	principalIdentityAddCmd.Flags().String("issuer", "", "proxy preset or OIDC issuer")
	principalIdentityAddCmd.Flags().String("subject", "", "the login or subject the method asserts")
	principalIdentityAddCmd.Flags().String("name", "", "label for the identity")
	markFlagsRequired(principalIdentityAddCmd, "method", "subject")

	cliconv.WithSideEffect(principalListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(principalListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalListCmd, []cliconv.Example{
		{Title: "Every principal", Command: "ctxt principal list"},
		{Title: "Active people", Command: "ctxt principal list --kind person --status active"},
	})
	cliconv.WithSideEffect(principalShowCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(principalShowCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalShowCmd, []cliconv.Example{
		{Title: "One principal with its identities and keys", Command: "ctxt principal show prn_01J..."},
	})

	// Creating adds a row; each run adds another.
	cliconv.WithSideEffect(principalCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(principalCreateCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(principalCreateCmd, []cliconv.Example{
		{Title: "A federation peer", Command: "ctxt principal create --kind peer --name \"partner instance\""},
		{Title: "A capture device", Command: "ctxt principal create --kind device --name phone"},
	})
	cliconv.WithNextSteps(principalCreateCmd, []cliconv.NextStep{
		{When: "to give it a key", Suggest: "ctxt auth key create --principal <id>", Reason: "a principal authenticates with its own keys"},
		{When: "to sign a person in", Suggest: "ctxt principal link <id>", Reason: "a single-use link opens the web UI as them"},
	})

	cliconv.WithSideEffect(principalSuspendCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(principalSuspendCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalSuspendCmd, []cliconv.Example{
		{Title: "Suspend a principal", Command: "ctxt principal suspend prn_01J..."},
	})
	cliconv.WithNextSteps(principalSuspendCmd, []cliconv.NextStep{
		{When: "to check its state", Suggest: "ctxt principal show <id>", Reason: "status reads suspended; keys and sessions are kept"},
		{When: "to end it for good", Suggest: "ctxt principal revoke <id> --confirm-token=<sha>", Reason: "revoke cascades to every credential"},
	})

	// Revoking cascades to every credential and cannot be undone.
	cliconv.WithSideEffect(principalRevokeCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(principalRevokeCmd)
	cliconv.WithIdempotency(principalRevokeCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalRevokeCmd, []cliconv.Example{
		{Title: "Revoke a principal", Command: "ctxt principal revoke prn_01J... --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(principalRevokeCmd, []cliconv.NextStep{
		{When: "after revoking", Suggest: "ctxt principal list --status revoked", Reason: "confirm the principal is gone"},
	})

	// A link is a new credential each time.
	cliconv.WithSideEffect(principalLinkCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(principalLinkCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(principalLinkCmd, []cliconv.Example{
		{Title: "A sign-in link", Command: "ctxt principal link prn_01J..."},
	})
	cliconv.WithNextSteps(principalLinkCmd, []cliconv.NextStep{
		{When: "the link expired or was used", Suggest: "ctxt principal link <id>", Reason: "each link is single-use and valid for 10 minutes"},
	})

	cliconv.WithSideEffect(principalIdentityAddCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(principalIdentityAddCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(principalIdentityAddCmd, []cliconv.Example{
		{Title: "Bind a Tailscale login", Command: "ctxt principal identity add prn_01J... --method proxy --issuer tailscale --subject alice@example.com"},
	})
	cliconv.WithNextSteps(principalIdentityAddCmd, []cliconv.NextStep{
		{When: "to see what is bound", Suggest: "ctxt principal identity list <id>", Reason: "every identity of the principal, with its method"},
	})
	cliconv.WithSideEffect(principalIdentityListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(principalIdentityListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalIdentityListCmd, []cliconv.Example{
		{Title: "A principal's identities", Command: "ctxt principal identity list prn_01J..."},
	})
	cliconv.WithSideEffect(principalIdentityRemoveCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(principalIdentityRemoveCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(principalIdentityRemoveCmd, []cliconv.Example{
		{Title: "Remove an identity", Command: "ctxt principal identity remove prn_01J... idn_01J..."},
	})
	cliconv.WithNextSteps(principalIdentityRemoveCmd, []cliconv.NextStep{
		{When: "after removing", Suggest: "ctxt principal identity list <id>", Reason: "the row stays listed as revoked"},
	})
}

func principalPath(id string, rest ...string) string {
	return "/api/v1/principals/" + url.PathEscape(strings.TrimSpace(id)) + strings.Join(rest, "")
}

// linkReply is the body of a minted invite or sign-in link.
type linkReply struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	ExpiresIn int       `json:"expires_in"`
}

// principalRow is the row shape the tabular formats project from.
type principalRow struct {
	ID      string `table:"ID"`
	Kind    string `table:"Kind"`
	Name    string `table:"Name"`
	Roles   string `table:"Roles"`
	Status  string `table:"Status"`
	Created string `table:"Created"`
}

func renderPrincipals(cmd *cobra.Command, w io.Writer, list []storage.Principal) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"principals": list, "total": len(list)})
	}
	rows := make([]principalRow, 0, len(list))
	for _, p := range list {
		rows = append(rows, principalRow{ID: p.ID, Kind: p.Kind, Name: p.DisplayName,
			Roles: strings.Join(p.Roles, ","), Status: p.Status, Created: fmtStamp(p.CreatedAt)})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Kind, r.Name, r.Roles, r.Status, r.Created})
	}
	printTable(w, []string{"ID", "KIND", "NAME", "ROLES", "STATUS", "CREATED"}, table)
	return nil
}

func runPrincipalList(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	status, _ := cmd.Flags().GetString("status")
	if kind != "" && !contains(storage.PrincipalKinds, kind) {
		return output.UsageError(fmt.Sprintf("--kind %q: want one of %s", kind, strings.Join(storage.PrincipalKinds, ", ")))
	}
	if status != "" && !contains(storage.PrincipalStatuses, status) {
		return output.UsageError(fmt.Sprintf("--status %q: want one of %s", status, strings.Join(storage.PrincipalStatuses, ", ")))
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if status != "" {
		q.Set("status", status)
	}
	var reply apiList[storage.Principal]
	if err := client.Get(cmdContext(cmd), "/api/v1/principals", q, &reply); err != nil {
		return err
	}
	return renderPrincipals(cmd, cmd.OutOrStdout(), reply.Data)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func runPrincipalShow(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	ctx := cmdContext(cmd)
	var p storage.Principal
	if err := client.Get(ctx, principalPath(args[0]), nil, &p); err != nil {
		return err
	}
	var idns apiList[storage.Identity]
	if err := client.Get(ctx, principalPath(args[0], "/identities"), nil, &idns); err != nil {
		return err
	}
	var keys apiList[authn.KeyInfo]
	if err := client.Get(ctx, principalPath(args[0], "/keys"), nil, &keys); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, map[string]any{"principal": p, "identities": idns.Data, "keys": keys.Data})
	}
	fmt.Fprintf(out, "Principal: %s\nKind:      %s\nName:      %s\nRoles:     %s\nStatus:    %s\nCreated:   %s\n",
		p.ID, p.Kind, p.DisplayName, strings.Join(p.Roles, ", "), p.Status, fmtStamp(p.CreatedAt))
	if p.RevokedAt != nil {
		fmt.Fprintf(out, "Revoked:   %s\n", fmtStamp(*p.RevokedAt))
	}
	fmt.Fprintf(out, "\nIdentities (%d)\n", len(idns.Data))
	if err := renderIdentities(cmd, out, idns.Data); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nKeys (%d)\n", len(keys.Data))
	return renderKeys(cmd, out, keys.Data)
}

func runPrincipalCreate(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	name, _ := cmd.Flags().GetString("name")
	roles, _ := cmd.Flags().GetStringSlice("role")
	if !contains(principalKinds, kind) {
		return output.UsageError(fmt.Sprintf("--kind %q: want one of %s", kind, strings.Join(principalKinds, ", ")))
	}
	if strings.TrimSpace(name) == "" {
		return output.UsageError("--name is required")
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would create a %s principal %q on %s\n", kind, name, client.URL())
		return nil
	}
	body := map[string]any{"kind": kind, "display_name": name}
	if len(roles) > 0 {
		body["roles"] = roles
	}
	var p storage.Principal
	if err := client.Post(cmdContext(cmd), "/api/v1/principals", body, &p); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintln(cmd.OutOrStdout(), p.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created %s principal %s (%s), roles %s.\n", p.Kind, p.ID, p.DisplayName, strings.Join(p.Roles, ", "))
	return nil
}

func runPrincipalSuspend(cmd *cobra.Command, args []string) error {
	return principalTransition(cmd, args[0], "suspend", "Suspended")
}

func runPrincipalRevoke(cmd *cobra.Command, args []string) error {
	return principalTransition(cmd, args[0], "revoke", "Revoked")
}

func principalTransition(cmd *cobra.Command, id, verb, done string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	path := principalPath(id, "/"+verb)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would %s principal %s on %s\n", verb, strings.TrimSpace(id), client.URL())
		return nil
	}
	var p storage.Principal
	if err := client.Post(cmdContext(cmd), path, nil, &p); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s principal %s (%s).\n", done, p.ID, p.DisplayName)
	return nil
}

func runPrincipalLink(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would mint a sign-in link for principal %s on %s\n", strings.TrimSpace(args[0]), client.URL())
		return nil
	}
	var link linkReply
	if err := client.Post(cmdContext(cmd), principalPath(args[0], "/links"), nil, &link); err != nil {
		return err
	}
	return printLink(cmd, link, "Single-use sign-in link for "+strings.TrimSpace(args[0]))
}

// printLink prints a minted link: the URL alone on stdout, the note on
// stderr, so the link can be captured.
func printLink(cmd *cobra.Command, link linkReply, what string) error {
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), link)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s, valid until %s:\n", what, link.ExpiresAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(cmd.OutOrStdout(), link.URL)
	return nil
}

func runPrincipalIdentityAdd(cmd *cobra.Command, args []string) error {
	method, _ := cmd.Flags().GetString("method")
	issuer, _ := cmd.Flags().GetString("issuer")
	subject, _ := cmd.Flags().GetString("subject")
	name, _ := cmd.Flags().GetString("name")
	if !contains(manualIdentityMethods, method) {
		return output.UsageError(fmt.Sprintf("--method %q: want one of %s", method, strings.Join(manualIdentityMethods, ", ")))
	}
	if strings.TrimSpace(subject) == "" {
		return output.UsageError("--subject is required")
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would bind %s identity %q to principal %s on %s\n", method, subject, strings.TrimSpace(args[0]), client.URL())
		return nil
	}
	var idn storage.Identity
	if err := client.Post(cmdContext(cmd), principalPath(args[0], "/identities"),
		map[string]any{"method": method, "issuer": issuer, "subject": subject, "name": name}, &idn); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), idn)
	}
	fmt.Fprintln(cmd.OutOrStdout(), idn.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Bound %s identity %s to principal %s.\n", idn.Method, idn.ID, idn.PrincipalID)
	return nil
}

func runPrincipalIdentityList(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[storage.Identity]
	if err := client.Get(cmdContext(cmd), principalPath(args[0], "/identities"), nil, &reply); err != nil {
		return err
	}
	return renderIdentities(cmd, cmd.OutOrStdout(), reply.Data)
}

func runPrincipalIdentityRemove(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	idn := strings.TrimSpace(args[1])
	path := principalPath(args[0], "/identities/"+url.PathEscape(idn))
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would remove identity %s from principal %s on %s\n", idn, strings.TrimSpace(args[0]), client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"id": idn, "removed": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed identity %s.\n", idn)
	return nil
}
