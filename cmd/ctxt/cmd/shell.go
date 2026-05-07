package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/repl"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive REPL session (eliminates per-command startup overhead)",
	Long: `ctxt shell starts an interactive REPL that holds a single open connection
to your knowledge store for the entire session.

This eliminates the ~50ms SQLite initialisation cost incurred by each
standalone ctxt invocation, making interactive exploration significantly snappier.

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
}

func runShell(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return fmt.Errorf("shell: init service: %w", err)
	}
	defer cleanup()

	sessionSvc = svc
	defer func() { sessionSvc = nil }()

	repl.SetCobraExecHook(func(ctx context.Context, cobraArgs []string) error {
		oldArgs := os.Args
		os.Args = append([]string{"ctxt"}, cobraArgs...)
		defer func() { os.Args = oldArgs }()
		return rootCmd.ExecuteContext(ctx)
	})

	session := repl.NewSession(svc, cfg)
	return session.Run(cmd.Context())
}
