package cmd

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var jobsCmd = &cobra.Command{
	Use:        "job",
	Short:      "[deprecated] moved to dpkms job",
	Long:       `Deprecated alias for the dpkms job command group. Forwards to 'dpkms job' subcommands.`,
	Deprecated: "use 'dpkms job' instead",
	RunE:       movedToDpkms("job"),
}

// jobIdempotency maps each deprecated job subcommand to its kit/idempotent
// classification. Kit's default verb table does not cover cancel/log/retry/
// status, so they are stamped explicitly here.
var jobIdempotency = map[string]cliconv.Idempotency{
	"list":   cliconv.IdempotencyYes,
	"status": cliconv.IdempotencyYes,
	"log":    cliconv.IdempotencyYes,
	"retry":  cliconv.IdempotencyNo,
	"cancel": cliconv.IdempotencyYes,
}

// jobSideEffect maps each deprecated job subcommand to its kit/side-effect.
var jobSideEffect = map[string]cliconv.SideEffect{
	"list":   cliconv.SideEffectRead,
	"status": cliconv.SideEffectRead,
	"log":    cliconv.SideEffectRead,
	"retry":  cliconv.SideEffectWrite,
	"cancel": cliconv.SideEffectWrite,
}

// jobLong supplies a per-subcommand Long description so the deprecated
// stubs satisfy the 12fcc cmd.Long requirement.
var jobLong = map[string]string{
	"list":   "Deprecated alias for 'dpkms job list'. Forwards invocations to dpkms.",
	"status": "Deprecated alias for 'dpkms job status'. Forwards invocations to dpkms.",
	"log":    "Deprecated alias for 'dpkms job log'. Forwards invocations to dpkms.",
	"retry":  "Deprecated alias for 'dpkms job retry'. Forwards invocations to dpkms.",
	"cancel": "Deprecated alias for 'dpkms job cancel'. Forwards invocations to dpkms.",
}

// jobExamples supplies 12fcc strict-gate Examples per subcommand. The
// surface is deprecated — the examples mirror the dpkms replacement so
// agents can hop straight to the canonical CLI.
var jobExamples = map[string][]cliconv.Example{
	"list": {
		{Title: "List recent jobs", Command: "dpkms job list"},
		{Title: "List only failed jobs", Command: "dpkms job list --status failed"},
	},
	"status": {
		{Title: "Show a job's status", Command: "dpkms job status job_abc123"},
		{Title: "JSON output", Command: "dpkms job status job_abc123 --output json"},
	},
	"log": {
		{Title: "Tail a job's log", Command: "dpkms job log job_abc123"},
		{Title: "Print full log to stdout", Command: "dpkms job log job_abc123 --follow=false"},
	},
	"retry": {
		{Title: "Retry a failed job", Command: "dpkms job retry job_abc123"},
		{Title: "Retry every failed job", Command: "dpkms job retry --all"},
	},
	"cancel": {
		{Title: "Cancel a running job", Command: "dpkms job cancel job_abc123"},
		{Title: "Cancel every pending job", Command: "dpkms job cancel --all"},
	},
}

// jobNextSteps supplies follow-ups for write-class subcommands.
var jobNextSteps = map[string][]cliconv.NextStep{
	"retry": {
		{When: "on success", Suggest: "dpkms job status <job-id>", Reason: "watch the retried job progress"},
	},
	"cancel": {
		{When: "after cancel", Suggest: "dpkms job list --status cancelled", Reason: "confirm the job moved to cancelled"},
	},
}

func init() {
	rootCmd.AddCommand(jobsCmd)

	for _, sub := range []string{
		"list", "status", "log", "retry", "cancel",
	} {
		name := sub
		c := &cobra.Command{
			Use:                name,
			Short:              fmt.Sprintf("[deprecated] moved to dpkms job %s", name),
			Long:               jobLong[name],
			Deprecated:         fmt.Sprintf("use 'dpkms job %s' instead", name),
			RunE:               movedToDpkms("job " + name),
			DisableFlagParsing: true,
		}
		cliconv.WithSideEffect(c, jobSideEffect[name])
		cliconv.WithIdempotency(c, jobIdempotency[name])
		if ex, ok := jobExamples[name]; ok {
			cliconv.WithExamples(c, ex)
		}
		if ns, ok := jobNextSteps[name]; ok {
			cliconv.WithNextSteps(c, ns)
		}
		jobsCmd.AddCommand(c)
	}
}
