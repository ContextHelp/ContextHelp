package blob

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	s3store "github.com/ideacrafterslabs/ctxt/internal/storage/blob/s3"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/stub"
)

// New creates a BlobStore based on the configuration.
func New(cfg config.BlobConfig) (storage.BlobStore, error) {
	backend := cfg.Backend
	if backend == "" {
		backend = "local"
	}

	switch backend {
	case "local":
		return local.New(cfg.Local.Path)
	case "s3":
		return s3store.New(cfg.S3)
	case "garage":
		if cfg.S3.Endpoint == "" {
			return nil, fmt.Errorf("blob: garage backend requires s3.endpoint (e.g. http://localhost:3900)")
		}
		return s3store.New(garageDefaults(cfg.S3))
	case "stub":
		return stub.New(), nil
	default:
		return nil, fmt.Errorf("blob: unknown backend %q (valid: local, s3, garage, stub)", backend)
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
		out.Region = "garage"
	}
	return out
}
