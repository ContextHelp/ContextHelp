package cmd

import (
	"os"

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
		// Check ASCII-only mode (disables Unicode box-drawing characters).
		if os.Getenv("CTXT_TUI_ASCII") == "1" {
			// Full ASCII-only mode is a Phase 3 polish task.
		}

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
	// "tui" is not in kit's defaultIdempotency table; an interactive
	// full-screen session has no replay semantics.
	cliconv.WithIdempotency(tuiCmd, cliconv.IdempotencyNo)
}
