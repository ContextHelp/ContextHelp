package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive terminal interface",
	Long: `ctxt tui opens a full-screen terminal user interface for browsing knowledge objects,
capturing new content, and composing outputs without leaving the terminal.

Keyboard shortcuts:
  /           focus search
  Tab         next pane
  Shift+Tab   previous pane
  Ctrl+N      capture new content
  ?           toggle help
  q           quit`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// CTXT_TUI_ASCII=1 is reserved for ASCII-only rendering (swapping the
		// Unicode box-drawing characters). Not yet implemented: the variable is
		// deliberately not read here so it has no silent partial effect.

		svc, cleanup, err := newService()
		if err != nil {
			return err
		}
		defer cleanup()

		return tui.Run(tui.NewRealAdapter(svc), cfg)
	},
}

func init() {
	tuiCmd.Flags().String("theme", "default", "color theme (default | high-contrast | solarized)")
	rootCmd.AddCommand(tuiCmd)
	cliconv.WithSideEffect(tuiCmd, cliconv.SideEffectInteractive)
	cliconv.WithExamples(tuiCmd, []cliconv.Example{
		{Title: "Open the terminal UI", Command: "ctxt tui"},
		{Title: "Use a high-contrast theme", Command: "ctxt tui --theme high-contrast"},
	})
	cliconv.WithNextSteps(tuiCmd, []cliconv.NextStep{
		{When: "for a non-TUI workflow", Suggest: "ctxt shell", Reason: "REPL with the same data layer"},
	})
	// "tui" is not in kit's defaultIdempotency table; an interactive
	// full-screen session has no replay semantics.
	cliconv.WithIdempotency(tuiCmd, cliconv.IdempotencyNo)
}
