package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Driver implements storage.StorageDriver for SQLite.
type Driver struct {
	db   *sql.DB
	path string
	// skipMigrateSnapshot turns off the snapshot Migrate writes before
	// applying pending migrations (WithMigrateSnapshot(false)).
	skipMigrateSnapshot bool
	objects             *ObjectStore
	entities            *EntityStore
	edges               *EdgeStore
	jobs                *JobStore
	pipelines           *PipelineStore
	steps               *StepStore
	registries          *RegistryStore
	reminders           *ReminderStore
	feeds               *FeedStore
	feedItems           *FeedItemStore
	batches             *BatchStore
	detectors           *DetectorStore
	blobs               storage.BlobStore
	proximity           *ProximityStore
	watches             *WatchStore
	attachments         *AttachmentStore
	resurfacing         *ResurfacingQueueStore
	entitlements        *entitlementStore
	metering            *MeteringStore
	savedSearches       *SavedSearchStore
	searchHistory       *SearchHistoryStore
}

// ErrNoBlobStore is returned by New when it is given no blob store.
// Content above storage.blob.threshold is written to that store, so a
// driver without one would lose it.
var ErrNoBlobStore = errors.New("sqlite: blob store is required")

// New creates a new SQLite driver for the given database path. blobs is
// where externalized content lives: build it with blob.New from
// storage.blob, or open both through storageutil.Open. Before Init
// applies pending migrations to an existing database it snapshots the
// database into SnapshotDir(path); WithMigrateSnapshot(false) turns
// that off.
func New(path string, blobs storage.BlobStore, opts ...Option) (*Driver, error) {
	if blobs == nil {
		return nil, ErrNoBlobStore
	}
	sqlite_vec.Auto()
	// Encode pragmas in the DSN so every pooled connection inherits them.
	// go-sqlite3 requires file: URI prefix for query parameters to be
	// parsed as connection pragmas (bare path+? is treated as filename).
	//
	// _txlock=immediate: every transaction here writes, and a deferred
	// one that reads first (the FTS5 delete that opens Update, Delete and
	// Reproject reads the content row before writing the index) holds a
	// read snapshot; if another writer commits meanwhile, the upgrade to a
	// write lock fails with SQLITE_BUSY at once, bypassing busy_timeout.
	// BEGIN IMMEDIATE takes the write lock up front, so concurrent writers
	// queue on busy_timeout instead.
	dsn := "file:" + path + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=ON&_cache_size=-64000&_busy_timeout=5000&_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// sql.Open is lazy: the file is first opened by whatever statement runs
	// next. Open it here, so a path SQLite cannot open (a parent that is a
	// file, a missing directory, no permission) is reported against that
	// path instead of surfacing from the capability probe as a missing
	// engine feature.
	if err := db.PingContext(context.Background()); err != nil {
		openErr := fmt.Errorf("open sqlite database %s: %w", path, err)
		if cerr := db.Close(); cerr != nil {
			return nil, fmt.Errorf("%w; close: %w", openErr, cerr)
		}
		return nil, openErr
	}

	// Probe engine capabilities before any migration runs, so a degraded build
	// is diagnosed here rather than failing mid-schema on a virtual table.
	if err := probeCapabilities(context.Background(), db); err != nil {
		if cerr := db.Close(); cerr != nil {
			return nil, fmt.Errorf("%w; close: %w", err, cerr)
		}
		return nil, err
	}

	d := &Driver{db: db, path: path}
	for _, o := range opts {
		o(d)
	}
	d.objects = &ObjectStore{db: db}
	d.objects.emb = d.Embeddings()
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
	d.detectors = &DetectorStore{db: db}
	d.blobs = blobs
	d.proximity = &ProximityStore{db: db}
	d.watches = &WatchStore{db: db}
	d.attachments = &AttachmentStore{db: db}
	d.resurfacing = &ResurfacingQueueStore{db: db}
	d.entitlements = &entitlementStore{db: db}
	d.metering = &MeteringStore{db: db}
	d.savedSearches = &SavedSearchStore{db: db}
	d.searchHistory = &SearchHistoryStore{db: db}
	return d, nil
}

func (d *Driver) Init(ctx context.Context) error {
	return d.Migrate(ctx)
}

func (d *Driver) Close(_ context.Context) error {
	return d.db.Close()
}

func (d *Driver) Objects() storage.ObjectStore                { return d.objects }
func (d *Driver) Entities() storage.EntityStore               { return d.entities }
func (d *Driver) Edges() storage.EdgeStore                    { return d.edges }
func (d *Driver) Jobs() storage.JobStore                      { return d.jobs }
func (d *Driver) Pipelines() storage.PipelineStore            { return d.pipelines }
func (d *Driver) Steps() storage.StepStore                    { return d.steps }
func (d *Driver) Registries() storage.RegistryStore           { return d.registries }
func (d *Driver) Reminders() storage.ReminderStore            { return d.reminders }
func (d *Driver) Feeds() storage.FeedStore                    { return d.feeds }
func (d *Driver) FeedItems() storage.FeedItemStore            { return d.feedItems }
func (d *Driver) Batches() storage.BatchStore                 { return d.batches }
func (d *Driver) Detectors() storage.DetectorStore            { return d.detectors }
func (d *Driver) Blobs() storage.BlobStore                    { return d.blobs }
func (d *Driver) Proximity() storage.ProximityStore           { return d.proximity }
func (d *Driver) Watches() storage.WatchStore                 { return d.watches }
func (d *Driver) Aliases() storage.AliasStore                 { return &aliasStore{db: d.db} }
func (d *Driver) AuditLog() storage.AuditStore                { return &auditStore{db: d.db} }
func (d *Driver) Attachments() storage.AttachmentStore        { return d.attachments }
func (d *Driver) Resurfacing() storage.ResurfacingQueueStore  { return d.resurfacing }
func (d *Driver) Entitlements() storage.EntitlementStore      { return d.entitlements }
func (d *Driver) Metering() storage.MeteringStore             { return d.metering }
func (d *Driver) SavedSearches() storage.SavedSearchStore     { return d.savedSearches }
func (d *Driver) SearchHistory() storage.SearchHistoryStore   { return d.searchHistory }
func (d *Driver) Watermarks() storage.WatermarkStore          { return &watermarkStore{db: d.db} }
func (d *Driver) PendingEntities() storage.PendingEntityStore { return &pendingEntityStore{db: d.db} }
func (d *Driver) UISessions() storage.UISessionStore          { return &uiSessionStore{db: d.db} }
func (d *Driver) Auth() storage.AuthStore                     { return &authStore{db: d.db} }
func (d *Driver) Sharing() storage.SharingStore               { return &sharingStore{db: d.db} }

func (d *Driver) Health(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// DB returns the underlying database connection for use by other packages (e.g. search).
func (d *Driver) DB() *sql.DB {
	return d.db
}
