package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// activeProfileName is the focus profile cmd runs under: --profile when
// passed (an explicit "" selects none), else profile.default (CTXT_PROFILE
// over the config file, merged into cfg by Load), else none — the server
// then applies its own default. Every command that honours the global
// --profile resolves it here; see config.ProfileConfig.Active.
//
// The flag is read from cmd, never from viper: viper's "profile" key is an
// alias target for "profile.default" and does not tell a passed flag from
// an unset one.
func activeProfileName(cmd *cobra.Command) string {
	var flag string
	var flagSet bool
	if cmd != nil {
		if f := cmd.Flags().Lookup("profile"); f != nil && f.Changed {
			flag, flagSet = f.Value.String(), true
		}
	}
	var p config.ProfileConfig
	if cfg != nil {
		p = cfg.Profile
	}
	return p.Active(flag, flagSet)
}

// activeProfile resolves the active profile name and its definition; ok
// is false when no profile is active or the name is not defined.
func activeProfile(cmd *cobra.Command) (string, config.FocusProfile, bool) {
	name := activeProfileName(cmd)
	if name == "" || cfg == nil {
		return name, config.FocusProfile{}, false
	}
	p, ok := cfg.Profile.Profiles[name]
	return name, p, ok
}
