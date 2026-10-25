package cmd

import (
	"io"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
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

		a, closeRead, err := tuiAdapter(cmd)
		if err != nil {
			return err
		}
		defer closeRead()
		return tui.Run(a, tuiStartOpts(cmd, tui.StartOpts{}))
	},
}

// tuiAdapter opens the TUI's backend: one read-only service for the
// session (no lock, built once, so the 2 s job poll costs no rebuild),
// with any warning printed before the TUI takes the screen; and a write
// opener that opens the locked path per capture or job action. A write
// open's warnings repeat the session open's (same database) and are not
// printed under the TUI. Every operation runs as the owner.
func tuiAdapter(cmd *cobra.Command) (*tui.RealAdapter, func(), error) {
	read, closeRead, err := openService(cmd, true, os.Stderr)
	if err != nil {
		return nil, nil, err
	}
	a := tui.NewRealAdapter(read, func() (*service.Service, func(), error) {
		return openService(cmd, false, io.Discard)
	}).WithContext(asOwner)
	return a, closeRead, nil
}

// tuiStartOpts returns opts with Profile set to cmd's active profile, so
// the TUI runs under the same --profile / profile.default resolution as
// every other command.
func tuiStartOpts(cmd *cobra.Command, opts tui.StartOpts) tui.StartOpts {
	opts.Profile = activeProfileName(cmd)
	return opts
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
