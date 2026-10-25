package cmd

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/repl"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive REPL session",
	Long: `ctxt shell starts an interactive REPL over the ctxt command tree.

Each line runs one command, which opens the knowledge store (or reaches
dpkms) for that command alone, so the shell runs beside dpkms serve.

Jobs run in dpkms: the shell prints no job notifications. Follow a job
with ctxt job status <job-id>.

Commands inside the shell are identical to standalone CLI commands:

  ctxt> find authentication flow
  ctxt> list --type url --tagged checkout
  ctxt> open 1
  ctxt> authentication flow | make brief

Special syntax:
  <query> | make <type>   pipe find results directly into a make command
  <rsql>                  any line with == or =in= is treated as list --q
  <free text>             anything else is treated as find <query>

Press Ctrl+D or type exit / quit to end the session.`,
	RunE: runShell,
}

func init() {
	rootCmd.AddCommand(shellCmd)
	cliconv.WithSideEffect(shellCmd, cliconv.SideEffectInteractive)
	cliconv.WithExamples(shellCmd, []cliconv.Example{
		{Title: "Open the interactive REPL", Command: "ctxt shell"},
	})
	cliconv.WithNextSteps(shellCmd, []cliconv.NextStep{
		{When: "inside the REPL", Suggest: "find <query>", Reason: "search the knowledge store"},
		{When: "to leave", Suggest: "exit", Reason: "or press Ctrl+D"},
	})
	// "shell" is not in kit's defaultIdempotency table; an interactive
	// REPL session has no replay semantics.
	cliconv.WithIdempotency(shellCmd, cliconv.IdempotencyNo)
}

func runShell(cmd *cobra.Command, args []string) error {
	return prepareShell().Run(cmd.Context())
}

// prepareShell wires the REPL to the command tree. It opens nothing:
// each line's command opens the store for its own read or write class
// and closes it when done, so the shell runs beside dpkms serve and
// never holds the database lock between lines.
func prepareShell() *repl.Session {
	repl.SetCobraExecHook(shellExec)
	return repl.NewSession(cfg)
}

// shellExec runs one shell line through the command tree, with every
// flag back at its default first.
func shellExec(ctx context.Context, cobraArgs []string) error {
	resetAllFlags(rootCmd)
	rootCmd.SetArgs(cobraArgs)
	return rootCmd.ExecuteContext(ctx)
}
