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
		jobsCmd.AddCommand(c)
	}
}
