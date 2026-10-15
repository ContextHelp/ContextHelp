package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt share preview | audit (ADR-080 §2 owner bypass, §9 read audit):
// the owner runs a read command through another viewer's view, and
// asks who read what.

var sharePreviewCmd = &cobra.Command{
	Use:   "preview --as <principal|policy> <command> [args...]",
	Short: "Run a read command as another viewer",
	Long: `Run a read command (find, list, show, ...) as another viewer: the
principal id or the share policy name given to --as. The instance
answers exactly what that viewer would see, through the same
projection, the same checks and the same audit, which records the
request as yours with the viewer named. Only the owner may preview,
and only commands that read; the flags after the command belong to it.

Examples:
  ctxt share preview --as prn_01J... find "sarah"
  ctxt share preview --as colleagues-network show <object-id>
  ctxt share preview --as partners-network list --type contact`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSharePreview,
}

var shareAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "List who read what through sharing",
	Long: `List the read audit: one entry per request a shared viewer made, newest
first, with who made it, the route, the policy versions in force and
the objects the answer carried. --principal asks what one viewer read,
--object asks who read one object; at least one is required. --since
narrows the window (a date, an RFC 3339 time, or a duration such as 24h
or 7d). Your own requests are not logged, except previews, which are
logged as yours with the viewer named. Object ids are kept for
sharing.audit.retention (90 days by default); the entries and their
counts stay.

Examples:
  ctxt share audit --object <object-id>
  ctxt share audit --principal prn_01J... --since 7d`,
	Args: cobra.NoArgs,
	RunE: runShareAudit,
}

func init() {
	shareCmd.AddCommand(sharePreviewCmd)
	shareCmd.AddCommand(shareAuditCmd)

	// The flags after the previewed command are the command's own.
	sharePreviewCmd.Flags().SetInterspersed(false)
	sharePreviewCmd.Flags().String("server", "", serverFlagUsage)
	cliconv.WithSideEffect(sharePreviewCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(sharePreviewCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(sharePreviewCmd, []cliconv.Example{
		{Title: "What a policy shows of one contact", Command: "ctxt share preview --as colleagues-network show <object-id>"},
		{Title: "What a person finds", Command: "ctxt share preview --as prn_01J... find \"sarah\""},
	})
	cliconv.WithNextSteps(sharePreviewCmd, []cliconv.NextStep{
		{When: "a field shows that should not", Suggest: "ctxt share policy edit <policy>", Reason: "narrow the fields the policy renders"},
		{When: "to see what they saw for real", Suggest: "ctxt share audit --principal <id>", Reason: "the read audit lists their requests"},
	})

	shareAuditCmd.Flags().String("server", "", serverFlagUsage)
	shareAuditCmd.Flags().String("principal", "", "what this principal (by id) read")
	shareAuditCmd.Flags().String("object", "", "who read this object (by id)")
	shareAuditCmd.Flags().String("since", "", "entries since a date (YYYY-MM-DD), an RFC 3339 time or a duration ago (24h, 7d)")
	shareAuditCmd.Flags().Int("limit", 100, "at most this many entries, newest first")
	cliconv.WithSideEffect(shareAuditCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(shareAuditCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareAuditCmd, []cliconv.Example{
		{Title: "Who saw one contact", Command: "ctxt share audit --object <object-id>"},
		{Title: "What a partner read this week", Command: "ctxt share audit --principal prn_01J... --since 7d"},
	})
	cliconv.WithNextSteps(shareAuditCmd, []cliconv.NextStep{
		{When: "a viewer read more than it should", Suggest: "ctxt share revoke <grant-id>", Reason: "the grant stops on the next request"},
		{When: "to see a viewer's current view", Suggest: "ctxt share preview --as <id> list", Reason: "the preview runs through its view"},
	})
}

func runSharePreview(cmd *cobra.Command, args []string) error {
	as := viewAs(cmd)
	if as == "" {
		return output.UsageError("--as is required: a principal id or a share policy name")
	}
	root := cmd.Root()
	target, rest, err := root.Find(args)
	if err != nil {
		return output.UsageError(err.Error())
	}
	if target == nil || target == root || target == cmd || !target.Runnable() {
		return output.UsageError(fmt.Sprintf("unknown command %q: name a read command such as find, list or show", strings.Join(args, " ")))
	}
	if se, ok := kitcli.GetSideEffect(target); !ok || se != kitcli.SideEffectRead {
		e := output.UsageError(fmt.Sprintf("%s is not a read command; preview runs read commands only", target.CommandPath()))
		e.SuggestedFix = "preview find, list, show or another command that only reads"
		return e
	}
	previewAs = as
	defer func() { previewAs = "" }()
	if err := target.ParseFlags(rest); err != nil {
		return output.UsageError(err.Error())
	}
	positional := target.Flags().Args()
	if err := target.ValidateArgs(positional); err != nil {
		return output.UsageError(err.Error())
	}
	target.SetContext(cmd.Context())
	cliformat.Bind(target)
	switch {
	case target.PreRunE != nil:
		if err := target.PreRunE(target, positional); err != nil {
			return err
		}
	case target.PreRun != nil:
		target.PreRun(target, positional)
	}
	if target.RunE != nil {
		return target.RunE(target, positional)
	}
	target.Run(target, positional)
	return nil
}

// auditReply is the body of GET /share/audit.
type auditReply struct {
	Data  []storage.AccessEntry `json:"data"`
	Total int                   `json:"total"`
}

func runShareAudit(cmd *cobra.Command, _ []string) error {
	principal, _ := cmd.Flags().GetString("principal")
	object, _ := cmd.Flags().GetString("object")
	since, _ := cmd.Flags().GetString("since")
	limit, _ := cmd.Flags().GetInt("limit")
	principal, object, since = strings.TrimSpace(principal), strings.TrimSpace(object), strings.TrimSpace(since)
	if principal == "" && object == "" {
		e := output.UsageError("say whose reads or which object: --principal <id> or --object <id>")
		e.SuggestedFix = "ctxt share audit --object <object-id>, or --principal <principal-id>"
		return e
	}
	if limit <= 0 {
		return output.UsageError("--limit must be positive")
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if principal != "" {
		q.Set("principal", principal)
	}
	if object != "" {
		q.Set("object", object)
	}
	if since != "" {
		q.Set("since", since)
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would list the read audit (%s) on %s\n", q.Encode(), client.URL())
		return nil
	}
	var reply auditReply
	if err := client.Get(cmdContext(cmd), "/api/v1/share/audit", q, &reply); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"entries": reply.Data, "total": reply.Total})
	}
	return renderAudit(cmd.OutOrStdout(), reply.Data)
}

// renderAudit prints the entries as a table, newest first.
func renderAudit(w io.Writer, entries []storage.AccessEntry) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No reads recorded.")
		return err
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			e.At.UTC().Format(time.RFC3339),
			e.PrincipalID,
			e.Route,
			strconv.Itoa(e.ObjectCount),
			strconv.Itoa(e.EntityCount),
			auditIDs(e.ObjectIDs),
			e.PolicyVersions,
		})
	}
	printTable(w, []string{"TIME", "PRINCIPAL", "ROUTE", "OBJECTS", "ENTITIES", "IDS", "POLICIES"}, rows)
	_, err := fmt.Fprintf(w, "Total: %d\n", len(entries))
	return err
}

// auditIDs shows up to three object ids, then how many more.
func auditIDs(ids []string) string {
	const shown = 3
	if len(ids) <= shown {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:shown], ", ") + fmt.Sprintf(" +%d", len(ids)-shown)
}
