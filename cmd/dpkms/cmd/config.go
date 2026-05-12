package cmd

import (
	"github.com/spf13/cobra"
	kitconfig "hop.top/kit/go/core/config"
	kitconfigcli "hop.top/kit/go/console/cli/config"
)

// configCmd is the parent for kit-provided introspection subcommands.
//
// Adds two subcommands via kit/console/cli/config.RegisterPathSubcommands:
//
//	dpkms config path     # highest-precedence existing config file
//	dpkms config paths    # full ordered chain, highest-precedence first
//
// Both honour --format=text|json|yaml and --from <dir>. The resolver wires
// dpkms's project markers (.ctxt/config.yaml, .ctxt.yaml, ctxt.yaml) onto
// kit's canonical 4-layer cascade so the precedence chain matches what
// internal/config.Load actually walks at startup.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect dpkms configuration",
	Args:  cobra.NoArgs,
}

func init() {
	rootCmd.AddCommand(configCmd)

	resolver := func(cwd string) []kitconfigcli.ResolvedPath {
		raw := kitconfig.PathsForToolWithMarkers(cwd, "ctxt", []string{
			".ctxt/config.yaml",
			".ctxt.yaml",
			"ctxt.yaml",
		})
		out := make([]kitconfigcli.ResolvedPath, len(raw))
		for i, r := range raw {
			out[i] = kitconfigcli.ResolvedPath{
				Path:   r.Path,
				Source: r.Source,
				Scope:  r.Scope,
				Exists: r.Exists,
			}
		}
		return out
	}

	kitconfigcli.RegisterPathSubcommands(configCmd, "dpkms",
		kitconfigcli.WithResolver(resolver))
}
