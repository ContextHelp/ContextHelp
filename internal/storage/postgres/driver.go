package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	blobstub "github.com/ideacrafterslabs/ctxt/internal/storage/blob/stub"
)

// DefaultVectorDimension is used when no dimension is specified before Init.
// Matches OpenAI text-embedding-3-small and the SQLite driver default.
const DefaultVectorDimension = 1536

type Driver struct {
	db              *sql.DB
	connStr         string
	vectorDimension int
	caps            *pgCaps
	objects         *ObjectStore
	entities        *EntityStore
	edges           *EdgeStore
	jobs            *JobStore
	pipelines       *PipelineStore
	steps           *StepStore
	registries      *RegistryStore
	reminders       *ReminderStore
	feeds           *FeedStore
	feedItems       *FeedItemStore
	batches         *BatchStore
	detectors       *DetectorStore
	blobs           storage.BlobStore
	proximity       *ProximityStore
	watches         *WatchStore
	aliases         *AliasStore
	auditLog        *AuditStore
	attachments     *AttachmentStore
	resurfacing     *ResurfacingQueueStore
	entitlements    *EntitlementStore
	metering        *MeteringStore
	savedSearches   *SavedSearchStore
	searchHistory   *SearchHistoryStore
}

func New(connStr string) (*Driver, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	d := &Driver{db: db, connStr: connStr, vectorDimension: DefaultVectorDimension}
	d.caps = &pgCaps{}
	d.objects = &ObjectStore{db: db, caps: d.caps}
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
	d.blobs = blobstub.New()
	d.proximity = &ProximityStore{db: db}
	d.watches = &WatchStore{db: db}
	d.aliases = &AliasStore{db: db}
	d.auditLog = &AuditStore{db: db}
	d.attachments = &AttachmentStore{}
	d.resurfacing = &ResurfacingQueueStore{}
	d.entitlements = &EntitlementStore{db: db}
	d.metering = &MeteringStore{}
	d.savedSearches = &SavedSearchStore{}
	d.searchHistory = &SearchHistoryStore{}
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

func (d *Driver) Objects() storage.ObjectStore               { return d.objects }
func (d *Driver) Entities() storage.EntityStore              { return d.entities }
func (d *Driver) Edges() storage.EdgeStore                   { return d.edges }
func (d *Driver) Jobs() storage.JobStore                     { return d.jobs }
func (d *Driver) Pipelines() storage.PipelineStore           { return d.pipelines }
func (d *Driver) Steps() storage.StepStore                   { return d.steps }
func (d *Driver) Registries() storage.RegistryStore          { return d.registries }
func (d *Driver) Reminders() storage.ReminderStore           { return d.reminders }
func (d *Driver) Feeds() storage.FeedStore                   { return d.feeds }
func (d *Driver) FeedItems() storage.FeedItemStore           { return d.feedItems }
func (d *Driver) Batches() storage.BatchStore                { return d.batches }
func (d *Driver) Detectors() storage.DetectorStore           { return d.detectors }
func (d *Driver) Blobs() storage.BlobStore                   { return d.blobs }
func (d *Driver) Proximity() storage.ProximityStore          { return d.proximity }
func (d *Driver) Watches() storage.WatchStore                { return d.watches }
func (d *Driver) Aliases() storage.AliasStore                { return d.aliases }
func (d *Driver) AuditLog() storage.AuditStore               { return d.auditLog }
func (d *Driver) Attachments() storage.AttachmentStore       { return d.attachments }
func (d *Driver) Resurfacing() storage.ResurfacingQueueStore { return d.resurfacing }
func (d *Driver) Entitlements() storage.EntitlementStore     { return d.entitlements }
func (d *Driver) Metering() storage.MeteringStore            { return d.metering }
func (d *Driver) SavedSearches() storage.SavedSearchStore    { return d.savedSearches }
func (d *Driver) SearchHistory() storage.SearchHistoryStore  { return d.searchHistory }
func (d *Driver) Watermarks() storage.WatermarkStore         { return &watermarkStore{db: d.db} }

// SetBlobs allows injection of a custom BlobStore implementation.
func (d *Driver) SetBlobs(bs storage.BlobStore) { d.blobs = bs }

// SetVectorDimension reconfigures the pgvector dimension before Init is
// called; the objects.embedding typmod and its ANN index are created with
// this dimension at migration time. Mirrors the SQLite driver contract:
// calling it after migration has no effect on the already-created schema.
func (d *Driver) SetVectorDimension(dim int) {
	d.vectorDimension = dim
}

// SQLDialect implements search.dialectDetector; signals Postgres SQL dialect.
func (d *Driver) SQLDialect() string { return "postgres" }

func (d *Driver) Health(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *Driver) DB() *sql.DB {
	return d.db
}

// pgCaps carries server capabilities detected at migration time, shared by
// the stores that adapt query strategy to them.
type pgCaps struct {
	// iterativeScan is true when the vector extension supports
	// hnsw.iterative_scan (>= 0.8.0). pgvector applies WHERE after HNSW
	// traversal, so without iterative scans a filtered KNN query can
	// under-return below LIMIT even when matches exist.
	iterativeScan bool
}

// pgvectorSupportsIterativeScan reports whether the given pgvector extension
// version (e.g. "0.8.6") supports the hnsw.iterative_scan GUC, introduced in
// 0.8.0. Unknown or unparsable versions report false: SET LOCAL of an
// unrecognized GUC errors, so the guard must fail closed.
func pgvectorSupportsIterativeScan(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 0 || minor >= 8
}
