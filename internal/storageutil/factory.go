package storageutil

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// NewDriver creates a StorageDriver from a type string and path.
func NewDriver(typ, path string) (storage.StorageDriver, error) {
	switch typ {
	case "sqlite":
		return sqlite.New(path)
	case "postgres":
		return postgres.New(path)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}
