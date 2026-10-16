package cmd

import (
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
}
