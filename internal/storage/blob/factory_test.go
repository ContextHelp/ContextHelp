package blob

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFactoryLocal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "local",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new local: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryStub(t *testing.T) {
	cfg := config.BlobConfig{Backend: "stub"}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new stub: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryUnknown(t *testing.T) {
	cfg := config.BlobConfig{Backend: "unknown"}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestFactoryDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new default: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryGarageDefaultsPathStyleAndRegion(t *testing.T) {
	cfg := config.BlobConfig{
		Backend: "garage",
		S3: config.BlobS3Config{
			Endpoint:  "http://localhost:3900",
			Bucket:    "test",
			AccessKey: "GK",
			SecretKey: "SK",
		},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new garage: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryGarageRequiresEndpoint(t *testing.T) {
	cfg := config.BlobConfig{
		Backend: "garage",
		S3:      config.BlobS3Config{Bucket: "test"},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("expected error: garage backend requires endpoint")
	}
}

func TestGarageDefaultsAppliesPathStyleAndRegion(t *testing.T) {
	in := config.BlobS3Config{
		Endpoint: "http://localhost:3900",
		Bucket:   "test",
	}
	got := garageDefaults(in)
	if !got.UsePathStyle {
		t.Error("garageDefaults should force UsePathStyle=true")
	}
	if got.Region != "garage" {
		t.Errorf("garageDefaults should default Region to %q, got %q", "garage", got.Region)
	}
}

func TestGarageDefaultsPreservesExplicitRegion(t *testing.T) {
	in := config.BlobS3Config{
		Endpoint: "http://localhost:3900",
		Region:   "eu-west-1",
		Bucket:   "test",
	}
	got := garageDefaults(in)
	if got.Region != "eu-west-1" {
		t.Errorf("explicit region should win, got %q", got.Region)
	}
}
