package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
)

// ctxt steward: steward access (ADR-083 §2, §3) over
// /api/v1/steward/grants. A request names a steward, a frozen scope, a
// window and a reason; approvers decide it with ctxt approval; the
// approved request is a grant. Reading under a grant (elevation) is a
// web UI browser session's only: no command elevates.

var stewardCmd = &cobra.Command{
	Use:   "steward",
	Short: "Request, review and revoke steward access",
	Long: `Steward access lets one named person read beyond their own grants for a
set time, for compliance work such as eDiscovery or a legal hold.
Someone files a request naming the steward, what they may read, for how
long and why. Approvers other than the requester and the steward decide
it with 'ctxt approval approve <id> --digest <digest>' (the digest
'ctxt approval show' prints), and an approved request becomes a
steward grant. The steward reads under a grant only from the web UI:
they elevate their browser session into one grant at a time. No command
elevates. Steward access is off unless the instance sets
steward.enabled. Every command calls the instance with its configured
key.

Examples:
  ctxt steward request --steward prn_01J... --scope scope.yaml --expires 7d --reason "..."
  ctxt steward list --state active
  ctxt steward revoke stg_01J... --reason "matter closed"`,
}

var stewardRequestCmd = &cobra.Command{
	Use:   "request",
	Short: "Request steward access for a person",
	Long: `File a request for steward access. Name the steward with --steward
(yourself when left out): a person who holds the steward-requester role.
Give what they may read either as a share policy document with --scope
(a YAML or JSON file, or - for stdin) or as an audience with
--as-audience, which copies what its members can see right now. The
window opens at --not-before (now when left out) and closes at
--expires, each a duration (7d, 72h), a date (2026-12-31) or an RFC 3339
time; it may not be longer than steward.max_duration. --reason is shown
to the approvers and must be long enough (steward.min_reason_chars).
Nothing is granted yet: the command prints the request id, and
approvers decide it with 'ctxt approval approve'. Filing needs the
request:steward scope, and the owner files nothing.

Examples:
  ctxt steward request --steward prn_01J... --scope scope.yaml --expires 7d --reason "Legal hold for matter 2026-117"
  ctxt steward request --as-audience partners --expires 72h --matter CASE-117 --reason "..."`,
	Args: cobra.NoArgs,
	RunE: runStewardRequest,
}

var stewardListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List the steward grants you can see",
	Long: `List the steward grants you can see, newest first: the ones you are the
steward of or requested, or every grant when you hold approve:steward or
are the owner. --state keeps one state: approved (its window has not
opened yet), active, expired or revoked. Pending requests are on the
approval queue: 'ctxt approval list --kind steward.grant'.

Examples:
  ctxt steward list
  ctxt steward list --state active`,
	Args: cobra.NoArgs,
	RunE: runStewardList,
}

var stewardShowCmd = &cobra.Command{
	Use:   "show <grant-id>",
	Short: "Show one steward grant",
	Long: `Show a steward grant: its steward, matter, window and state, the frozen
scope and its digest, and who requested it. A grant you cannot see is
reported as not found.

Examples:
  ctxt steward show stg_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runStewardShow,
}

var stewardRevokeCmd = &cobra.Command{
	Use:   "revoke <grant-id>",
	Short: "Revoke a steward grant",
	Long: `Revoke a steward grant for good. The steward's next request under it is
refused and their elevation ends; nothing brings the grant back. The
steward, the requester, an approver (approve:steward) and the owner may
revoke it. The grant stays on record as revoked, with --reason.

Examples:
  ctxt steward revoke stg_01J... --reason "matter closed"`,
	Args: cobra.ExactArgs(1),
	RunE: runStewardRevoke,
}

func init() {
	rootCmd.AddCommand(stewardCmd)
	stewardCmd.AddCommand(stewardRequestCmd, stewardListCmd, stewardShowCmd, stewardRevokeCmd)
	f := stewardRequestCmd.Flags()
	f.String("steward", "", "the person who will read; you when left out")
	f.String("scope", "", "a share policy document (YAML or JSON) naming what the steward may read; - reads stdin")
	f.String("as-audience", "", "copy what this audience's members can see now, in place of --scope")
	f.String("not-before", "", "when the window opens: a duration from now, a date or an RFC 3339 time; now when left out")
	f.String("expires", "", "when the window closes: a duration (7d, 72h), a date (2026-12-31) or an RFC 3339 time")
	f.String("reason", "", "why the access is needed; the approvers read it")
	f.String("matter", "", "an external reference, such as a case or ticket number")
	f.String("renews", "", "the grant this request renews")
	markFlagsRequired(stewardRequestCmd, "expires", "reason")
	stewardListCmd.Flags().String("state", "", "keep grants in this state: approved, active, expired or revoked")
	stewardListCmd.Flags().String("steward", "", "keep the grants of this steward")
	stewardListCmd.Flags().Int("limit", 0, "show at most this many grants")
	stewardRevokeCmd.Flags().String("reason", "", "why the grant ends, kept with it")

	// Each request is a new one.
	cliconv.WithSideEffect(stewardRequestCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(stewardRequestCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(stewardRequestCmd, []cliconv.Example{
		{Title: "Request a scope for a steward", Command: `ctxt steward request --steward prn_01J... --scope scope.yaml --expires 7d --reason "..."`},
		{Title: "What an audience sees", Command: `ctxt steward request --as-audience partners --expires 72h --reason "..."`},
	})
	cliconv.WithNextSteps(stewardRequestCmd, []cliconv.NextStep{
		{When: "to follow the request", Suggest: "ctxt approval show <id>", Reason: "its frozen scope and the decisions so far"},
	})

	for _, c := range []*cobra.Command{stewardListCmd, stewardShowCmd} {
		cliconv.WithSideEffect(c, cliconv.SideEffectRead)
		cliconv.WithIdempotency(c, cliconv.IdempotencyYes)
	}
	cliconv.WithExamples(stewardListCmd, []cliconv.Example{
		{Title: "Grants in force", Command: "ctxt steward list --state active"},
	})
	cliconv.WithNextSteps(stewardListCmd, []cliconv.NextStep{
		{When: "to read one", Suggest: "ctxt steward show <grant-id>", Reason: "its scope, window and state"},
	})
	cliconv.WithExamples(stewardShowCmd, []cliconv.Example{
		{Title: "Show a grant", Command: "ctxt steward show stg_01J..."},
	})
	cliconv.WithNextSteps(stewardShowCmd, []cliconv.NextStep{
		{When: "to end it now", Suggest: "ctxt steward revoke <grant-id>", Reason: "revocation is effective on the steward's next request"},
	})

	// Revoking a revoked grant changes nothing more.
	cliconv.WithSideEffect(stewardRevokeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(stewardRevokeCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(stewardRevokeCmd, []cliconv.Example{
		{Title: "Revoke a grant", Command: `ctxt steward revoke stg_01J... --reason "matter closed"`},
	})
	cliconv.WithNextSteps(stewardRevokeCmd, []cliconv.NextStep{
		{When: "to check it", Suggest: "ctxt steward show <grant-id>", Reason: "a revoked grant stays on record"},
	})
}
