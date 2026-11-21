package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"hop.top/kit/go/runtime/sideeffect"
)

// resetAllFlags puts every flag of cmd and its subcommands back to its
// default and unmarks it changed, so one run of the command tree (a
// shell line) never sees the flags of the previous one. It also clears
// the dry-run mark an earlier --dry-run run left on a command's context.
//
// Kit records a dry run twice: in the --dry-run flag and, when it
// applies the flag, as a mark on the leaf's context; kitcli.IsDryRun
// reads either. Cobra keeps a subcommand's context across in-process
// runs, so resetting the flag alone leaves every later run of that leaf
// a dry run. The rest of the context is kept: callers inject a
// cancelable one on a subcommand before executing it.
func resetAllFlags(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		resetFlag(f)
		f.Changed = false
	}
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
	if ctx := cmd.Context(); ctx != nil && sideeffect.IsDryRun(ctx) {
		cmd.SetContext(sideeffect.WithDryRun(ctx, false))
	}
	for _, sub := range cmd.Commands() {
		resetAllFlags(sub)
	}
}

// resetFlag sets f to its default value. A slice flag's default renders
// as "[a,b]"; it is replaced element-wise rather than appended to.
func resetFlag(f *pflag.Flag) {
	if slice, ok := f.Value.(pflag.SliceValue); ok {
		def := strings.TrimSuffix(strings.TrimPrefix(f.DefValue, "["), "]")
		var vals []string
		if def != "" {
			vals = strings.Split(def, ",")
		}
		_ = slice.Replace(vals)
		return
	}
	_ = f.Value.Set(f.DefValue)
}
