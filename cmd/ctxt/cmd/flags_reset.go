package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetAllFlags puts every flag of cmd and its subcommands back to its
// default and unmarks it changed, so one run of the command tree (a
// shell line) never sees the flags of the previous one.
func resetAllFlags(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		resetFlag(f)
		f.Changed = false
	}
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
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
