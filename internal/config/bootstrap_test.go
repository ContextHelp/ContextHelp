package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hermeticEnv points every config-cascade lookup at a per-test tempdir so
// these tests can never read (or be perturbed by) the developer's real
// config under $HOME.
func hermeticEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv(EnvConfigPath, "")
	return dir
}

// TestBootstrap_ParseErrIsFatal is the core regression guard. An explicit
// -c/--config parse failure previously printed a warning, discarded the
// user's paths + overrides and continued against the DEFAULT config —
// silently retargeting destructive commands at the wrong database.
// Bootstrap must instead return an error and no config at all.
func TestBootstrap_ParseErrIsFatal(t *testing.T) {
	hermeticEnv(t)

	parseErr := errors.New(`-c "/typo/path.yaml": not a key=value pair and no such file`)

	cfg, effective, err := Bootstrap("dpkms", nil, nil, parseErr)
	if err == nil {
		t.Fatal("Bootstrap must fail when -c/--config failed to parse; " +
			"falling back to the default config is a data-safety hazard")
	}
	if !errors.Is(err, ErrConfigArgsInvalid) {
		t.Errorf("error must wrap ErrConfigArgsInvalid, got %v", err)
	}
	if !errors.Is(err, parseErr) {
		t.Errorf("error must wrap the underlying parse error, got %v", err)
	}
	// Naming the offending token is the whole point of the message.
	if got := err.Error(); !strings.Contains(got, "/typo/path.yaml") {
		t.Errorf("error must name the offending path, got %q", got)
	}
	if cfg != nil {
		t.Error("no config may be returned on a fatal parse error; " +
			"a non-nil cfg invites callers to proceed against defaults")
	}
	if effective != "" {
		t.Errorf("effective path must be empty on failure, got %q", effective)
	}
}

// TestBootstrap_NoConfigFlagLoadsDefaults guards the other half of the
// contract: kit's ConfigArgs returns (nil, nil, nil) when no -c was
// supplied, and that ordinary path must keep working.
func TestBootstrap_NoConfigFlagLoadsDefaults(t *testing.T) {
	hermeticEnv(t)

	cfg, effective, err := Bootstrap("dpkms", nil, nil, nil)
	if err != nil {
		t.Fatalf("Bootstrap without -c must succeed: %v", err)
	}
	if cfg == nil {
		t.Fatal("Bootstrap without -c must return a config")
	}
	if effective != "" {
		t.Errorf("effective path must be empty when no -c path given, got %q", effective)
	}
}

// TestBootstrap_ValidPathLoads proves a good -c still layers in and that
// the effective path is reported for path-targeting subcommands.
func TestBootstrap_ValidPathLoads(t *testing.T) {
	dir := hermeticEnv(t)

	path := filepath.Join(dir, "extra.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nserver:\n  port: 4343\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, effective, err := Bootstrap("dpkms", []string{path}, nil, nil)
	if err != nil {
		t.Fatalf("Bootstrap with a valid -c must succeed: %v", err)
	}
	if cfg == nil {
		t.Fatal("Bootstrap with a valid -c must return a config")
	}
	if cfg.Server.Port != 4343 {
		t.Errorf("value from -c file must be applied: server.port = %d, want 4343", cfg.Server.Port)
	}
	if effective != path {
		t.Errorf("effective path = %q, want %q", effective, path)
	}
}

// TestBootstrap_LastPathWins mirrors kit's repeatable -c semantics: later
// paths layer over earlier ones, so the LAST is the effective file.
func TestBootstrap_LastPathWins(t *testing.T) {
	dir := hermeticEnv(t)

	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte("version: 1\nserver:\n  port: 1111\n"), 0o600); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("version: 1\nserver:\n  port: 2222\n"), 0o600); err != nil {
		t.Fatalf("write second: %v", err)
	}

	cfg, effective, err := Bootstrap("dpkms", []string{first, second}, nil, nil)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if cfg.Server.Port != 2222 {
		t.Errorf("later -c must win: server.port = %d, want 2222", cfg.Server.Port)
	}
	if effective != second {
		t.Errorf("effective path = %q, want the last path %q", effective, second)
	}
}

// TestBootstrap_OverridesApplied proves key=value -c tokens still reach
// the merged config.
func TestBootstrap_OverridesApplied(t *testing.T) {
	hermeticEnv(t)

	cfg, _, err := Bootstrap("dpkms", nil, map[string]any{
		"server": map[string]any{"port": 9999},
	}, nil)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("override must be applied: server.port = %d, want 9999", cfg.Server.Port)
	}
}

// TestBootstrap_BrokenFileIsFatal covers the adjacent failure mode: the
// -c token parsed fine (the file exists) but the file itself is
// unreadable YAML. The old code warned and substituted an EMPTY
// config.Config{} — which drops every user setting including storage
// paths, so it is at least as dangerous as the parse-error fallback.
func TestBootstrap_BrokenFileIsFatal(t *testing.T) {
	dir := hermeticEnv(t)

	path := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n  bad: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, effective, err := Bootstrap("dpkms", []string{path}, nil, nil)
	if err == nil {
		t.Fatal("Bootstrap must fail on an unparseable config file rather than " +
			"silently substituting an empty config")
	}
	if !errors.Is(err, ErrConfigLoadFailed) {
		t.Errorf("error must wrap ErrConfigLoadFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Errorf("error must name the offending file, got %q", err.Error())
	}
	if cfg != nil {
		t.Error("no config may be returned when loading failed")
	}
	if effective != "" {
		t.Errorf("effective path must be empty on failure, got %q", effective)
	}
}

// TestBootstrap_MissingEnvConfigIsTolerated pins the first-run
// bootstrap path. CTXT_CONFIG is ambient rather than per-invocation and
// routinely points at a file that does not exist yet — `ctxt setup` is
// the command that creates it. Making config-load failures fatal must
// not turn that normal state into a hard error, or setup can never run.
func TestBootstrap_MissingEnvConfigIsTolerated(t *testing.T) {
	dir := hermeticEnv(t)
	t.Setenv(EnvConfigPath, filepath.Join(dir, "not-created-yet.yaml"))

	cfg, _, err := Bootstrap("ctxt", nil, nil, nil)
	if err != nil {
		t.Fatalf("a CTXT_CONFIG pointing at a not-yet-created file must not "+
			"be fatal (first-run bootstrap): %v", err)
	}
	if cfg == nil {
		t.Fatal("defaults must still load when CTXT_CONFIG file is absent")
	}
}

// TestBootstrap_ExplicitPathStillBeatsEnvConfig guards the precedence
// that moving CTXT_CONFIG into the cascade slot could have broken: an
// explicit -c path layers after the cascade and must still win.
func TestBootstrap_ExplicitPathStillBeatsEnvConfig(t *testing.T) {
	dir := hermeticEnv(t)

	envPath := filepath.Join(dir, "from-env.yaml")
	if err := os.WriteFile(envPath, []byte("version: 1\nserver:\n  port: 1111\n"), 0o600); err != nil {
		t.Fatalf("write env config: %v", err)
	}
	t.Setenv(EnvConfigPath, envPath)

	flagPath := filepath.Join(dir, "from-flag.yaml")
	if err := os.WriteFile(flagPath, []byte("version: 1\nserver:\n  port: 2222\n"), 0o600); err != nil {
		t.Fatalf("write flag config: %v", err)
	}

	cfg, effective, err := Bootstrap("ctxt", []string{flagPath}, nil, nil)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if cfg.Server.Port != 2222 {
		t.Errorf("explicit -c must win over CTXT_CONFIG: server.port = %d, want 2222",
			cfg.Server.Port)
	}
	if effective != flagPath {
		t.Errorf("effective path = %q, want %q", effective, flagPath)
	}
}

// TestBootstrap_SentinelsAreDistinct keeps the two failure classes
// tellable apart: "you typed the flag wrong" vs "the file is broken".
func TestBootstrap_SentinelsAreDistinct(t *testing.T) {
	if errors.Is(ErrConfigArgsInvalid, ErrConfigLoadFailed) ||
		errors.Is(ErrConfigLoadFailed, ErrConfigArgsInvalid) {
		t.Error("ErrConfigArgsInvalid and ErrConfigLoadFailed must be distinct sentinels")
	}
}
