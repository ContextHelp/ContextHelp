package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Driver implements storage.StorageDriver for SQLite.
type Driver struct {
	db       *sql.DB
	path     string
	objects  *ObjectStore
	entities *EntityStore
	edges    *EdgeStore
	jobs     *JobStore
}

// New creates a new SQLite driver for the given database path.
func New(path string) (*Driver, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Set pragmas for performance and correctness.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA cache_size=-64000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %s: %w", pragma, err)
		}
	}

	d := &Driver{db: db, path: path}
	d.objects = &ObjectStore{db: db}
	d.entities = &EntityStore{db: db}
	d.edges = &EdgeStore{db: db}
	d.jobs = &JobStore{db: db}
	return d, nil
}

func (d *Driver) Init(ctx context.Context) error {
	return d.Migrate(ctx)
}

func (d *Driver) Close(_ context.Context) error {
	return d.db.Close()
}

func (d *Driver) Objects() storage.ObjectStore   { return d.objects }
func (d *Driver) Entities() storage.EntityStore { return d.entities }
func (d *Driver) Edges() storage.EdgeStore       { return d.edges }
func (d *Driver) Jobs() storage.JobStore         { return d.jobs }

func (d *Driver) Health(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// DB returns the underlying database connection for use by other packages (e.g. search).
func (d *Driver) DB() *sql.DB {
	return d.db
}
