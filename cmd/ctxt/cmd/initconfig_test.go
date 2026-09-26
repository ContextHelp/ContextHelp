package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// clearKitConfigViper clears kit's private viper copy of "config".
//
// executeCommand's resetAllFlags already handles the cobra side correctly
// (resetFlag calls SliceValue.Replace(nil) for slice flags — a plain
// Set(DefValue) would APPEND on a stringArray). But kit's Root keeps its
// own viper, which ConfigArgs consults, so it must be cleared separately
// or a stale -c leaks into the next in-process run.
func clearKitConfigViper() {
	if root != nil && root.Viper != nil {
		root.Viper.Set("config", []string{})
	}
}

// hermeticConfigEnv redirects every config-cascade and data-dir lookup
// into a tempdir, so an in-process CLI test can never read the
// developer's real config — the exact hazard guarded here.
func hermeticConfigEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv("CTXT_INSTANCE", "")
	t.Setenv(config.EnvConfigPath, "")
	return dir
}

// runWithConfigFlag drives the real cobra tree so the assertion covers
// the whole path: OnInitialize(initConfig) → PersistentPreRunE → Execute.
func runWithConfigFlag(t *testing.T, args ...string) (string, error) {
	t.Helper()
	clearKitConfigViper()
	out, err := executeCommand(args...)
	clearKitConfigViper()
	return out, err
}

// TestInitConfig_InvalidConfigFlagIsFatal is the regression guard for the
// data-safety defect: an unparseable explicit -c used to print a warning,
// discard every user-supplied path and override, and continue against the
// DEFAULT config and database. It must now abort before any command body.
func TestInitConfig_InvalidConfigFlagIsFatal(t *testing.T) {
	dir := hermeticConfigEnv(t)
	missing := filepath.Join(dir, "does-not-exist.yaml")

	_, err := runWithConfigFlag(t, "config", "path", "-c", missing)
	if err == nil {
		t.Fatal("an invalid explicit -c must abort with an error, not fall back " +
			"to the default config")
	}
	if !errors.Is(err, config.ErrConfigArgsInvalid) {
		t.Errorf("error must wrap config.ErrConfigArgsInvalid, got %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the offending path %q, got %q", missing, err.Error())
	}

	// The whole point: no config was silently substituted.
	if cfg != nil {
		t.Error("cfg must stay nil after a fatal -c failure; a populated cfg " +
			"means a command body could still run against the default database")
	}
	if cfgFile != "" {
		t.Errorf("cfgFile must stay empty after a fatal -c failure, got %q", cfgFile)
	}
}

// TestInitConfig_DirectoryConfigFlagIsFatal covers kit's other parse
// rejection: a bare-directory token. Same fail-loud contract.
func TestInitConfig_DirectoryConfigFlagIsFatal(t *testing.T) {
	dir := hermeticConfigEnv(t)

	_, err := runWithConfigFlag(t, "config", "path", "-c", dir)
	if err == nil {
		t.Fatal("a bare-directory -c must abort with an error")
	}
	if !errors.Is(err, config.ErrConfigArgsInvalid) {
		t.Errorf("error must wrap config.ErrConfigArgsInvalid, got %v", err)
	}
}

// TestInitConfig_NoConfigFlagStillWorks guards against over-correcting:
// the ordinary no-flag path must keep loading the default cascade.
func TestInitConfig_NoConfigFlagStillWorks(t *testing.T) {
	hermeticConfigEnv(t)

	if _, err := runWithConfigFlag(t, "config", "path"); err != nil {
		t.Fatalf("running without -c must succeed: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg must be populated when no -c was supplied")
	}
	if cfgFile != "" {
		t.Errorf("cfgFile must be empty when no -c path was supplied, got %q", cfgFile)
	}
}

// TestInitConfig_ValidConfigFlagLoads proves a good -c still reaches the
// merged config and is recorded as the effective path.
func TestInitConfig_ValidConfigFlagLoads(t *testing.T) {
	dir := hermeticConfigEnv(t)

	path := filepath.Join(dir, "custom.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 4343\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := runWithConfigFlag(t, "config", "path", "-c", path); err != nil {
		t.Fatalf("a valid -c must succeed: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg must be populated for a valid -c")
	}
	if cfg.Server.Port != 4343 {
		t.Errorf("value from the -c file must be applied: server.port = %d, want 4343",
			cfg.Server.Port)
	}
	if cfgFile != path {
		t.Errorf("cfgFile = %q, want the supplied path %q", cfgFile, path)
	}
}
