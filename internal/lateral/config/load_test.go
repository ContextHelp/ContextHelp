package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ProjectOverridesUser(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	proj := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(user, []byte("scoring:\n  weights:\n    session_topic: 0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proj, []byte("scoring:\n  weights:\n    session_topic: 0.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		UserConfigPath:    user,
		ProjectConfigPath: proj,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scoring.Weights.SessionTopic != 0.2 {
		t.Fatalf("project layer should win, got %v", cfg.Scoring.Weights.SessionTopic)
	}
}

func TestEffectiveForStrategy_LayersStrategyOnTop(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "lateral.yaml")
	strategy := filepath.Join(dir, "lateral.sibling-repo.yaml")
	if err := os.WriteFile(base, []byte("scoring:\n  weights:\n    session_topic: 0.5\n    capture_window: 0.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strategy, []byte("scoring:\n  weights:\n    session_topic: 0.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := EffectiveForStrategy(LoadOptions{ProjectConfigPath: base}, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scoring.Weights.SessionTopic != 0.7 {
		t.Fatalf("strategy override should win, got %v", cfg.Scoring.Weights.SessionTopic)
	}
	if cfg.Scoring.Weights.CaptureWindow != 0.3 {
		t.Fatalf("base value should survive when strategy didn't touch it, got %v", cfg.Scoring.Weights.CaptureWindow)
	}
}

func TestLoad_DefaultsApplyWhenNoFiles(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scoring.Weights.SessionTopic != 0.5 {
		t.Fatalf("default SessionTopic=0.5 expected, got %v", cfg.Scoring.Weights.SessionTopic)
	}
	if cfg.Lifecycle.SoftDeleteDays != 30 {
		t.Fatalf("default SoftDeleteDays=30 expected, got %v", cfg.Lifecycle.SoftDeleteDays)
	}
	if cfg.Jobs.EngineKind != "memory" {
		t.Fatalf("default Jobs.EngineKind=memory expected, got %q", cfg.Jobs.EngineKind)
	}
}
