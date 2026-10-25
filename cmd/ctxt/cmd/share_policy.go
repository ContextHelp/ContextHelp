package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt share policy: the share policies of the instance (ADR-080 §2),
// YAML in, versioned by the instance. ctxt sends the document as
// written and dpkms validates it; a refused document names the field.

var sharePolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Manage share policies: what an audience may see",
	Long: `Manage share policies. A policy is a named YAML document: the data filter
objects must match, the classes it excludes, the fields it renders per
type and the capabilities it opens. The instance validates it and keeps
a version that every edit bumps. A grant gives a policy to an audience.

Examples:
  ctxt share policy create colleagues.yaml
  ctxt share policy show colleagues-network
  ctxt share policy edit colleagues-network colleagues.yaml`,
}

var sharePolicyCreateCmd = &cobra.Command{
	Use:   "create [file]",
	Short: "Create a share policy from a YAML document",
	Long: `Create a share policy from a YAML document read from file, or from stdin
when file is - or absent. The document's name names the policy; it
must be unique on the instance. A document that does not validate is
refused with the failing field named. Start from a template with
'ctxt share template install' and edit from there.

Examples:
  ctxt share policy create colleagues.yaml
  cat colleagues.yaml | ctxt share policy create`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSharePolicyCreate,
}

var sharePolicyEditCmd = &cobra.Command{
	Use:   "edit <policy> [file]",
	Short: "Replace a share policy with a new version",
	Long: `Replace a share policy's document with the YAML read from file, or from
stdin when file is - or absent, and bump its version. The document's
name must be the policy's: an edit never renames. Every viewer of the
policy sees the new version on its next request.

Examples:
  ctxt share policy edit colleagues-network colleagues.yaml
  ctxt share policy show colleagues-network | sed 's/city/country/' | ctxt share policy edit colleagues-network`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSharePolicyEdit,
}

var sharePolicyShowCmd = &cobra.Command{
	Use:   "show <policy>",
	Short: "Show a share policy as YAML",
	Long: `Show one share policy, by name or id, as the YAML document it was
created from, with its id, version and last update in a leading
comment. The output is a valid document for 'ctxt share policy edit'.

Examples:
  ctxt share policy show colleagues-network
  ctxt share policy show colleagues-network --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runSharePolicyShow,
}

var sharePolicyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List share policies",
	Long: `List every share policy, by name, with its version and last update.

Examples:
  ctxt share policy list`,
	Args: cobra.NoArgs,
	RunE: runSharePolicyList,
}

func init() {
	shareCmd.AddCommand(sharePolicyCmd)
	sharePolicyCmd.AddCommand(sharePolicyCreateCmd, sharePolicyEditCmd, sharePolicyShowCmd, sharePolicyListCmd)

	cliconv.WithSideEffect(sharePolicyCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(sharePolicyCreateCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(sharePolicyCreateCmd, []cliconv.Example{
		{Title: "From a file", Command: "ctxt share policy create colleagues.yaml"},
		{Title: "From stdin", Command: "cat colleagues.yaml | ctxt share policy create"},
	})
	cliconv.WithNextSteps(sharePolicyCreateCmd, []cliconv.NextStep{
		{When: "to give it to an audience", Suggest: "ctxt share grant <audience> <policy>", Reason: "a policy applies through a grant"},
		{When: "to read it back", Suggest: "ctxt share policy show <policy>", Reason: "the stored document, with its version"},
	})

	// Sending the same document twice bumps the version twice; the
	// view is the same.
	cliconv.WithSideEffect(sharePolicyEditCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(sharePolicyEditCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(sharePolicyEditCmd, []cliconv.Example{
		{Title: "Replace the document", Command: "ctxt share policy edit colleagues-network colleagues.yaml"},
	})
	cliconv.WithNextSteps(sharePolicyEditCmd, []cliconv.NextStep{
		{When: "to confirm the version", Suggest: "ctxt share policy list", Reason: "every edit bumps it"},
	})

	cliconv.WithSideEffect(sharePolicyShowCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(sharePolicyShowCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(sharePolicyShowCmd, []cliconv.Example{
		{Title: "One policy as YAML", Command: "ctxt share policy show colleagues-network"},
	})
	cliconv.WithSideEffect(sharePolicyListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(sharePolicyListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(sharePolicyListCmd, []cliconv.Example{
		{Title: "Every policy", Command: "ctxt share policy list"},
	})
}

// readPolicyFile reads the policy document from path, or from stdin
// when path is "" or "-".
func readPolicyFile(cmd *cobra.Command, path string) (string, error) {
	var (
		data []byte
		err  error
	)
	if path == "" || path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path) // #nosec G304 -- the operator names the policy file
	}
	if err != nil {
		return "", output.UsageError(fmt.Sprintf("read policy document: %v", err))
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", output.UsageError("the policy document is empty; give a file or pipe YAML on stdin")
	}
	return string(data), nil
}

func runSharePolicyCreate(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) == 1 {
		path = args[0]
	}
	doc, err := readPolicyFile(cmd, path)
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would create a share policy from a %d-byte document on %s\n", len(doc), client.URL())
		return nil
	}
	var p storage.SharePolicy
	if err := client.Post(cmdContext(cmd), sharePath("policies"), map[string]any{"document": doc}, &p); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintln(cmd.OutOrStdout(), p.ID)
	fmt.Fprintf(cmd.ErrOrStderr(), "Created policy %s (%s) at version %d.\n", p.Name, p.ID, p.Version)
	return nil
}

func runSharePolicyEdit(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) == 2 {
		path = args[1]
	}
	doc, err := readPolicyFile(cmd, path)
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	policy := strings.TrimSpace(args[0])
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would replace share policy %s with a %d-byte document on %s\n", policy, len(doc), client.URL())
		return nil
	}
	var p storage.SharePolicy
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "PUT", Path: sharePath("policies", policy), Body: map[string]any{"document": doc}}, &p); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), p)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Policy %s is now at version %d.\n", p.Name, p.Version)
	return nil
}

func runSharePolicyShow(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var row storage.SharePolicy
	if err := client.Get(cmdContext(cmd), sharePath("policies", args[0]), nil, &row); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), row)
	}
	p, err := sharing.PolicyFromSpec(&row)
	if err != nil {
		return output.WrapError(err, output.CodeGeneric, output.ExitGeneric)
	}
	doc, err := yaml.Marshal(p)
	if err != nil {
		return output.WrapError(fmt.Errorf("render policy %s: %w", row.Name, err), output.CodeGeneric, output.ExitGeneric)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "# %s, version %d, updated %s\n%s", row.ID, row.Version, fmtStamp(row.UpdatedAt), doc)
	return nil
}

// policyRow is the row shape the tabular formats project from.
type policyRow struct {
	ID      string `table:"ID"`
	Name    string `table:"Name"`
	Version int    `table:"Version"`
	Updated string `table:"Updated"`
}

func renderPolicies(cmd *cobra.Command, w io.Writer, list []storage.SharePolicy) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"policies": list, "total": len(list)})
	}
	rows := make([]policyRow, 0, len(list))
	for _, p := range list {
		rows = append(rows, policyRow{ID: p.ID, Name: p.Name, Version: p.Version, Updated: fmtStamp(p.UpdatedAt)})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Name, fmt.Sprint(r.Version), r.Updated})
	}
	printTable(w, []string{"ID", "NAME", "VERSION", "UPDATED"}, table)
	return nil
}

func runSharePolicyList(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[storage.SharePolicy]
	if err := client.Get(cmdContext(cmd), sharePath("policies"), nil, &reply); err != nil {
		return err
	}
	return renderPolicies(cmd, cmd.OutOrStdout(), reply.Data)
}
