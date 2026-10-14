package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// serveEnv lists the env vars that set serve's settings; each test
// starts with all of them unset.
var serveEnv = append([]string{config.EnvDPKMSWorkers, "CH_PUBLIC", config.EnvProfile}, portEnv...)

// loadServeConfig loads the dpkms config the way serve gets it, with
// body (when non-empty) as a -c config file, against an empty XDG tree.
// The serve env vars are set from env (unset when absent).
func loadServeConfig(t *testing.T, body string, env map[string]string) *config.Config {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(config.EnvConfigPath, "")
	for _, k := range serveEnv {
		t.Setenv(k, env[k])
	}
	var paths []string
	if body != "" {
		path := filepath.Join(xdg, "serve.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	c, err := config.LoadWithOverrides(binName, "", paths, nil)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return c
}

// serveSettingsFor parses args as serve flags and returns the settings
// serve would run with under c.
func serveSettingsFor(t *testing.T, c *config.Config, args ...string) serveSettings {
	t.Helper()
	resetAllFlags(serveCmd)
	t.Cleanup(func() { resetAllFlags(serveCmd) })
	if err := serveCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return resolveServeSettings(serveCmd.Flags(), c)
}

func assertSettings(t *testing.T, got, want serveSettings) {
	t.Helper()
	if got != want {
		t.Errorf("serve settings = %+v, want %+v", got, want)
	}
}

const serveSettingsYAML = `server:
  workers: 7
  dev: true
  reminder_interval: 5s
steps:
  path: /srv/steps
`

// With no flag, env or config serve runs on the built-in defaults.
func TestServeSettings_Defaults(t *testing.T) {
	assertSettings(t, serveSettingsFor(t, loadServeConfig(t, "", nil)), serveSettings{
		workers:          config.DefaultServerWorkers,
		reminderInterval: config.DefaultReminderInterval,
	})
}

// Settings in the config file reach serve.
func TestServeSettings_FromConfig(t *testing.T) {
	assertSettings(t, serveSettingsFor(t, loadServeConfig(t, serveSettingsYAML, nil)), serveSettings{
		workers:          7,
		dev:              true,
		reminderInterval: 5 * time.Second,
		stepsPath:        "/srv/steps",
	})
}

// DPKMS_WORKERS beats the config file.
func TestServeSettings_EnvOverridesConfig(t *testing.T) {
	c := loadServeConfig(t, serveSettingsYAML, map[string]string{config.EnvDPKMSWorkers: "9"})
	if got := serveSettingsFor(t, c).workers; got != 9 {
		t.Errorf("workers = %d, want 9 from %s over the config file", got, config.EnvDPKMSWorkers)
	}

	c = loadServeConfig(t, "", map[string]string{config.EnvDPKMSWorkers: "11"})
	if got := serveSettingsFor(t, c).workers; got != 11 {
		t.Errorf("workers = %d, want 11 from %s alone", got, config.EnvDPKMSWorkers)
	}
}

// A passed flag beats env and config file, including a flag set back to
// its own default value.
func TestServeSettings_FlagOverridesEnvAndConfig(t *testing.T) {
	c := loadServeConfig(t, serveSettingsYAML, map[string]string{config.EnvDPKMSWorkers: "9"})
	assertSettings(t, serveSettingsFor(t, c,
		"--workers", "4", "--dev=false", "--reminder-interval", "1m", "--steps-path", "/opt/steps"),
		serveSettings{
			workers:          4,
			dev:              false,
			reminderInterval: time.Minute,
			stepsPath:        "/opt/steps",
		})
}

// An unpassed flag leaves the config value alone.
func TestServeSettings_UnpassedFlagKeepsConfig(t *testing.T) {
	c := loadServeConfig(t, serveSettingsYAML, nil)
	assertSettings(t, serveSettingsFor(t, c, "--workers", "2"), serveSettings{
		workers:          2,
		dev:              true,
		reminderInterval: 5 * time.Second,
		stepsPath:        "/srv/steps",
	})
}

// resolveDefaultProfile settles cfg.Profile.Default: --profile, then
// profile.default (config file or CTXT_PROFILE), then the only profile.
func TestResolveDefaultProfile(t *testing.T) {
	const twoProfiles = "profile:\n  default: b\n  profiles:\n    a: {}\n    b: {}\n"
	for _, tc := range []struct {
		name string
		body string
		env  map[string]string
		args []string
		want string
	}{
		{name: "config default", body: twoProfiles, want: "b"},
		{name: "env over config", body: twoProfiles, env: map[string]string{config.EnvProfile: "a"}, want: "a"},
		{name: "flag over config", body: twoProfiles, args: []string{"--profile", "a"}, want: "a"},
		{name: "flag over env", body: twoProfiles, env: map[string]string{config.EnvProfile: "b"}, args: []string{"--profile", "a"}, want: "a"},
		{name: "empty flag selects none", body: "profile:\n  default: solo\n  profiles:\n    solo: {}\n", args: []string{"--profile", ""}, want: ""},
		{name: "single profile", body: "profile:\n  profiles:\n    solo: {}\n", want: "solo"},
		{name: "no profiles", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := cfg
			t.Cleanup(func() { cfg = prev })
			cfg = loadServeConfig(t, tc.body, tc.env)

			resetAllFlags(serveCmd)
			t.Cleanup(func() { resetAllFlags(serveCmd) })
			if err := serveCmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}
			if err := resolveDefaultProfile(serveCmd); err != nil {
				t.Fatalf("resolveDefaultProfile: %v", err)
			}
			if cfg.Profile.Default != tc.want {
				t.Errorf("cfg.Profile.Default = %q, want %q", cfg.Profile.Default, tc.want)
			}
		})
	}
}
