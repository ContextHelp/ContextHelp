package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var entitlementCmd = &cobra.Command{
	Use:   "entitlement",
	Short: "Grant and revoke principals' namespace entitlements",
	Long: `Grant and revoke the namespace entitlements of principals that call this
instance (protected or public instances).

A principal with no grant is unrestricted. A grant opts the principal
into namespace gating: it may then read only entities whose namespace
matches one of its globs ("ai.*", "devops.k8s", "*"). A grant with no
namespaces left, or one past its expiry, denies every gated read. The
instance owner (admin role) bypasses the gate.

These commands manage inbound grants only. Entitlements this instance
holds on remote registries are listed by 'ctxt registry entitlements'.
They read and write the database directly; a running dpkms applies a
change on the principal's next request.

Examples:
  # Gate a principal to two namespaces
  dpkms entitlement grant phone ai.* devops.k8s

  # Grant until a date
  dpkms entitlement grant partner finance.* --expires 2026-12-31

  # Take one namespace away
  dpkms entitlement revoke phone devops.k8s

  # Remove the grant: the principal is unrestricted again
  dpkms entitlement ungate phone --confirm=yes

  # Every grant, as JSON
  dpkms entitlement list --format json`,
}

var entitlementListCmd = &cobra.Command{
	Use:   "list",
	Short: "List principals' namespace grants",
	Long:  `List every inbound grant: principal, namespaces, expiry and last change.`,
	Args:  cobra.NoArgs,
	RunE:  runEntitlementList,
}

var entitlementGrantCmd = &cobra.Command{
	Use:   "grant <principal> <namespace>...",
	Short: "Grant namespaces to a principal",
	Long: `Add namespace globs to a principal's grant, creating the grant when the
principal has none. Creating a grant gates the principal: from then on
it reads only the namespaces granted to it. Namespaces already granted
are left as they are.

--expires sets when the grant ends (a duration such as 720h, a date
such as 2026-12-31 meaning the end of that UTC day, or an RFC 3339
time); --expires never removes the expiry. Without --expires a new
grant never expires and an existing grant keeps its expiry. An expired
grant denies every gated read.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runEntitlementGrant,
}

var entitlementRevokeCmd = &cobra.Command{
	Use:   "revoke <principal> <namespace>...",
	Short: "Revoke namespaces from a principal",
	Long: `Remove namespace globs from a principal's grant. Each glob is matched
exactly as granted ("ai.*" removes "ai.*", not "ai.models").

Revoking the last namespace keeps the grant with no namespaces: the
principal is then denied every gated read. To lift gating altogether
use 'dpkms entitlement ungate'. Revoking a namespace that is not
granted, or from a principal with no grant, changes nothing.`,
	Args: cobra.MinimumNArgs(2),
	RunE: runEntitlementRevoke,
}

var entitlementUngateCmd = &cobra.Command{
	Use:   "ungate <principal>",
	Short: "Delete a principal's grant, returning it to unrestricted access",
	Long: `Delete a principal's grant. This RETURNS THE PRINCIPAL TO UNRESTRICTED
ACCESS: with no grant, the principal may read every namespace again.
To deny a principal everything instead, revoke its namespaces and keep
the empty grant.

The deleted namespaces are printed so the grant can be recreated with
'dpkms entitlement grant'. Ungating a principal with no grant changes
nothing.`,
	Args: cobra.ExactArgs(1),
	RunE: runEntitlementUngate,
}

func init() {
	rootCmd.AddCommand(entitlementCmd)
	entitlementCmd.AddCommand(entitlementListCmd)
	entitlementCmd.AddCommand(entitlementGrantCmd)
	entitlementCmd.AddCommand(entitlementRevokeCmd)
	entitlementCmd.AddCommand(entitlementUngateCmd)

	entitlementGrantCmd.Flags().String("expires", "",
		"grant end as a duration (720h), a date (2026-12-31) or an RFC 3339 time; \"never\" removes it")

	cliconv.WithSideEffect(entitlementListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(entitlementListCmd, cliconv.IdempotencyYes)
	// Granting a namespace already held, or revoking one not held,
	// changes nothing: both are idempotent writes with an inverse.
	cliconv.WithSideEffect(entitlementGrantCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(entitlementGrantCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(entitlementRevokeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(entitlementRevokeCmd, cliconv.IdempotencyYes)
	// Ungate deletes the grant and widens the principal's access to
	// everything; the prior grant is gone unless the operator re-creates
	// it, so it is destructive and behind the confirmation gate.
	cliconv.WithSideEffect(entitlementUngateCmd, cliconv.SideEffectDestructive)
	cliconv.WithIdempotency(entitlementUngateCmd, cliconv.IdempotencyYes)
}

// entitlementRow is the row shape the tabular formats project from.
type entitlementRow struct {
	Principal  string `table:"Principal"`
	Namespaces string `table:"Namespaces"`
	Expires    string `table:"Expires"`
	Updated    string `table:"Updated"`
}

// openGrants opens the configured database and returns the grant
// manager over it. The caller closes the returned driver.
func openGrants(cmd *cobra.Command) (context.Context, *registry.Grants, func(), error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	driver, cleanup, err := openStore(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	return ctx, registry.NewGrants(driver.Entitlements()), cleanup, nil
}

func formatNamespaces(ns []string) string {
	if len(ns) == 0 {
		return "(none: every gated read denied)"
	}
	return strings.Join(ns, ", ")
}

func formatGrantExpiry(e *storage.RegistryEntitlement, now time.Time) string {
	switch {
	case e.ExpiresAt.IsZero():
		return "never"
	case now.After(e.ExpiresAt):
		return e.ExpiresAt.Format(time.RFC3339) + " (expired)"
	default:
		return e.ExpiresAt.Format(time.RFC3339)
	}
}

func runEntitlementList(cmd *cobra.Command, _ []string) error {
	ctx, grants, closeFn, err := openGrants(cmd)
	if err != nil {
		return err
	}
	defer closeFn()

	list, err := grants.List(ctx)
	if err != nil {
		return fmt.Errorf("list grants: %w", err)
	}
	w := cmd.OutOrStdout()
	if isJSONOutput() {
		if list == nil {
			list = []*storage.RegistryEntitlement{}
		}
		return outputJSON(w, map[string]any{"entitlements": list, "total": len(list)})
	}
	now := time.Now().UTC()
	rows := make([]entitlementRow, 0, len(list))
	for _, e := range list {
		rows = append(rows, entitlementRow{
			Principal:  e.RegistryName,
			Namespaces: formatNamespaces(e.Namespaces),
			Expires:    formatGrantExpiry(e, now),
			Updated:    e.FetchedAt.Format(time.RFC3339),
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No grants: every principal is unrestricted.")
		return nil
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.Principal, r.Namespaces, r.Expires, r.Updated})
	}
	printAdminTable(w, []string{"Principal", "Namespaces", "Expires", "Updated"}, table)
	return nil
}

// parseGrantExpiry reads --expires: unset keeps the current expiry,
// "never" clears it, anything else parses as an API key expiry does.
func parseGrantExpiry(cmd *cobra.Command, now time.Time) (registry.GrantOptions, error) {
	if !cmd.Flags().Changed("expires") {
		return registry.GrantOptions{}, nil
	}
	raw, _ := cmd.Flags().GetString("expires")
	if strings.EqualFold(strings.TrimSpace(raw), "never") {
		return registry.GrantOptions{SetExpiry: true}, nil
	}
	if strings.TrimSpace(raw) == "" {
		return registry.GrantOptions{}, fmt.Errorf("--expires: give a duration, a date, an RFC 3339 time or \"never\"")
	}
	at, err := parseKeyExpiry(raw, now)
	if err != nil {
		return registry.GrantOptions{}, err
	}
	return registry.GrantOptions{SetExpiry: true, ExpiresAt: at}, nil
}

func runEntitlementGrant(cmd *cobra.Command, args []string) error {
	opts, err := parseGrantExpiry(cmd, time.Now().UTC())
	if err != nil {
		return err
	}
	ctx, grants, closeFn, err := openGrants(cmd)
	if err != nil {
		return err
	}
	defer closeFn()

	dryRun := kitcli.IsDryRun(cmd)
	res, err := grants.Grant(ctx, args[0], args[1:], opts, dryRun)
	if err != nil {
		return fmt.Errorf("grant: %w", err)
	}
	return renderGrantResult(cmd.OutOrStdout(), actionGrant, res, dryRun)
}

func runEntitlementRevoke(cmd *cobra.Command, args []string) error {
	ctx, grants, closeFn, err := openGrants(cmd)
	if err != nil {
		return err
	}
	defer closeFn()

	dryRun := kitcli.IsDryRun(cmd)
	res, err := grants.Revoke(ctx, args[0], args[1:], dryRun)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	return renderGrantResult(cmd.OutOrStdout(), actionRevoke, res, dryRun)
}

func runEntitlementUngate(cmd *cobra.Command, args []string) error {
	ctx, grants, closeFn, err := openGrants(cmd)
	if err != nil {
		return err
	}
	defer closeFn()

	dryRun := kitcli.IsDryRun(cmd)
	res, err := grants.Ungate(ctx, args[0], dryRun)
	if err != nil {
		return fmt.Errorf("ungate: %w", err)
	}
	return renderGrantResult(cmd.OutOrStdout(), actionUngate, res, dryRun)
}

// Actions renderGrantResult reports, as the JSON "action" field carries them.
const (
	actionGrant  = "grant"
	actionRevoke = "revoke"
	actionUngate = "ungate"
)

// renderGrantResult reports a grant, revoke or ungate: JSON carries the
// whole result; text says what changed, or that nothing did.
func renderGrantResult(w io.Writer, action string, res *registry.GrantResult, dryRun bool) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{
			"action": action, "dry_run": dryRun, "no_op": !res.Changed, "result": res,
		})
	}
	if dryRun {
		fmt.Fprintln(w, "[dry-run] no writes will occur")
	}
	p := res.Principal
	if !res.Changed {
		switch {
		case action == actionUngate:
			fmt.Fprintf(w, "No change: %s has no grant (already unrestricted)\n", p)
		case action == actionRevoke && res.Entitlement == nil:
			fmt.Fprintf(w, "No change: %s has no grant\n", p)
		case action == actionRevoke:
			fmt.Fprintf(w, "No change: %s is not granted %s\n", p, strings.Join(res.Unchanged, ", "))
		default:
			fmt.Fprintf(w, "No change: %s already holds %s\n", p, strings.Join(res.Unchanged, ", "))
		}
		return nil
	}

	switch action {
	case actionGrant:
		if len(res.Added) > 0 {
			fmt.Fprintf(w, "%s %s to %s\n", pick(dryRun, "Would grant", "Granted"), strings.Join(res.Added, ", "), p)
		}
		if len(res.Unchanged) > 0 {
			fmt.Fprintf(w, "Already granted: %s\n", strings.Join(res.Unchanged, ", "))
		}
		if res.Created {
			fmt.Fprintf(w, "%s is now gated: it reads only its granted namespaces\n", p)
		}
	case actionRevoke:
		fmt.Fprintf(w, "%s %s from %s\n", pick(dryRun, "Would revoke", "Revoked"), strings.Join(res.Removed, ", "), p)
		if len(res.Unchanged) > 0 {
			fmt.Fprintf(w, "Not granted: %s\n", strings.Join(res.Unchanged, ", "))
		}
	case actionUngate:
		fmt.Fprintf(w, "%s the grant of %s (namespaces: %s)\n", pick(dryRun, "Would delete", "Deleted"), p, formatNamespaces(res.Removed))
		fmt.Fprintf(w, "%s %s unrestricted: it may read every namespace\n", p, pick(dryRun, "would be", "is now"))
		return nil
	}
	if e := res.Entitlement; e != nil {
		fmt.Fprintf(w, "Namespaces: %s\n", formatNamespaces(e.Namespaces))
		fmt.Fprintf(w, "Expires:    %s\n", formatGrantExpiry(e, time.Now().UTC()))
	}
	return nil
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
