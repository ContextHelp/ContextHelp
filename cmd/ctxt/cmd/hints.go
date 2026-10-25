package cmd

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
)

// Next-step hints: one line each, printed after a command succeeds, on
// a terminal, in table format. kit owns the registry (root.Hints), the
// gating (--no-hints, --quiet, HOP_QUIET_HINTS, non-TTY,
// json/yaml) and the rendering (output.RenderHints); ctxt only says
// which hints a command has and when to print them.

// hintedAnnotation marks a command whose RunE renders its hints after
// success, so a second installHints leaves it alone.
const hintedAnnotation = "ctxt.cli.runE.hinted"

var registerHintsOnce sync.Once

// hintKey is the name a command's hints are registered under: its path
// without the binary name ("feed add", "import chrome").
func hintKey(c *cobra.Command) string {
	return strings.TrimPrefix(strings.TrimPrefix(c.CommandPath(), c.Root().Name()), " ")
}

// notDryRun is a hint condition: the command did real work.
func notDryRun(c *cobra.Command) func() bool {
	return func() bool {
		dry, _ := c.Flags().GetBool("dry-run")
		return !dry
	}
}

// registerHints fills root.Hints for the commands that have next steps.
// It runs once the tree is built, since the import subcommands are
// enumerated from it.
func registerHints() {
	h := root.Hints
	h.Register("analyze", output.Hint{Message: "Run `ctxt job status <job-id>` to follow the job."})
	h.Register("feed add", output.Hint{Message: "Run `ctxt feed list` to see the feed and its last sync."})
	h.Register("profile create", output.Hint{Message: "Run `ctxt profile set <name>` to make it the active profile."})
	h.Register("watch start", output.Hint{Message: "Run `ctxt watch status` in another shell to check progress; Ctrl-C stops the watcher."})

	for _, c := range importCmd.Commands() {
		if c == importBatchCmd {
			h.Register(hintKey(c), output.Hint{
				Message:   "Run `ctxt import batch status <batch-id>` to follow the batch.",
				Condition: notDryRun(c),
			})
			continue
		}
		if c.RunE == nil {
			continue
		}
		h.Register(hintKey(c), output.Hint{
			Message:   "Run `ctxt job list --state pending` to follow the import jobs.",
			Condition: notDryRun(c),
		})
	}
}

// installHints registers the hints and wraps the RunE of every command
// that has some so they print after a successful run. watch start is
// left out: it runs until stopped, so it prints its hint itself once
// the watcher is up.
func installHints(rootCmd *cobra.Command) {
	registerHintsOnce.Do(registerHints)
	wrapHinted(rootCmd)
}

func wrapHinted(c *cobra.Command) {
	if c.RunE != nil && c != watchStartCmd && c.Annotations[hintedAnnotation] != annotationTrue &&
		len(root.Hints.Lookup(hintKey(c))) > 0 {
		inner := c.RunE
		c.RunE = func(cmd *cobra.Command, args []string) error {
			if err := inner(cmd, args); err != nil {
				return err
			}
			renderHints(cmd)
			return nil
		}
		if c.Annotations == nil {
			c.Annotations = make(map[string]string)
		}
		c.Annotations[hintedAnnotation] = annotationTrue
	}
	for _, child := range c.Commands() {
		wrapHinted(child)
	}
}

// renderHints prints cmd's active hints through kit. kit decides
// whether anything prints: only table format on a terminal, and never
// under --no-hints, --quiet or HOP_QUIET_HINTS.
func renderHints(cmd *cobra.Command) {
	output.RenderHints(cmd.OutOrStdout(), root.Hints.Lookup(hintKey(cmd)),
		cliformat.Active(), root.Viper, root.Theme.Muted)
}
