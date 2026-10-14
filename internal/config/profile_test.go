package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Active: a passed --profile (even empty) wins over profile.default;
// without one, profile.default; with neither, none.
func TestProfileConfigActive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured string
		flag       string
		flagSet    bool
		want       string
	}{
		{name: "neither", want: ""},
		{name: "default only", configured: "ops", want: "ops"},
		{name: "flag only", flag: "growth", flagSet: true, want: "growth"},
		{name: "flag over default", configured: "ops", flag: "growth", flagSet: true, want: "growth"},
		{name: "empty flag selects none", configured: "ops", flag: "", flagSet: true, want: ""},
		{name: "unset flag value ignored", configured: "ops", flag: "growth", flagSet: false, want: "ops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := ProfileConfig{Default: tc.configured}
			assert.Equal(t, tc.want, p.Active(tc.flag, tc.flagSet))
		})
	}
}

// profile.default reaching Active already carries CTXT_PROFILE over the
// config file: Load merges the env var into the typed config.
func TestProfileConfigActiveAfterLoad(t *testing.T) {
	const body = "profile:\n  default: ops\n  profiles:\n    ops: {}\n    growth: {}\n"
	for _, tc := range []struct {
		name    string
		env     string
		flag    string
		flagSet bool
		want    string
	}{
		{name: "file default", want: "ops"},
		{name: "env over file", env: "growth", want: "growth"},
		{name: "flag over env", env: "growth", flag: "ops", flagSet: true, want: "ops"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			t.Setenv(EnvConfigPath, "")
			t.Setenv(EnvProfile, tc.env)
			path := filepath.Join(xdg, "ctxt.yaml")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			cfg, err := LoadWithOverrides("ctxt", "", []string{path}, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.Profile.Active(tc.flag, tc.flagSet))
		})
	}
}
