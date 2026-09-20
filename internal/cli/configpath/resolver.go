// Package configpath builds kit/console/cli/config-shaped resolvers
// that mirror the per-bin cascade walked by internal/config.LoadWithOverrides.
//
// Adopters wiring `<bin> config path` / `<bin> config paths` via
// kit/console/cli/config.RegisterPathSubcommands call [Resolver] to get a
// closure that returns the true precedence chain. Centralizing the
// resolver here keeps ctxt's and dpkms's `config path(s)` output in
// lock-step with internal/config — kit's stock PathsForTool* helpers
// compose `<tool>/config.yaml`, which is the wrong shape for the
// shared `contexthelp/<bin>.yaml` layout.
package configpath

import (
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	kitconfigcli "hop.top/kit/go/console/cli/config"
)

// Resolver returns a closure usable as kitconfigcli.WithResolver argument.
//
// The closure emits one [kitconfigcli.ResolvedPath] per cascade rung,
// highest precedence first:
//
//  1. $CTXT_CONFIG override (Source="env"). Omitted when unset.
//  2. Nearest `.contexthelp/<bin>.yaml` walking up from cwd (Source="project").
//     When no marker matches, a synthetic non-existent entry rooted at cwd
//     is emitted so `config paths` is still informative on a fresh checkout.
//  3. $XDG_CONFIG_HOME/contexthelp/<bin>.yaml (Source="user"). Omitted when
//     xdg resolution fails (e.g. no $HOME).
//  4. /etc/contexthelp/<bin>.yaml (Source="system"). Always present.
//  5. Synthetic "<defaults>" sentinel (Source="default"). Always present.
//
// `-c/--config` is not surfaced — the flag is parsed at root level and
// the path subcommand runs without it bound. Users who pass `-c` know
// which file they're loading.
func Resolver(bin string) func(cwd string) []kitconfigcli.ResolvedPath {
	return func(cwd string) []kitconfigcli.ResolvedPath {
		var out []kitconfigcli.ResolvedPath

		if envCfg := os.Getenv(config.EnvConfigPath); envCfg != "" {
			out = append(out, kitconfigcli.ResolvedPath{
				Path:   envCfg,
				Source: "env",
				Scope:  config.EnvConfigPath,
				Exists: fileExists(envCfg),
			})
		}

		if project := config.WalkUpForMarker(cwd, bin); project != "" {
			out = append(out, kitconfigcli.ResolvedPath{
				Path:   project,
				Source: "project",
				Exists: true, // WalkUpForMarker only returns paths that os.Stat succeeded on
			})
		} else if cwd != "" {
			candidate := filepath.Join(cwd, ".contexthelp", bin+".yaml")
			out = append(out, kitconfigcli.ResolvedPath{
				Path:   candidate,
				Source: "project",
				Exists: false,
			})
		}

		system, user, _ := config.CascadeSlots(bin)
		if user != "" {
			out = append(out, kitconfigcli.ResolvedPath{
				Path:   user,
				Source: "user",
				Exists: fileExists(user),
			})
		}
		if system != "" {
			out = append(out, kitconfigcli.ResolvedPath{
				Path:   system,
				Source: "system",
				Exists: fileExists(system),
			})
		}

		out = append(out, kitconfigcli.ResolvedPath{
			Path:   "<defaults>",
			Source: "default",
			Exists: true,
		})

		return out
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	// #nosec G703 -- path is a config location being probed for
	// existence: operator supplied, and Stat reads metadata only.
	_, err := os.Stat(path)
	return err == nil
}
