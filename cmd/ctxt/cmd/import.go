package cmd

import "github.com/spf13/cobra"

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data from external sources",
	Long: `Import external knowledge sources into ContextHelp.

Use source-specific subcommands such as:
  ctxt import chrome --file bookmarks.html`,
}

func init() {
	rootCmd.AddCommand(importCmd)
}
