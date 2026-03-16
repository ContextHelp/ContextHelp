package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadFromYAML is a test helper that writes yaml content to a temp file and loads it.
func loadFromYAML(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return Load(cfgPath)
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with no config file should succeed: %v", err)
	}

	if cfg.Storage.Type != "sqlite" {
		t.Errorf("expected storage.type=sqlite, got %s", cfg.Storage.Type)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected server.port=8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("expected server.grpc_port=9090, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Server.Workers != 4 {
		t.Errorf("expected server.workers=4, got %d", cfg.Server.Workers)
	}
	if cfg.Server.Public {
		t.Error("expected server.public=false")
	}
	if cfg.I18n.Enabled {
		t.Error("expected i18n.enabled=false")
	}
	if cfg.I18n.AutoTranslate {
		t.Error("expected i18n.auto_translate=false")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := []byte(`storage:
  type: postgres
  path: /custom/db
server:
  port: 3000
  grpc_port: 3001
  workers: 8
  public: true
profile:
  default: engineering
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load with valid config should succeed: %v", err)
	}

	if cfg.Storage.Type != "postgres" {
		t.Errorf("expected storage.type=postgres, got %s", cfg.Storage.Type)
	}
	if cfg.Storage.Path != "/custom/db" {
		t.Errorf("expected storage.path=/custom/db, got %s", cfg.Storage.Path)
	}
	if cfg.Server.Port != 3000 {
		t.Errorf("expected server.port=3000, got %d", cfg.Server.Port)
	}
	if cfg.Server.GRPCPort != 3001 {
		t.Errorf("expected server.grpc_port=3001, got %d", cfg.Server.GRPCPort)
	}
	if cfg.Server.Workers != 8 {
		t.Errorf("expected server.workers=8, got %d", cfg.Server.Workers)
	}
	if !cfg.Server.Public {
		t.Error("expected server.public=true")
	}
	if cfg.Profile.Default != "engineering" {
		t.Errorf("expected profile.default=engineering, got %s", cfg.Profile.Default)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}
}

func TestGetConfigPath(t *testing.T) {
	path := GetConfigPath()
	if path == "" {
		t.Error("GetConfigPath should return a non-empty path")
	}
	if filepath.Base(path) != DefaultConfigFileName {
		t.Errorf("expected filename %s, got %s", DefaultConfigFileName, filepath.Base(path))
	}
}

func TestGetConfigPathWithEnv(t *testing.T) {
	t.Setenv(EnvConfigPath, "/custom/config.yaml")
	path := GetConfigPath()
	if path != "/custom/config.yaml" {
		t.Errorf("expected /custom/config.yaml, got %s", path)
	}
}

func TestEnsureConfigDir(t *testing.T) {
	err := EnsureConfigDir()
	if err != nil {
		t.Fatalf("EnsureConfigDir should succeed: %v", err)
	}
}

func TestEnsureDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, filepath.Join(dir, "data"))

	err := EnsureDataDir()
	if err != nil {
		t.Fatalf("EnsureDataDir should succeed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "data")); os.IsNotExist(err) {
		t.Error("data directory should have been created")
	}
}

func TestLoadWithRegistries(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := []byte(`registries:
  - name: uxpatterns
    url: https://uxpatterns.example.com
  - name: devtools
    url: https://devtools.registry.io
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed: %v", err)
	}

	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Name != "uxpatterns" {
		t.Errorf("expected first registry name=uxpatterns, got %s", cfg.Registries[0].Name)
	}
	if cfg.Registries[1].URL != "https://devtools.registry.io" {
		t.Errorf("expected second registry URL, got %s", cfg.Registries[1].URL)
	}
}

func TestLoadWithPlugins(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := []byte(`plugins:
  - type: enrichment
    plugin: custom-extractor
    config:
      model: gpt-4
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed: %v", err)
	}

	if len(cfg.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(cfg.Plugins))
	}
	if cfg.Plugins[0].Type != "enrichment" {
		t.Errorf("expected plugin type=enrichment, got %s", cfg.Plugins[0].Type)
	}
	if cfg.Plugins[0].Plugin != "custom-extractor" {
		t.Errorf("expected plugin name=custom-extractor, got %s", cfg.Plugins[0].Plugin)
	}
}

func TestLoadWithI18n(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := []byte(`i18n:
  enabled: true
  preferred_languages:
    - en
    - fr
  auto_translate: true
  translate_tags: true
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load should succeed: %v", err)
	}

	if !cfg.I18n.Enabled {
		t.Error("expected i18n.enabled=true")
	}
	if !cfg.I18n.AutoTranslate {
		t.Error("expected i18n.auto_translate=true")
	}
	if !cfg.I18n.TranslateTags {
		t.Error("expected i18n.translate_tags=true")
	}
	if len(cfg.I18n.PreferredLanguages) != 2 {
		t.Errorf("expected 2 languages, got %d", len(cfg.I18n.PreferredLanguages))
	}
}

func TestEnsureConfigDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission test not reliable on Windows")
	}

	tmp := t.TempDir()
	readonlyDir := filepath.Join(tmp, "readonly")
	require.NoError(t, os.MkdirAll(readonlyDir, 0755))

	// Lock the directory so MkdirAll cannot create children
	require.NoError(t, os.Chmod(readonlyDir, 0444))
	t.Cleanup(func() {
		os.Chmod(readonlyDir, 0755) // restore so TempDir cleanup can remove it
	})

	// Point CTXT_CONFIG to a path that requires creating a subdir inside the read-only dir
	t.Setenv(EnvConfigPath, filepath.Join(readonlyDir, "subdir", "config.yaml"))

	err := EnsureConfigDir()
	assert.Error(t, err, "EnsureConfigDir should fail when parent directory is read-only")
}

func TestEnsureConfigDirCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "newdir")
	configFile := filepath.Join(configDir, "config.yaml")

	t.Setenv(EnvConfigPath, configFile)

	err := EnsureConfigDir()
	require.NoError(t, err, "EnsureConfigDir should succeed for a writable temp path")

	info, statErr := os.Stat(configDir)
	require.NoError(t, statErr, "config directory should exist after EnsureConfigDir")
	assert.True(t, info.IsDir(), "config path should be a directory")
}

func TestGetConfigPathDefault(t *testing.T) {
	// Ensure the env var is unset so we exercise the default path
	t.Setenv(EnvConfigPath, "")

	path := GetConfigPath()
	assert.NotEmpty(t, path, "GetConfigPath should return a non-empty path when env is unset")
	assert.True(t, strings.HasSuffix(path, DefaultConfigFileName),
		"path should end with %s, got %s", DefaultConfigFileName, path)
}

func TestGetConfigPathEnvOverride(t *testing.T) {
	custom := "/tmp/custom/config.yaml"
	t.Setenv(EnvConfigPath, custom)

	path := GetConfigPath()
	assert.Equal(t, custom, path, "GetConfigPath should return the exact env value")
}

func TestDuplicatesValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid warn policy",
			cfg:  Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: 0.95}},
		},
		{
			name: "valid drop policy",
			cfg:  Config{Duplicates: DuplicatesConfig{Policy: "drop", SimilarityThreshold: 0.80}},
		},
		{
			name: "valid keep policy",
			cfg:  Config{Duplicates: DuplicatesConfig{Policy: "keep", SimilarityThreshold: 0.99}},
		},
		{
			name:    "invalid policy",
			cfg:     Config{Duplicates: DuplicatesConfig{Policy: "delete", SimilarityThreshold: 0.95}},
			wantErr: true,
		},
		{
			name:    "threshold too high",
			cfg:     Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: 1.01}},
			wantErr: true,
		},
		{
			name:    "threshold negative",
			cfg:     Config{Duplicates: DuplicatesConfig{Policy: "warn", SimilarityThreshold: -0.1}},
			wantErr: true,
		},
		{
			name: "empty policy defaults to warn (pass)",
			cfg:  Config{Duplicates: DuplicatesConfig{Policy: "", SimilarityThreshold: 0.95}},
			// empty policy treated as "warn" by validator
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDuplicatesDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "warn", cfg.Duplicates.Policy)
	assert.Equal(t, 0.95, cfg.Duplicates.SimilarityThreshold)
	assert.True(t, cfg.Duplicates.CheckExact)
	assert.False(t, cfg.Duplicates.CheckSimilar)
}

func TestBlobConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Storage.Blob.Backend != "local" {
		t.Errorf("blob backend: got %q, want %q", cfg.Storage.Blob.Backend, "local")
	}
	if cfg.Storage.Blob.Threshold != 65536 {
		t.Errorf("blob threshold: got %d, want %d", cfg.Storage.Blob.Threshold, 65536)
	}
	if cfg.Storage.Blob.S3.Region != "us-east-1" {
		t.Errorf("s3 region: got %q, want %q", cfg.Storage.Blob.S3.Region, "us-east-1")
	}
	if cfg.Storage.Blob.S3.MaxRetries != 3 {
		t.Errorf("s3 max_retries: got %d, want %d", cfg.Storage.Blob.S3.MaxRetries, 3)
	}
	if cfg.Storage.Blob.S3.PresignExpiry != time.Hour {
		t.Errorf("s3 presign_expiry: got %v, want %v", cfg.Storage.Blob.S3.PresignExpiry, time.Hour)
	}
}

func TestSearchConfigDefaults(t *testing.T) {
	cfg, err := loadFromYAML(t, `version: 1`)
	require.NoError(t, err)
	assert.Equal(t, "hybrid", cfg.Search.DefaultMode)
	assert.Equal(t, 60, cfg.Search.RRF.K)
	assert.InDelta(t, 0.5, cfg.Search.RRF.FTSWeight, 0.001)
	assert.InDelta(t, 0.5, cfg.Search.RRF.VectorWeight, 0.001)
	assert.Equal(t, 50, cfg.Search.CandidatePool.FTS)
	assert.Equal(t, 50, cfg.Search.CandidatePool.Vector)
	assert.InDelta(t, 0.0, cfg.Search.MinScore, 0.001)
	assert.True(t, cfg.Search.FallbackToFTS)
}
