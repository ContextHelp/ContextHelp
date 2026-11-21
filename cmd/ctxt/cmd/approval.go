package cmd

import (
	"encoding/json"
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
)

// ctxt approval: the approval queue of the instance (ADR-083 §9), over
// /api/v1/approvals. Requests are filed by the commands whose changes
// need a second person; these commands list, show and decide them.

var approvalCmd = &cobra.Command{
	Use:   "approval",
	Short: "Review the requests that need a second person",
	Long: `Review and decide approval requests. These are changes that apply only
once someone other than the person who asked accepts them. Each request
freezes the change with a digest, needs a set number of approvals from
eligible approvers, and expires if nobody decides it in time
(approvals.pending_ttl, 72 hours by default). The requester, the subject,
and anyone the change benefits can never decide it, and one rejection
rejects it. You see the requests you filed, the ones about you, and the
ones you may decide; the owner sees them all. Every command calls the
instance with its configured key.

Examples:
  ctxt approval list --state requested
  ctxt approval show apr_01J...
  ctxt approval approve apr_01J... --note "checked with legal"`,
}

var approvalListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List approval requests",
	Long: `List the approval requests you can see, newest first. Use --state for
one state (requested, approved, rejected, withdrawn, stale, or expired),
and --kind for one kind. The DECIDE column marks the ones you may decide
now.

Examples:
  ctxt approval list
  ctxt approval list --state requested`,
	Args: cobra.NoArgs,
	RunE: runApprovalList,
}

var approvalShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one approval request",
	Long: `Show an approval request: what it changes (its frozen payload and
digest), who asked and why, the decisions so far, and whether you may
decide it. A request you may not see is reported as not found.

Examples:
  ctxt approval show apr_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runApprovalShow,
}

var approvalApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a request",
	Long: `Approve a request. Your approval binds to the payload digest. Use
--digest to pin the digest you reviewed with 'ctxt approval show'.
Without it, the command reads the request first and approves the digest
it finds. The last approval a request needs applies the change at once.
If what the request was filed against changed since, the request ends
stale, nothing is applied, and the requester files it again. When the
change produces a credential, this command prints it once; nothing shows
it again.

Examples:
  ctxt approval approve apr_01J...
  ctxt approval approve apr_01J... --digest v1:sha256:... --note "matches the ticket"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runApprovalDecide(cmd, args[0], "approve") },
}

var approvalRejectCmd = &cobra.Command{
	Use:   "reject <id>",
	Short: "Reject a request",
	Long: `Reject a request. One rejection by an eligible approver rejects it,
whatever approvals it already has. The rejection binds to the payload
digest like an approval does, and --note is kept with it.

Examples:
  ctxt approval reject apr_01J... --note "wrong audience"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runApprovalDecide(cmd, args[0], "reject") },
}

var approvalWithdrawCmd = &cobra.Command{
	Use:   "withdraw <id>",
	Short: "Withdraw your own pending request",
	Long: `Withdraw a pending request you filed. The owner may withdraw any
request. A withdrawn request stays on record and cannot be decided.

Examples:
  ctxt approval withdraw apr_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runApprovalWithdraw,
}

func init() {
	rootCmd.AddCommand(approvalCmd)
	approvalCmd.AddCommand(approvalListCmd, approvalShowCmd, approvalApproveCmd, approvalRejectCmd, approvalWithdrawCmd)
	approvalListCmd.Flags().String("kind", "", "keep requests of this kind")
	approvalListCmd.Flags().String("state", "", "keep requests in this state: requested, approved, rejected, withdrawn, stale or expired")
	approvalListCmd.Flags().Int("limit", 0, "show at most this many requests")
	for _, c := range []*cobra.Command{approvalApproveCmd, approvalRejectCmd} {
		c.Flags().String("note", "", "a note kept with your decision")
		c.Flags().String("digest", "", "the payload digest you reviewed; read from the request when unset")
	}

	cliconv.WithSideEffect(approvalListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(approvalListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(approvalListCmd, []cliconv.Example{
		{Title: "What waits for a decision", Command: "ctxt approval list --state requested"},
	})
	cliconv.WithNextSteps(approvalListCmd, []cliconv.NextStep{
		{When: "to review one", Suggest: "ctxt approval show <id>", Reason: "its frozen payload, digest and decisions"},
	})

	cliconv.WithSideEffect(approvalShowCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(approvalShowCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(approvalShowCmd, []cliconv.Example{
		{Title: "Review a request", Command: "ctxt approval show apr_01J..."},
	})
	cliconv.WithNextSteps(approvalShowCmd, []cliconv.NextStep{
		{When: "to accept it", Suggest: "ctxt approval approve <id> --digest <digest>", Reason: "the approval binds to the digest you reviewed"},
		{When: "to refuse it", Suggest: "ctxt approval reject <id>", Reason: "one rejection rejects it"},
	})

	// One decision per approver: a second one is refused.
	for _, c := range []*cobra.Command{approvalApproveCmd, approvalRejectCmd} {
		cliconv.WithSideEffect(c, cliconv.SideEffectWrite)
		cliconv.WithIdempotency(c, cliconv.IdempotencyNo)
	}
	cliconv.WithExamples(approvalApproveCmd, []cliconv.Example{
		{Title: "Approve what you reviewed", Command: "ctxt approval approve apr_01J... --digest v1:sha256:..."},
	})
	cliconv.WithExamples(approvalRejectCmd, []cliconv.Example{
		{Title: "Reject with a note", Command: "ctxt approval reject apr_01J... --note \"wrong audience\""},
	})
	for _, c := range []*cobra.Command{approvalApproveCmd, approvalRejectCmd} {
		cliconv.WithNextSteps(c, []cliconv.NextStep{
			{When: "to see where it stands", Suggest: "ctxt approval show <id>", Reason: "its state and the decisions so far"},
		})
	}

	cliconv.WithSideEffect(approvalWithdrawCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(approvalWithdrawCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(approvalWithdrawCmd, []cliconv.Example{
		{Title: "Withdraw a request", Command: "ctxt approval withdraw apr_01J..."},
	})
	cliconv.WithNextSteps(approvalWithdrawCmd, []cliconv.NextStep{
		{When: "to see your requests", Suggest: "ctxt approval list", Reason: "a withdrawn request stays on record"},
	})
}

// approvalDecision is one decision on a request.
type approvalDecision struct {
	ApproverID    string    `json:"approver_id"`
	Decision      string    `json:"decision"`
	PayloadDigest string    `json:"payload_digest"`
	Note          string    `json:"note"`
	DecidedAt     time.Time `json:"decided_at"`
}

// approvalStateRequested is the state of a request still waiting on approvals.
const approvalStateRequested = "requested"

// approvalReply is a request as GET /api/v1/approvals answers it; Result
// is set only on the approval that applied it.
type approvalReply struct {
	ID                string             `json:"id"`
	Kind              string             `json:"kind"`
	Action            string             `json:"action"`
	State             string             `json:"state"`
	RequestedBy       string             `json:"requested_by"`
	SubjectID         string             `json:"subject_id,omitempty"`
	Reason            string             `json:"reason"`
	Payload           json.RawMessage    `json:"payload"`
	PayloadDigest     string             `json:"payload_digest"`
	ApprovalsRequired int                `json:"approvals_required"`
	Approvals         int                `json:"approvals"`
	RequestedAt       time.Time          `json:"requested_at"`
	ExpiresAt         time.Time          `json:"expires_at"`
	DecidedAt         *time.Time         `json:"decided_at,omitempty"`
	ResultRef         string             `json:"result_ref,omitempty"`
	Decisions         []approvalDecision `json:"decisions"`
	CanDecide         bool               `json:"can_decide"`
	Result            json.RawMessage    `json:"result,omitempty"`
}

// approvalPath is /api/v1/approvals, then the escaped segments.
func approvalPath(segments ...string) string {
	path := "/api/v1/approvals"
	for _, s := range segments {
		path += "/" + url.PathEscape(strings.TrimSpace(s))
	}
	return path
}

// approvalRow is the row shape the tabular formats project from.
type approvalRow struct {
	ID          string `table:"ID"`
	Kind        string `table:"Kind"`
	Action      string `table:"Action"`
	State       string `table:"State"`
	Approvals   string `table:"Approvals"`
	RequestedBy string `table:"Requested by"`
	Expires     string `table:"Expires"`
	Decide      string `table:"Decide"`
}

func renderApprovals(cmd *cobra.Command, w io.Writer, list []approvalReply) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"approvals": list, "total": len(list)})
	}
	rows := make([]approvalRow, 0, len(list))
	for _, a := range list {
		decide := ""
		if a.CanDecide {
			decide = confirmYes
		}
		rows = append(rows, approvalRow{
			ID: a.ID, Kind: a.Kind, Action: a.Action, State: a.State,
			Approvals:   fmt.Sprintf("%d/%d", a.Approvals, a.ApprovalsRequired),
			RequestedBy: a.RequestedBy, Expires: fmtStamp(a.ExpiresAt), Decide: decide,
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No approval requests.")
		return nil
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Kind, r.Action, r.State, r.Approvals, r.RequestedBy, r.Expires, r.Decide})
	}
	printTable(w, []string{"ID", "KIND", "ACTION", "STATE", "APPROVALS", "REQUESTED BY", "EXPIRES", "DECIDE"}, table)
	return nil
}

func runApprovalList(cmd *cobra.Command, _ []string) error {
	q := url.Values{}
	for _, name := range []string{"kind", "state"} {
		if v, _ := cmd.Flags().GetString(name); strings.TrimSpace(v) != "" {
			q.Set(name, strings.TrimSpace(v))
		}
	}
	if n, _ := cmd.Flags().GetInt("limit"); n != 0 {
		if n < 0 {
			return output.UsageError("--limit must be positive")
		}
		q.Set("limit", strconv.Itoa(n))
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[approvalReply]
	if err := client.Get(cmdContext(cmd), approvalPath(), q, &reply); err != nil {
		return err
	}
	return renderApprovals(cmd, cmd.OutOrStdout(), reply.Data)
}

func runApprovalShow(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var a approvalReply
	if err := client.Get(cmdContext(cmd), approvalPath(args[0]), nil, &a); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), a)
	}
	printApproval(cmd.OutOrStdout(), a)
	return nil
}

// printApproval writes a request for a terminal.
func printApproval(w io.Writer, a approvalReply) {
	fmt.Fprintf(w, "%s  %s / %s  %s\n", a.ID, a.Kind, a.Action, a.State)
	fmt.Fprintf(w, "Requested by %s on %s, expires %s\n", a.RequestedBy, fmtStamp(a.RequestedAt), fmtStamp(a.ExpiresAt))
	if a.SubjectID != "" {
		fmt.Fprintf(w, "Subject: %s\n", a.SubjectID)
	}
	if a.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", a.Reason)
	}
	fmt.Fprintf(w, "Approvals: %d of %d\n", a.Approvals, a.ApprovalsRequired)
	fmt.Fprintf(w, "Digest: %s\n", a.PayloadDigest)
	fmt.Fprintf(w, "Payload: %s\n", string(a.Payload))
	for _, d := range a.Decisions {
		line := fmt.Sprintf("  %s %s on %s", d.ApproverID, d.Decision, fmtStamp(d.DecidedAt))
		if d.Note != "" {
			line += ": " + d.Note
		}
		fmt.Fprintln(w, line)
	}
	if a.ResultRef != "" {
		fmt.Fprintf(w, "Result: %s\n", a.ResultRef)
	}
	if a.CanDecide {
		fmt.Fprintln(w, "You may decide this request.")
	}
}

// runApprovalDecide approves or rejects request id (verb approve or
// reject), bound to --digest or to the digest the request holds now.
func runApprovalDecide(cmd *cobra.Command, id, verb string) error {
	note, _ := cmd.Flags().GetString("note")
	digest, _ := cmd.Flags().GetString("digest")
	digest = strings.TrimSpace(digest)
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if digest == "" {
		var a approvalReply
		if err := client.Get(cmdContext(cmd), approvalPath(id), nil, &a); err != nil {
			return err
		}
		digest = a.PayloadDigest
	}
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would %s approval request %s at digest %s on %s\n", verb, id, digest, client.URL())
		return nil
	}
	var a approvalReply
	if err := client.Post(cmdContext(cmd), approvalPath(id, verb), map[string]string{"payload_digest": digest, "note": note}, &a); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), a)
	}
	switch {
	case len(a.Result) > 0:
		// The once-only result (a credential): stdout, nothing else.
		var s string
		if json.Unmarshal(a.Result, &s) == nil {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), string(a.Result))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Approved %s; it is applied. The result above is shown once.\n", a.ID)
	case a.State == approvalStateRequested:
		fmt.Fprintln(cmd.OutOrStdout(), a.ID)
		fmt.Fprintf(cmd.ErrOrStderr(), "Recorded; %s has %d of %d approvals.\n", a.ID, a.Approvals, a.ApprovalsRequired)
	default:
		fmt.Fprintln(cmd.OutOrStdout(), a.ID)
		fmt.Fprintf(cmd.ErrOrStderr(), "%s is %s.\n", a.ID, a.State)
	}
	return nil
}

func runApprovalWithdraw(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	path := approvalPath(args[0])
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would withdraw approval request %s through %s on %s\n", args[0], path, client.URL())
		return nil
	}
	var a approvalReply
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, &a); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), a)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Withdrew approval request %s.\n", a.ID)
	return nil
}
