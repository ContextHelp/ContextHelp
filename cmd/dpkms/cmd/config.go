package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/cli/configpath"
	"github.com/spf13/cobra"
	kitconfigcli "hop.top/kit/go/console/cli/config"
)

// configCmd is the parent for kit-provided introspection subcommands.
//
// Adds two subcommands via kit/console/cli/config.RegisterPathSubcommands:
//
//	dpkms config path     # highest-precedence existing config file
//	dpkms config paths    # full ordered chain, highest-precedence first
//
// Both honour --format=text|json|yaml and --from <dir>. The resolver comes
// from internal/cli/configpath, which mirrors what internal/config.Load
// actually walks at startup (per-bin file under the shared `contexthelp/`
// namespace: `.contexthelp/dpkms.yaml`, `$XDG_CONFIG_HOME/contexthelp/dpkms.yaml`,
// `/etc/contexthelp/dpkms.yaml`, plus `$CTXT_CONFIG`).
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect dpkms configuration",
	Args:  cobra.NoArgs,
}

func init() {
	rootCmd.AddCommand(configCmd)

	kitconfigcli.RegisterPathSubcommands(configCmd, "dpkms",
		kitconfigcli.WithResolver(configpath.Resolver(binName)))
}
