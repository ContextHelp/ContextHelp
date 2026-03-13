package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type Driver struct {
	db         *sql.DB
	connStr    string
	objects    *ObjectStore
	entities   *EntityStore
	edges      *EdgeStore
	jobs       *JobStore
	pipelines  *PipelineStore
	steps      *StepStore
	registries *RegistryStore
	reminders  *ReminderStore
	feeds      *FeedStore
	feedItems  *FeedItemStore
	batches    *BatchStore
}

func New(connStr string) (*Driver, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	d := &Driver{db: db, connStr: connStr}
	d.objects = &ObjectStore{db: db}
	d.entities = &EntityStore{db: db}
	d.edges = &EdgeStore{db: db}
	d.jobs = &JobStore{db: db}
	d.pipelines = &PipelineStore{db: db}
	d.steps = &StepStore{db: db}
	d.registries = &RegistryStore{db: db}
	d.reminders = &ReminderStore{db: db}
	d.feeds = &FeedStore{db: db}
	d.feedItems = &FeedItemStore{db: db}
	d.batches = &BatchStore{db: db}
	return d, nil
}

func (d *Driver) Init(ctx context.Context) error {
	if err := d.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return d.Migrate(ctx)
}

func (d *Driver) Close(_ context.Context) error {
	return d.db.Close()
}

func (d *Driver) Objects() storage.ObjectStore      { return d.objects }
func (d *Driver) Entities() storage.EntityStore     { return d.entities }
func (d *Driver) Edges() storage.EdgeStore          { return d.edges }
func (d *Driver) Jobs() storage.JobStore            { return d.jobs }
func (d *Driver) Pipelines() storage.PipelineStore  { return d.pipelines }
func (d *Driver) Steps() storage.StepStore          { return d.steps }
func (d *Driver) Registries() storage.RegistryStore { return d.registries }
func (d *Driver) Reminders() storage.ReminderStore  { return d.reminders }
func (d *Driver) Feeds() storage.FeedStore          { return d.feeds }
func (d *Driver) FeedItems() storage.FeedItemStore  { return d.feedItems }
func (d *Driver) Batches() storage.BatchStore          { return d.batches }
func (d *Driver) Detectors() storage.DetectorStore     { panic("postgres: Detectors not implemented") }

func (d *Driver) Health(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *Driver) DB() *sql.DB {
	return d.db
}
