package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestLoadConfig_Defaults asserts the daemon config bundle starts in
// the documented v1 posture: substrate defaults, JIT off, GitHub all
// on (DefaultConfig), roster all on (zero-value Gates).
func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig(LoadOptions{})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if cfg.JIT.Enabled {
		t.Errorf("JIT.Enabled = true; want false (default opt-in)")
	}
	if !cfg.GitHub.EnableParent {
		t.Errorf("GitHub.EnableParent = false; want true (DefaultConfig)")
	}
	if !cfg.GitHub.EnableGist {
		t.Errorf("GitHub.EnableGist = false; want true (DefaultConfig)")
	}
	if !cfg.GitHub.EnableSecurityAdvisory {
		t.Errorf("GitHub.EnableSecurityAdvisory = false; want true (DefaultConfig)")
	}
	if cfg.Substrate.Scoring.Weights.SessionTopic != 0.5 {
		t.Errorf("Substrate.Scoring.Weights.SessionTopic = %v; want 0.5", cfg.Substrate.Scoring.Weights.SessionTopic)
	}
	// Roster zero value means every flag nil → roster.enabled() returns true.
	// Confirm via a representative entry.
	if cfg.Roster.GoogleStrategy != nil {
		t.Errorf("default Roster.GoogleStrategy not nil; want nil (= enabled)")
	}
}

// TestLoadConfig_StrategyGatesFlipFromYAML asserts the on-disk
// strategies.* block flips per-strategy gates correctly.
func TestLoadConfig_StrategyGatesFlipFromYAML(t *testing.T) {
	dir := t.TempDir()
	body := `strategies:
  jit:
    enabled: true
  github:
    enable_parent: true
    enable_gist: false
    enable_security_advisory: true
  x:
    enabled: false
  google:
    enabled: false
  youtube:
    enabled: false
`
	p := writeFile(t, dir, "lateral.yaml", body)
	cfg, err := LoadConfig(LoadOptions{ProjectConfigPath: p})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if !cfg.JIT.Enabled {
		t.Errorf("JIT.Enabled = false; want true")
	}
	if cfg.GitHub.EnableGist {
		t.Errorf("GitHub.EnableGist = true; want false")
	}
	if !cfg.GitHub.EnableSecurityAdvisory {
		t.Errorf("GitHub.EnableSecurityAdvisory = false; want true")
	}
	if cfg.Roster.XStrategy == nil || *cfg.Roster.XStrategy {
		t.Errorf("Roster.XStrategy = %v; want pointer-to-false", cfg.Roster.XStrategy)
	}
	if cfg.Roster.GoogleStrategy == nil || *cfg.Roster.GoogleStrategy {
		t.Errorf("Roster.GoogleStrategy = %v; want pointer-to-false", cfg.Roster.GoogleStrategy)
	}
	if cfg.Roster.YouTubeStrategy == nil || *cfg.Roster.YouTubeStrategy {
		t.Errorf("Roster.YouTubeStrategy = %v; want pointer-to-false", cfg.Roster.YouTubeStrategy)
	}
	// Untouched gates stay nil (= enabled by default).
	if cfg.Roster.MediumStrategy != nil {
		t.Errorf("Roster.MediumStrategy = %v; want nil", cfg.Roster.MediumStrategy)
	}
}

// TestLoadConfig_LayersRespectPrecedence asserts project layer beats
// user layer for both substrate and strategies blocks.
func TestLoadConfig_LayersRespectPrecedence(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "user.yaml", `strategies:
  jit:
    enabled: false
  google:
    enabled: false
`)
	proj := writeFile(t, dir, "project.yaml", `strategies:
  jit:
    enabled: true
  google:
    enabled: true
`)
	cfg, err := LoadConfig(LoadOptions{
		UserConfigPath:    user,
		ProjectConfigPath: proj,
	})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if !cfg.JIT.Enabled {
		t.Errorf("project layer should win for JIT; got false")
	}
	if cfg.Roster.GoogleStrategy == nil || !*cfg.Roster.GoogleStrategy {
		t.Errorf("project layer should win for Google; got %v", cfg.Roster.GoogleStrategy)
	}
}

// TestLoadConfig_MissingFile_NoError mirrors kit's tolerant behaviour
// for the System/User/Project slots: a missing file is not an error.
func TestLoadConfig_MissingFile_NoError(t *testing.T) {
	cfg, err := LoadConfig(LoadOptions{
		ProjectConfigPath: filepath.Join(t.TempDir(), "does-not-exist.yaml"),
	})
	if err != nil {
		t.Fatalf("LoadConfig err = %v; want nil for missing file", err)
	}
	// Defaults still apply.
	if cfg.GitHub.EnableParent != true {
		t.Errorf("missing-file fallback should keep GitHub default; got %v", cfg.GitHub)
	}
}

// TestLoadConfig_EmptyStrategiesBlock leaves defaults intact. Just
// declaring the lateral key with no strategies child shouldn't change
// anything.
func TestLoadConfig_EmptyStrategiesBlock(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "lateral.yaml", `scoring:
  weights:
    session_topic: 0.7
`)
	cfg, err := LoadConfig(LoadOptions{ProjectConfigPath: p})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if cfg.Substrate.Scoring.Weights.SessionTopic != 0.7 {
		t.Errorf("substrate scoring not applied; got %v", cfg.Substrate.Scoring.Weights.SessionTopic)
	}
	if cfg.JIT.Enabled {
		t.Errorf("JIT shouldn't flip without strategies block")
	}
}

// TestLoadConfig_JITAbsentBlockPreservesLowerLayer pins the precedence
// fix: a higher-precedence layer that omits the strategies.jit block
// must NOT silently disable JIT when a lower-precedence layer enabled
// it. Without the *bool wrapper, "absent" and "explicitly false" looked
// identical and overwrote prior layers with a zero-value bool.
func TestLoadConfig_JITAbsentBlockPreservesLowerLayer(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "user.yaml", `strategies:
  jit:
    enabled: true
`)
	// Project layer omits jit entirely (no strategies.jit block).
	proj := writeFile(t, dir, "project.yaml", `strategies:
  google:
    enabled: false
`)
	cfg, err := LoadConfig(LoadOptions{
		UserConfigPath:    user,
		ProjectConfigPath: proj,
	})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if !cfg.JIT.Enabled {
		t.Error("JIT.Enabled = false; want true (user-layer enable preserved when project layer omits jit block)")
	}
}

// TestLoadConfig_JITPartialEntryNoOp mirrors TestLoadConfig_PartialStrategyEntryNoOp
// for JIT: a strategies.jit: {} block (no enabled key) leaves the gate
// alone. Distinguishes "mentioned but not configured" from "explicitly
// false."
func TestLoadConfig_JITPartialEntryNoOp(t *testing.T) {
	dir := t.TempDir()
	user := writeFile(t, dir, "user.yaml", `strategies:
  jit:
    enabled: true
`)
	proj := writeFile(t, dir, "project.yaml", `strategies:
  jit: {}
`)
	cfg, err := LoadConfig(LoadOptions{
		UserConfigPath:    user,
		ProjectConfigPath: proj,
	})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if !cfg.JIT.Enabled {
		t.Error("JIT.Enabled = false; want true (empty jit block must not flip the gate)")
	}
}

// TestLoadConfig_PartialStrategyEntryNoOp asserts that a
// strategies.<name>: {} block (no enabled key) leaves the gate alone.
// Distinguishes "explicitly false" from "mentioned but not configured."
func TestLoadConfig_PartialStrategyEntryNoOp(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "lateral.yaml", `strategies:
  google: {}
  medium:
    enabled: false
`)
	cfg, err := LoadConfig(LoadOptions{ProjectConfigPath: p})
	if err != nil {
		t.Fatalf("LoadConfig err = %v", err)
	}
	if cfg.Roster.GoogleStrategy != nil {
		t.Errorf("empty google entry shouldn't set the gate; got %v", cfg.Roster.GoogleStrategy)
	}
	if cfg.Roster.MediumStrategy == nil || *cfg.Roster.MediumStrategy {
		t.Errorf("Medium gate should be pointer-to-false; got %v", cfg.Roster.MediumStrategy)
	}
}
