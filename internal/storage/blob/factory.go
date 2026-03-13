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
	case "stub":
		return stub.New(), nil
	default:
		return nil, fmt.Errorf("blob: unknown backend %q (valid: local, s3, stub)", backend)
	}
}
