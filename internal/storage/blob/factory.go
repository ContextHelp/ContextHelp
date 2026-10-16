package blob

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	s3store "github.com/ideacrafterslabs/ctxt/internal/storage/blob/s3"
)

const (
	// backendGarage is the storage.blob.backend value for a Garage server.
	backendGarage = "garage"
	// garageRegion is the SigV4 region Garage answers to by default.
	garageRegion = "garage"
)

// New creates the BlobStore that storage.blob configures. It is the one
// constructor for the blob store a storage driver writes through.
//
// The backend and its required fields are checked by
// config.BlobConfig.Validate, the same check `ctxt config validate`
// reports, so startup and validate agree on what is valid.
func New(cfg config.BlobConfig) (storage.BlobStore, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("blob: %w", err)
	}

	switch cfg.Backend {
	case "s3":
		return s3store.New(cfg.S3)
	case backendGarage:
		return s3store.New(garageDefaults(cfg.S3))
	default: // "" or "local"
		if cfg.Local.Path == "" {
			return nil, fmt.Errorf("blob: local backend requires storage.blob.local.path")
		}
		return local.New(cfg.Local.Path)
	}
}

// garageDefaults applies Garage-friendly defaults onto a BlobS3Config:
// path-style addressing (Garage's recommended layout), and a "garage" region
// when none is set (Garage requires SigV4 with a region; "garage" matches the
// default in the upstream quick-start guide).
//
// Endpoint must be set explicitly — there is no sensible default for it.
func garageDefaults(in config.BlobS3Config) config.BlobS3Config {
	if in.Endpoint == "" {
		// Surface as a config error via the s3 store so the user gets a
		// clear "endpoint is required" message instead of a network failure.
		// We still return the value to let s3store.New raise the canonical
		// error path.
		return in
	}
	out := in
	out.UsePathStyle = true
	if out.Region == "" {
		out.Region = garageRegion
	}
	return out
}
