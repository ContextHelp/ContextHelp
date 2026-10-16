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
  ctxt share preview --as partners-network list`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSharePreview,
}

var shareAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "List who read what through sharing",
	Long: `List the read audit: one entry per request a shared viewer made, answered or refused, newest first. Each entry is a receipt: who made the request, the route, the decision (allowed, denied, or deferred), the checks behind it in the order they ran, the policy versions in force, and the objects the answer carried.

--principal asks what one viewer read.
--object asks who read one object.
--decision denied asks who was refused.
At least one is required.

--checks prints every check of each entry with its outcome and reason code. --since narrows the window: a date, an RFC 3339 time, or a duration like 24h or 7d.

Your own requests are not logged, except previews, which are logged as yours with the viewer named. Object IDs are kept for sharing.audit.retention (90 days by default); the entries and their counts stay.

Examples:
  ctxt share audit --object <object-id>
  ctxt share audit --principal prn_01J... --since 7d
  ctxt share audit --decision denied --checks`,
	Args: cobra.NoArgs,
	RunE: runShareAudit,
}

func init() {
	shareCmd.AddCommand(sharePreviewCmd)
	shareCmd.AddCommand(shareAuditCmd)

	// The flags after the previewed command are the command's own.
	sharePreviewCmd.Flags().SetInterspersed(false)
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

	shareAuditCmd.Flags().String("principal", "", "what this principal (by id) read")
	shareAuditCmd.Flags().String("object", "", "who read this object (by id)")
	shareAuditCmd.Flags().String("decision", "", "only entries with this decision: allowed, denied or deferred")
	shareAuditCmd.Flags().Bool("checks", false, "print every check of each entry, in the order it ran")
	shareAuditCmd.Flags().String("since", "", "entries since a date (YYYY-MM-DD), an RFC 3339 time or a duration ago (24h, 7d)")
	shareAuditCmd.Flags().Int("limit", 100, "at most this many entries, newest first")
	cliconv.WithSideEffect(shareAuditCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(shareAuditCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(shareAuditCmd, []cliconv.Example{
		{Title: "Who saw one contact", Command: "ctxt share audit --object <object-id>"},
		{Title: "What a partner read this week", Command: "ctxt share audit --principal prn_01J... --since 7d"},
		{Title: "Who was refused, and by which check", Command: "ctxt share audit --decision denied --checks"},
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
	decision, _ := cmd.Flags().GetString("decision")
	since, _ := cmd.Flags().GetString("since")
	limit, _ := cmd.Flags().GetInt("limit")
	checks, _ := cmd.Flags().GetBool("checks")
	principal, object, since = strings.TrimSpace(principal), strings.TrimSpace(object), strings.TrimSpace(since)
	decision = strings.TrimSpace(decision)
	if principal == "" && object == "" && decision == "" {
		e := output.UsageError("say whose requests, which object or which decision: --principal <id>, --object <id> or --decision denied")
		e.SuggestedFix = "ctxt share audit --decision denied, or --object <object-id>, or --principal <principal-id>"
		return e
	}
	if decision != "" && !storage.AccessDecision(decision).Valid() {
		return output.UsageError("--decision must be allowed, denied or deferred")
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
	if decision != "" {
		q.Set("decision", decision)
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
	if checks {
		return renderAuditChecks(cmd.OutOrStdout(), reply.Data)
	}
	return renderAudit(cmd.OutOrStdout(), reply.Data)
}

// renderAudit prints the entries as a table, newest first. WHY names the
// first check that did not pass and its reason code, for an entry that
// was refused, deferred or had the backstop strip something.
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
			string(e.Decision),
			auditWhy(e),
			strconv.Itoa(e.ObjectCount),
			strconv.Itoa(e.EntityCount),
			auditIDs(e.ObjectIDs),
			e.PolicyVersions,
		})
	}
	printTable(w, []string{"TIME", "PRINCIPAL", "ROUTE", "DECISION", "WHY", "OBJECTS", "ENTITIES", "IDS", "POLICIES"}, rows)
	_, err := fmt.Fprintf(w, "Total: %d\n", len(entries))
	return err
}

// auditWhy is "check: reason" of the first check of e that failed or
// could not decide; "-" when every check passed or had nothing to
// decide.
func auditWhy(e storage.AccessEntry) string {
	for _, c := range e.Checks {
		if c.Outcome == storage.AccessOutcomeFail || c.Outcome == storage.AccessOutcomeUnknown {
			return c.Check + ": " + c.Reason
		}
	}
	return "-"
}

// renderAuditChecks prints each entry with its checks in the order they
// ran: the receipt behind the decision.
func renderAuditChecks(w io.Writer, entries []storage.AccessEntry) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "No reads recorded.")
		return err
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(w, "%s  %s  %s  %s\n", e.At.UTC().Format(time.RFC3339), e.PrincipalID, e.Route, e.Decision); err != nil {
			return err
		}
		for _, c := range e.Checks {
			if _, err := fmt.Fprintf(w, "  %-22s %-15s %s\n", c.Check, c.Outcome, c.Reason); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  digest %s\n", e.OperationDigest); err != nil {
			return err
		}
	}
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
