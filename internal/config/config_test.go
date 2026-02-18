package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
