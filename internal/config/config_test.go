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
	return Load("ctxt", cfgPath)
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("ctxt", "")
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

	cfg, err := Load("ctxt", cfgPath)
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
	_, err := Load("ctxt", "/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}
}

func TestGetConfigPath(t *testing.T) {
	path := GetConfigPath("ctxt")
	if path == "" {
		t.Error("GetConfigPath should return a non-empty path")
	}
	if filepath.Base(path) != "ctxt.yaml" {
		t.Errorf("expected filename ctxt.yaml, got %s", filepath.Base(path))
	}
}

func TestGetConfigPathWithEnv(t *testing.T) {
	t.Setenv(EnvConfigPath, "/custom/config.yaml")
	path := GetConfigPath("ctxt")
	if path != "/custom/config.yaml" {
		t.Errorf("expected /custom/config.yaml, got %s", path)
	}
}

func TestEnsureConfigDir(t *testing.T) {
	err := EnsureConfigDir("ctxt")
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

	cfg, err := Load("ctxt", cfgPath)
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

	cfg, err := Load("ctxt", cfgPath)
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

	cfg, err := Load("ctxt", cfgPath)
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

	err := EnsureConfigDir("ctxt")
	assert.Error(t, err, "EnsureConfigDir should fail when parent directory is read-only")
}

func TestEnsureConfigDirCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "newdir")
	configFile := filepath.Join(configDir, "config.yaml")

	t.Setenv(EnvConfigPath, configFile)

	err := EnsureConfigDir("ctxt")
	require.NoError(t, err, "EnsureConfigDir should succeed for a writable temp path")

	info, statErr := os.Stat(configDir)
	require.NoError(t, statErr, "config directory should exist after EnsureConfigDir")
	assert.True(t, info.IsDir(), "config path should be a directory")
}

func TestGetConfigPathDefault(t *testing.T) {
	// Ensure the env var is unset so we exercise the default path
	t.Setenv(EnvConfigPath, "")

	path := GetConfigPath("ctxt")
	assert.NotEmpty(t, path, "GetConfigPath should return a non-empty path when env is unset")
	assert.True(t, strings.HasSuffix(path, "ctxt.yaml"),
		"path should end with ctxt.yaml, got %s", path)
}

func TestGetConfigPathEnvOverride(t *testing.T) {
	custom := "/tmp/custom/config.yaml"
	t.Setenv(EnvConfigPath, custom)

	path := GetConfigPath("ctxt")
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
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, "warn", cfg.Duplicates.Policy)
	assert.Equal(t, 0.95, cfg.Duplicates.SimilarityThreshold)
	assert.True(t, cfg.Duplicates.CheckExact)
	assert.False(t, cfg.Duplicates.CheckSimilar)
}

func TestBlobConfigDefaults(t *testing.T) {
	cfg, err := Load("ctxt", "")
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

func TestResolveSearchConfig(t *testing.T) {
	global := SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: CandidatePoolConfig{FTS: 50, Vector: 50},
		MinScore:      0.0,
		FallbackToFTS: true,
	}
	profile := ProfileSearchStrategy{
		Mode: "vector",
		RRF:  RRFConfig{FTSWeight: 0.2, VectorWeight: 0.8},
	}
	resolved := ResolveSearchConfig(global, profile)
	assert.Equal(t, "vector", resolved.DefaultMode)
	assert.Equal(t, 60, resolved.RRF.K)          // inherited
	assert.InDelta(t, 0.2, resolved.RRF.FTSWeight, 0.001)    // overridden
	assert.InDelta(t, 0.8, resolved.RRF.VectorWeight, 0.001)  // overridden
	assert.Equal(t, 50, resolved.CandidatePool.FTS) // inherited
}

func TestProfileSearchStrategyOverride(t *testing.T) {
	cfg, err := loadFromYAML(t, `
version: 1
profile:
  profiles:
    research:
      description: Research mode
      search_strategy:
        mode: vector
        rrf:
          fts_weight: 0.2
          vector_weight: 0.8
`)
	require.NoError(t, err)
	p := cfg.Profile.Profiles["research"]
	assert.Equal(t, "vector", p.SearchStrategy.Mode)
	assert.InDelta(t, 0.2, p.SearchStrategy.RRF.FTSWeight, 0.001)
	assert.InDelta(t, 0.8, p.SearchStrategy.RRF.VectorWeight, 0.001)
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

func TestBackupConfigDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.Backup.Dir != "" {
		t.Fatalf("expected empty backup dir, got %q", cfg.Backup.Dir)
	}
}

func TestFederationsValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errFrag string
	}{
		{
			name: "zero federations",
			cfg:  Config{},
		},
		{
			name: "valid async entry",
			cfg: Config{Federations: []FederationEntry{
				{Name: "peer-a", URL: "https://peer-a.example.com", SyncMode: "async", Interval: 5 * time.Minute},
			}},
		},
		{
			name: "valid inline entry",
			cfg: Config{Federations: []FederationEntry{
				{Name: "peer-b", URL: "https://peer-b.example.com", SyncMode: "inline"},
			}},
		},
		{
			name: "valid mixed async and inline",
			cfg: Config{Federations: []FederationEntry{
				{Name: "async-peer", URL: "https://a.example.com", SyncMode: "async", Interval: time.Hour},
				{Name: "inline-peer", URL: "https://b.example.com", SyncMode: "inline"},
			}},
		},
		{
			name: "unknown sync_mode",
			cfg: Config{Federations: []FederationEntry{
				{Name: "bad-peer", URL: "https://bad.example.com", SyncMode: "push"},
			}},
			wantErr: true,
			errFrag: "sync_mode",
		},
		{
			name: "async without interval",
			cfg: Config{Federations: []FederationEntry{
				{Name: "no-interval", URL: "https://peer.example.com", SyncMode: "async"},
			}},
			wantErr: true,
			errFrag: "interval",
		},
		{
			name: "duplicate name",
			cfg: Config{Federations: []FederationEntry{
				{Name: "dup", URL: "https://a.example.com", SyncMode: "inline"},
				{Name: "dup", URL: "https://b.example.com", SyncMode: "inline"},
			}},
			wantErr: true,
			errFrag: "duplicate name",
		},
		{
			name: "empty name",
			cfg: Config{Federations: []FederationEntry{
				{Name: "", URL: "https://a.example.com", SyncMode: "inline"},
			}},
			wantErr: true,
			errFrag: "name must not be empty",
		},
		{
			name: "bidirectional sync_mode rejected (Phase 3 not implemented)",
			cfg: Config{Federations: []FederationEntry{
				{Name: "bi-peer", URL: "https://bi.example.com", SyncMode: "bidirectional"},
			}},
			wantErr: true,
			errFrag: "not yet implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errFrag != "" {
					assert.Contains(t, err.Error(), tt.errFrag)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFederationsLoadFromYAML(t *testing.T) {
	cfg, err := loadFromYAML(t, `
federations:
  - name: peer-a
    url: https://peer-a.internal:8080
    sync_mode: async
    interval: 10m
  - name: peer-b
    url: https://peer-b.internal:8080
    sync_mode: inline
`)
	require.NoError(t, err)
	require.Len(t, cfg.Federations, 2)
	assert.Equal(t, "peer-a", cfg.Federations[0].Name)
	assert.Equal(t, "async", cfg.Federations[0].SyncMode)
	assert.Equal(t, 10*time.Minute, cfg.Federations[0].Interval)
	assert.Equal(t, "peer-b", cfg.Federations[1].Name)
	assert.Equal(t, "inline", cfg.Federations[1].SyncMode)
}

func TestBrowserConfigDefaults(t *testing.T) {
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.False(t, cfg.Browser.Enabled)
	assert.Equal(t, "ibr", cfg.Browser.Binary)
	assert.Equal(t, 0, cfg.Browser.Port)
	assert.Equal(t, 3, cfg.Browser.MaxClients)
	assert.True(t, cfg.Browser.Headless)
	assert.Empty(t, cfg.Browser.AIProvider)
	assert.Empty(t, cfg.Browser.AIModel)
}

func TestBrowserConfigFromYAML(t *testing.T) {
	cfg, err := loadFromYAML(t, `
browser:
  enabled: true
  binary: /usr/local/bin/ibr
  port: 9222
  max_clients: 5
  headless: false
  ai_provider: anthropic
  ai_model: claude-sonnet-4-6
`)
	require.NoError(t, err)
	assert.True(t, cfg.Browser.Enabled)
	assert.Equal(t, "/usr/local/bin/ibr", cfg.Browser.Binary)
	assert.Equal(t, 9222, cfg.Browser.Port)
	assert.Equal(t, 5, cfg.Browser.MaxClients)
	assert.False(t, cfg.Browser.Headless)
	assert.Equal(t, "anthropic", cfg.Browser.AIProvider)
	assert.Equal(t, "claude-sonnet-4-6", cfg.Browser.AIModel)
}
