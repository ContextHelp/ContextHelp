package cmd

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
)

// The direct-store pipeline registry gets the driver's blob store and
// storage.blob.threshold, as serve's does, so externalize_content is
// available there.
func TestNewServiceWiresBlobStore(t *testing.T) {
	db := setupTestDB(t)
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{
		Storage: config.StorageConfig{
			Type: "sqlite",
			Path: filepath.Join(filepath.Dir(db.ConfigPath), "test.db"),
			Blob: config.BlobConfig{Backend: "local", Threshold: 4096, Local: config.BlobLocalConfig{Path: t.TempDir()}},
		},
	}

	opts := registryBuildOpts(db.Driver)
	if opts.BlobStore == nil || opts.BlobStore != db.Driver.Blobs() {
		t.Errorf("BuildOpts.BlobStore = %v, want the driver's blob store", opts.BlobStore)
	}
	if opts.BlobThreshold != 4096 {
		t.Errorf("BuildOpts.BlobThreshold = %d, want storage.blob.threshold 4096", opts.BlobThreshold)
	}
	if !builtins.CapabilitiesFromOpts(opts)["blob-externalize"] {
		t.Error("registry lacks the blob-externalize capability; externalize_content would be pruned")
	}
	if opts.Audit == nil {
		t.Error("BuildOpts lost the embedding write path wiring")
	}
	// A nil Factory would leave media steps without real providers.
	if opts.Factory == nil {
		t.Error("BuildOpts.Factory is nil; the direct-store registry needs the configured providers")
	}
}

// A read-only open builds no provider factory, so a bad providers.* config
// (a named backend that cannot run) cannot break a read command and the
// read path pays no provider probing. A write open keeps the factory and
// fails on that config.
func TestReadOpenBuildsNoProviderFactory(t *testing.T) {
	db := setupTestDB(t)
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{
		Storage: config.StorageConfig{
			Type: "sqlite",
			Path: filepath.Join(filepath.Dir(db.ConfigPath), "test.db"),
			Blob: config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: t.TempDir()}},
		},
		// whisper-cli with no model cannot run: a configuration error.
		Providers: config.ProvidersConfig{Transcription: config.ProviderBackendConfig{Backend: "whisper-cli"}},
	}

	read := registryBuildOptsTo(db.Driver, io.Discard, true)
	if read.Factory != nil {
		t.Error("a read-only open built a provider factory")
	}
	if _, err := builtins.ConfiguredRegistryWithOpts(read); err != nil {
		t.Errorf("read open: registry build failed on a media provider config: %v", err)
	}

	write := registryBuildOptsTo(db.Driver, io.Discard, false)
	if write.Factory == nil {
		t.Fatal("a write open built no provider factory")
	}
	if _, err := builtins.ConfiguredRegistryWithOpts(write); err == nil {
		t.Error("write open: registry build accepted a named backend that cannot run")
	}
}
