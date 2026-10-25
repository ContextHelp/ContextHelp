// Package service holds the business operations behind dpkms's REST and
// gRPC servers and ctxt's local commands: analyze and capture, search,
// inbox, links, batches, backup and restore, and re-analysis.
package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/composition"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/ranking"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"hop.top/kit/go/runtime/domain"
)

// Service coordinates all business operations.
type Service struct {
	// store is the unscoped driver. Nothing reads it directly: request
	// paths go through Store(ctx), which scopes it to the caller's
	// viewer, and the constructor hands it to the components that run
	// as the system (step discovery and install, the pipeline domain
	// service).
	store     storage.StorageDriver
	Queue     *jobs.Queue
	Pipes     pipeline.Registry
	Search    *search.Engine
	Discovery *steps.StepDiscovery
	// Installer installs and removes steps under steps.path.
	Installer *steps.Installer
	Bus       events.Bus
	Cfg       config.Config

	// pipelineSvc delegates pipeline CRUD to kit/runtime/domain so
	// pre_validated / pre_persisted veto seams fire on every
	// lifecycle op. The service is unexported because the public API
	// surface (CreatePipeline / DeletePipeline / ArchivePipeline /
	// UnarchivePipeline) routes through it internally — callers keep
	// the same method shapes.
	pipelineSvc *domain.Service[storage.Pipeline]

	// policyPub is the domain.EventPublisher attached to pipelineSvc.
	// nil in tests and outside dpkms serve; non-nil when the daemon
	// has wired kit/runtime/policy on the bus.
	policyPub domain.EventPublisher

	// registryTokens is where stored registry tokens are read from
	// (WithRegistryTokens); registryAuth applies them to every request
	// to a configured registry. Both nil: registry requests go
	// unauthenticated.
	registryTokens registrysync.TokenSource
	registryAuth   *registrysync.Credentials

	// viewers resolves each non-owner principal's viewer from the
	// store's sharing registry (ADR-080 §4); the transports call it
	// after authentication. nil without a store.
	viewers *sharing.Resolver
}

// Option configures a Service at construction time. Existing call
// sites that pass only a config value keep working (the variadic
// cfg arg is preserved); new behaviors (policy publisher, etc.) ride
// in via Option closures.
type Option func(*Service)

// WithPolicyPublisher attaches a domain.EventPublisher to the
// internal domain.Service[Pipeline] so policy.Engine can veto
// kit.runtime.entity.pre_persisted events. The daemon constructs the
// publisher from internal/policy.Init.
func WithPolicyPublisher(p domain.EventPublisher) Option {
	return func(s *Service) { s.policyPub = p }
}

// WithReservedStepNames refuses step installs under names reserved
// reports (the built-in steps: builtins.IsStepName).
func WithReservedStepNames(reserved func(string) bool) Option {
	return func(s *Service) { s.Installer.Reserved = reserved }
}

// WithRegistryTokens makes every request to a configured registry
// (manifest fetch, entity sync, entitlements, entity pull, step download,
// bookmark search) carry that registry's stored token per its auth
// config. ctxt passes the OS keychain store.
func WithRegistryTokens(tokens registrysync.TokenSource) Option {
	return func(s *Service) { s.registryTokens = tokens }
}

// New creates a new service instance.
func New(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string, bus events.Bus, cfg ...config.Config) *Service {
	return NewWithOptions(store, queue, pipes, engine, stepsPath, bus, nil, cfg...)
}

// NewWithOptions is the explicit constructor used by callers that need
// to wire optional behaviors (e.g. policy publisher). Existing callers
// of New continue to work unchanged.
func NewWithOptions(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string, bus events.Bus, opts []Option, cfg ...config.Config) *Service {
	discovery := steps.NewStepDiscovery(store)

	if bus == nil {
		bus = events.NewLocalBus()
	}

	var c config.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	s := &Service{
		store:     store,
		Queue:     queue,
		Pipes:     pipes,
		Search:    engine,
		Discovery: discovery,
		Installer: &steps.Installer{Dir: stepsPath, Store: store, Discovery: discovery},
		Bus:       bus,
		Cfg:       c,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.registryTokens != nil {
		s.registryAuth = registrysync.NewCredentials(c.Registries, s.registryTokens)
		discovery.WithCredentials(s.registryAuth)
	}
	if store != nil {
		s.pipelineSvc = newPipelineService(store.Pipelines(), s.policyPub)
		s.viewers = sharing.NewResolver(store.Sharing()).WithRegistry(store.Auth())
	}
	return s
}

// Viewers is the resolver the transports give each authenticated
// principal its viewer through: the owner is unrestricted, anyone else
// gets the viewer compiled from its grants over this service's store.
// nil for a service with no store, which resolves every non-owner to
// the zero viewer.
func (s *Service) Viewers() *sharing.Resolver { return s.viewers }

// Store returns the storage driver as the caller of ctx may see it:
// the driver itself for an unrestricted viewer; for a restricted one a
// driver whose reads answer through the viewer's predicate (hidden
// rows are absent, owner-only stores refuse) and whose every released
// object is rendered (ADR-080 §5) and recorded in the request's ledger
// (§8) before a handler can touch it; and a driver whose every read
// fails closed for a context no transport attached a viewer to, which
// is a transport bug and stays loud (§4). Every read the service
// performs on a caller's behalf goes through it; callers that act as
// the system (pipeline, jobs, housekeeping, the CLI at the owner's
// terminal) attach sharing.Unrestricted to their context, and say so.
func (s *Service) Store(ctx context.Context) storage.StorageDriver {
	return sharing.Scoped(ctx, s.store, s.provenance)
}

// provenance adapts the pipeline registry to the render layer: the
// step contracts of the pipeline named name, with or without its @vN
// suffix, so the derived rule can tell what each stored field was
// computed from.
func (s *Service) provenance(name string) ([]sharing.StepContract, bool) {
	if s.Pipes == nil || name == "" {
		return nil, false
	}
	p, err := s.Pipes.Get(name)
	if err != nil {
		if base, _, ok := strings.Cut(name, "@"); !ok || base == name {
			return nil, false
		} else if p, err = s.Pipes.Get(base); err != nil {
			return nil, false
		}
	}
	steps := make([]sharing.StepContract, 0, len(p.Steps))
	for _, st := range p.Steps {
		c := st.Contract()
		steps = append(steps, sharing.StepContract{Name: st.Name(), Requires: c.Requires, Produces: c.Produces})
	}
	return steps, true
}

// viewer is the viewer attached to ctx as storage sees it: nil when
// none was attached, so every read refuses.
func (s *Service) viewer(ctx context.Context) storage.Viewer {
	v, ok := sharing.FromContext(ctx)
	if !ok {
		return nil
	}
	return v
}

// ErrPipelineNotFound is returned by Analyze/Enqueue when the resolved
// pipeline name does not exist in the registry. Callers (HTTP handler,
// CLI) surface this as a 422 / non-zero exit rather than silently
// dropping the job. Previously a job was enqueued referencing a
// non-existent pipeline name (e.g. when --type document picked up the
// fallback "text.short" or any other path) — the worker would then fail
// to dispatch silently and the data would be lost without any signal to
// the operator. Failing loudly at enqueue time keeps the queue honest.
var ErrPipelineNotFound = errors.New("pipeline not found")

// pipelineExists reports whether a pipeline name resolves. Pipelines live
// in two places: the in-memory registry (built-ins, registered at
// startup) and the pipelines store (user-created via CreatePipeline,
// which persists only — it never mutates the registry). A check against
// the registry alone rejects every user-created pipeline, so both
// sources must be consulted for the answer to be correct.
func (s *Service) pipelineExists(ctx context.Context, name string) bool {
	if name == "" {
		return false
	}
	if _, err := s.Pipes.Get(name); err == nil {
		return true
	}
	if s.store == nil {
		return false
	}
	p, err := s.Store(ctx).Pipelines().Get(ctx, name)
	return err == nil && p != nil
}

// Analyze enqueues a content analysis job and returns the job ID.
// When req.Raw is true, skips AI enrichment and stores the object immediately
// with Status "raw"; returns the object ID (not a job ID).
//
// When the resolved pipeline does not exist in the registry, Analyze
// returns an error wrapping ErrPipelineNotFound rather than enqueueing a
// job that the worker would silently fail to dispatch. The raw path is
// exempt because it bypasses the pipeline entirely and persists the
// object directly.
func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (string, error) {
	now := time.Now().Truncate(time.Second)

	// Auto-detect type if it's "text" but content looks like a URL.
	detectedType := req.Type
	if detectedType == "text" && strings.HasPrefix(strings.TrimSpace(req.Content), "http") {
		detectedType = "url"
	}

	jobSource := req.Source
	if detectedType == "url" {
		jobSource = strings.TrimSpace(req.Content)
	}

	// Raw mode: bypass pipeline entirely; persist as-is.
	if req.Raw {
		return s.analyzeRaw(ctx, req, detectedType, jobSource, now)
	}

	// A replayed submission (same client-generated idempotency key) must
	// resolve to the job the first attempt enqueued — a response lost in
	// transit after the enqueue would otherwise duplicate the payload.
	if jobID, hit, err := s.dedupeOnIdempotencyKey(ctx, req.IdempotencyKey); err != nil {
		return "", fmt.Errorf("analyze: %w", err)
	} else if hit {
		return jobID, nil
	}

	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.detectPipeline(jobSource, req.Type, req.Content)
	}

	// validate the resolved pipeline exists in the registry. Without
	// this, a job referencing a non-existent pipeline (e.g. `--type document`
	// when no document.* pipeline is registered) would be enqueued and the
	// worker would silently fail to dispatch — data lost, no signal.
	if !s.pipelineExists(ctx, pipelineName) {
		return "", fmt.Errorf("analyze: %w: type=%q pipeline=%q (no pipeline registered for this content type)",
			ErrPipelineNotFound, req.Type, pipelineName)
	}

	// Duplicate detection (exact match only at analyze time; embeddings not yet computed).
	if !req.Force {
		if existingID, drop, err := s.analyzeDuplicate(ctx, req, jobSource); err != nil {
			return "", err
		} else if drop {
			return existingID, nil
		}
	}

	jobType := "ingest:" + detectedType
	if req.NoFanout {
		jobType += ":nofanout"
	}

	job := &storage.Job{
		ID:           uuid.New().String(),
		Type:         jobType,
		Status:       storage.JobPending,
		Payload:      req.Content,
		Pipeline:     pipelineName,
		Source:       jobSource,
		MaxRetries:   3,
		CreatedAt:    now,
		UpdatedAt:    now,
		UserMentions: req.Mentions, // forwarded to draft.Mentions in worker.
		UserHints:    req.Hints,    // forwarded to draft.Tags (Source:"user") in worker.
		UserProfile:  req.Profile,  // forwarded to draft.ProfileID in worker.
		UserNote:     req.Note,     // forwarded to draft.InboxNote in worker.

		IdempotencyKey: req.IdempotencyKey,
	}

	if err := s.Queue.Enqueue(ctx, job); err != nil {
		// Lost the insert race against a concurrent replay: the partial
		// unique index on the key rejected this row, so the surviving job
		// is the answer.
		if jobID, hit, lerr := s.dedupeOnIdempotencyKey(ctx, req.IdempotencyKey); lerr == nil && hit {
			return jobID, nil
		}
		return "", err
	}
	if ev, err := events.NewEvent("service.analyze", string(events.TopicJobEnqueued), job); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

// analyzeRaw persists req's content as a raw object, bypassing the
// pipeline, and returns the object ID.
func (s *Service) analyzeRaw(ctx context.Context, req AnalyzeRequest, detectedType, jobSource string, now time.Time) (string, error) {
	obj := &storage.KnowledgeObject{
		ID:          uuid.New().String(),
		Type:        detectedType,
		RawContent:  req.Content,
		Source:      jobSource,
		SourceKey:   req.SourceKey,
		ContentHash: storageutil.ContentHash(req.Content, jobSource),
		Status:      "raw",
		CreatedAt:   now,
		UpdatedAt:   now,
		Mentions:    mentions.ParseSlice(req.Mentions),
		// caller-asserted hints become Tag{Source:"user"} on the
		// raw path too, so capture-with-hints partitioning works without
		// the pipeline running.
		Tags: userHintsToTags(req.Hints),
		// caller-asserted profile and note flow directly onto
		// the KnowledgeObject so partitioning + audit-note capture
		// work even on the raw path (no pipeline to plumb through).
		ProfileID: req.Profile,
		InboxNote: req.Note,
	}
	if err := s.Store(ctx).Objects().Create(ctx, obj); err != nil {
		return "", fmt.Errorf("analyze raw: store: %w", err)
	}
	// caller-asserted mentions become thin entity rows + mention
	// edges even on the raw path so partitioning by @client/@project
	// works without the pipeline running.
	if err := s.writeUserMentionEdges(ctx, obj); err != nil {
		return "", fmt.Errorf("analyze raw: user mentions: %w", err)
	}
	if ev, err := events.NewEvent("service.analyze", string(events.TopicObjectRawStored), obj); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return obj.ID, nil
}

// analyzeDuplicate runs Analyze's exact-match duplicate check. drop
// reports that the duplicates policy drops the submission, whose answer
// is then the existing object's ID; otherwise Analyze goes on and
// enqueues it.
func (s *Service) analyzeDuplicate(ctx context.Context, req AnalyzeRequest, jobSource string) (existingID string, drop bool, err error) {
	hashForDedup := req.KnownHash
	if hashForDedup == "" && s.Cfg.Duplicates.CheckExact {
		hashForDedup = storageutil.ContentHash(req.Content, jobSource)
	}
	dup, err := s.checkDuplicates(ctx, hashForDedup, req.SourceKey, req.Profile, storage.VectorQuery{}, s.Cfg.Duplicates)
	if err != nil {
		return "", false, fmt.Errorf("analyze: duplicate check: %w", err)
	}
	if dup != nil {
		s.logDedupDecision(ctx, dup, s.Cfg.Duplicates.Policy)
		switch s.Cfg.Duplicates.Policy {
		case "drop":
			return dup.Existing.ID, true, nil
		case "warn":
			fmt.Fprintf(os.Stderr, "warning: duplicate detected (%s): existing object %s\n",
				dup.Kind, dup.Existing.ID)
		case "keep":
			// Fall through silently.
		}
	}
	return "", false, nil
}

// userHintsToTags converts caller-asserted hint strings to the
// Tag form KnowledgeObject.Tags expects, with Source:"user" so the
// auto-tagger merge step can identify and preserve them. Empty / blank
// hints are dropped — `--hint ""` shouldn't store a blank Tag.
func userHintsToTags(hints []string) []storage.Tag {
	if len(hints) == 0 {
		return nil
	}
	out := make([]storage.Tag, 0, len(hints))
	for _, h := range hints {
		label := strings.TrimSpace(h)
		if label == "" {
			continue
		}
		out = append(out, storage.Tag{
			Label:  label,
			Source: "user",
			Weight: 1.0,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// writeUserMentionEdges persists thin entity rows and the object → entity
// mentions edges for the caller-asserted mentions on obj. Mirrors the
// entity_resolver pipeline step but runs at analyze-time so even Raw-mode
// captures land partitioning data. Idempotent: thin upsert on entities,
// and the store keeps a mentions edge already there for the pair. The
// edges name each entity by storage.MentionTarget, as every mention-edge
// writer does. The raw path runs no pipeline, so obj.Graph holds no
// entity_mention node and each edge's provenance is the mentions field
// alone.
func (s *Service) writeUserMentionEdges(ctx context.Context, obj *storage.KnowledgeObject) error {
	if obj == nil || len(obj.Mentions) == 0 || obj.ID == "" {
		return nil
	}
	now := time.Now().UTC()
	for _, mention := range obj.Mentions {
		slug := storage.MentionSlug(mention)
		if slug == "" {
			continue
		}
		ns := strings.SplitN(slug, "/", 2)[0]
		entity := &storage.Entity{
			Slug:          slug,
			Title:         humanizeSlug(slug),
			Namespace:     ns,
			ContentStatus: storage.ContentStatusThin,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.Store(ctx).Entities().UpsertThin(ctx, entity); err != nil {
			return fmt.Errorf("upsert thin entity %q: %w", slug, err)
		}
	}
	edges := storage.MentionEdges(obj, obj.Mentions, false, now)
	if _, err := s.Store(ctx).Edges().CreateMissing(ctx, edges); err != nil {
		return fmt.Errorf("create user-mention edges: %w", err)
	}
	return nil
}

// humanizeSlug derives a display title from a namespace/slug path: takes the
// last segment, replaces dashes/underscores with spaces.
func humanizeSlug(slug string) string {
	parts := strings.Split(slug, "/")
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return last
}

// GetObject retrieves a knowledge object by ID.
// Externalized content comes back whole (see resolveContent).
func (s *Service) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	obj, err := s.Store(ctx).Objects().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.resolveContent(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// ListObjects lists knowledge objects matching the filter. Externalized
// content comes back as a preview (see previewContent).
func (s *Service) ListObjects(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	objs, total, err := s.Store(ctx).Objects().List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, 0, err
	}
	return objs, total, nil
}

// FacetCounts returns metadata type counts for objects matching the filter.
// The result maps metadata type values to their occurrence count.
func (s *Service) FacetCounts(ctx context.Context, filter storage.ObjectFilter) (map[string]int, error) {
	// Fetch all matching objects (no limit for faceting).
	facetFilter := filter
	facetFilter.Limit = 0
	facetFilter.Offset = 0
	objects, _, err := s.Store(ctx).Objects().List(ctx, facetFilter)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, obj := range objects {
		if obj.Metadata == nil {
			counts["(none)"]++
			continue
		}
		t, _ := obj.Metadata["type"].(string)
		if t == "" {
			t = "(none)"
		}
		counts[t]++
	}
	return counts, nil
}

// logOrphans logs each blob a write could not delete once nothing
// referred to it: its object is written, the blob is left behind.
func logOrphans(op, id string, orphans []blob.Orphan) {
	for _, o := range orphans {
		slog.Warn(op+": orphan blob left behind", "object", id, "blob", o.Key, "err", o.Err)
	}
}

// UpdateObject updates a knowledge object.
// Content above storage.blob.threshold is stored externalized, and a
// list preview is stored as the ref it was read from (see storable).
// The blob of content the update replaced is deleted unless another
// object refers to it (see blob.UpdateObject).
func (s *Service) UpdateObject(ctx context.Context, obj *storage.KnowledgeObject) error {
	row, err := s.storable(ctx, obj)
	if err != nil {
		return err
	}
	orphans, err := blob.UpdateObject(ctx, s.Store(ctx), row)
	if err != nil {
		return err
	}
	logOrphans("update object", obj.ID, orphans)
	if ev, err := events.NewEvent("service.objects", string(events.TopicObjectUpdated), events.ObjectUpdatedPayload{
		ObjectID: obj.ID,
		Type:     obj.Type,
	}); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return nil
}

// DeleteObject deletes a knowledge object, its edges and the blob its
// content is externalized to (see blob.DeleteObject). A blob that could
// not be deleted is logged as an orphan; the object is gone either way.
func (s *Service) DeleteObject(ctx context.Context, id string) error {
	orphans, err := blob.DeleteObject(ctx, s.Store(ctx), id)
	if err != nil {
		return err
	}
	logOrphans("delete object", id, orphans)
	if ev, err := events.NewEvent("service.objects", string(events.TopicObjectDeleted), events.ObjectDeletedPayload{ObjectID: id}); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return nil
}

// SearchObjects executes an RSQL query and returns matching objects.
// profileID, when non-empty, restricts results to the named profile's
// objects. The query runs as the caller's viewer: over its scoped
// store, with the cross-object operators compiled on both sides of
// their edges (ADR-080 §7).
func (s *Service) SearchObjects(ctx context.Context, query string, limit, offset int, profileID ...string) ([]*storage.KnowledgeObject, int, error) {
	objs, total, err := s.Search.SearchAs(ctx, s.viewer(ctx), s.Store(ctx).Objects(), query, limit, offset, profileID...)
	if err != nil {
		return nil, 0, err
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, 0, err
	}
	return objs, total, nil
}

// GetJob retrieves a job by ID.
func (s *Service) GetJob(ctx context.Context, id string) (*storage.Job, error) {
	return s.Store(ctx).Jobs().Get(ctx, id)
}

// ListJobs lists jobs matching the filter.
func (s *Service) ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	return s.Store(ctx).Jobs().List(ctx, filter)
}

// RetryJob retries a failed job.
func (s *Service) RetryJob(ctx context.Context, id string) error {
	return s.Queue.Retry(ctx, id)
}

// GetEntity retrieves an entity by slug.
func (s *Service) GetEntity(ctx context.Context, slug string) (*storage.Entity, error) {
	return s.Store(ctx).Entities().Get(ctx, slug)
}

// ResolveEntity returns the entity whose slug, else one of whose
// aliases, equals mention exactly. A miss wraps storage.ErrNotFound.
func (s *Service) ResolveEntity(ctx context.Context, mention string) (*storage.Entity, error) {
	return s.Store(ctx).Entities().Resolve(ctx, mention)
}

// ListEntities lists entities matching the filter.
func (s *Service) ListEntities(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	return s.Store(ctx).Entities().List(ctx, filter)
}

// ListVisibleEntities lists the entities matching filter whose namespace
// visible accepts. The restriction runs in the store ahead of
// filter.Limit and filter.Offset, so a page is filled from the visible
// entities alone and walking the offsets visits each of them exactly
// once. visible is asked once per candidate namespace: filter.Namespaces
// when set, else every namespace the store holds. A nil visible lists
// exactly like ListEntities.
func (s *Service) ListVisibleEntities(ctx context.Context, filter storage.EntityFilter, visible func(namespace string) bool) ([]*storage.Entity, error) {
	if visible == nil {
		return s.ListEntities(ctx, filter)
	}
	es := s.Store(ctx).Entities()
	candidates := filter.Namespaces
	if len(candidates) == 0 {
		var err error
		if candidates, err = es.Namespaces(ctx); err != nil {
			return nil, fmt.Errorf("list entities: %w", err)
		}
	}
	allowed := make([]string, 0, len(candidates))
	for _, ns := range candidates {
		if visible(ns) {
			allowed = append(allowed, ns)
		}
	}
	if len(allowed) == 0 {
		// An empty EntityFilter.Namespaces means no restriction, so
		// nothing visible must not reach the store.
		return nil, nil
	}
	filter.Namespaces = allowed
	return es.List(ctx, filter)
}

// RelatedObjects returns objects related to objectID by traversing shared
// mention-target edges up to depth hops (capped at 3). Results are limited to
// limit items (0 = default of 10). The seed object is never included.
func (s *Service) RelatedObjects(ctx context.Context, objectID string, depth, limit int) ([]*storage.KnowledgeObject, error) {
	if limit <= 0 {
		limit = 10
	}
	ids, err := s.Store(ctx).Edges().RelatedObjectIDs(ctx, objectID, depth, limit)
	if err != nil {
		return nil, fmt.Errorf("related objects: %w", err)
	}

	objs := make([]*storage.KnowledgeObject, 0, len(ids))
	for _, id := range ids {
		obj, err := s.Store(ctx).Objects().Get(ctx, id)
		if err != nil {
			continue // skip missing/deleted objects
		}
		objs = append(objs, obj)
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// EntityBacklinks returns objects that mention the given entity.
// slug may be in @namespace.id mention format or ctxt:// URI format.
func (s *Service) EntityBacklinks(ctx context.Context, slug string) ([]*storage.KnowledgeObject, error) {
	// Normalize slug to the edge storage format (ctxt:// URI string).
	if u, ok := mentions.Parse(slug); ok {
		slug = u.String()
	}
	edges, err := s.Store(ctx).Edges().ListTo(ctx, "entity", slug)
	if err != nil {
		return nil, err
	}

	var objs []*storage.KnowledgeObject
	for _, edge := range edges {
		if edge.FromType == nodeObject {
			obj, err := s.Store(ctx).Objects().Get(ctx, edge.FromID)
			if err != nil {
				continue
			}
			objs = append(objs, obj)
		}
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// CreatePipeline persists a new pipeline through domain.Service so
// the kit pre_validated / pre_persisted veto seams fire before the
// repo write. Validation (Name non-empty) runs in the pipelineValidator
// slot between the two pre-events. Steps may be empty — per-step
// validation lives in pipeline.ValidateComposability at execution time,
// not in the domain.Service path.
func (s *Service) CreatePipeline(ctx context.Context, req CreatePipelineRequest) (string, error) {
	now := time.Now().Truncate(time.Second)

	steps, err := s.parsePipelineSteps(req.Steps)
	if err != nil {
		return "", fmt.Errorf("parse steps: %w", err)
	}

	pipeline := &storage.Pipeline{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Steps:       steps,
		IsBuiltIn:   false,
		Archived:    false,
		Sandbox:     req.Sandbox,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if s.pipelineSvc != nil {
		if err := s.pipelineSvc.Create(ctx, pipeline); err != nil {
			return "", fmt.Errorf("create pipeline: %w", err)
		}
	} else {
		if err := s.Store(ctx).Pipelines().Create(ctx, pipeline); err != nil {
			return "", fmt.Errorf("create pipeline: %w", err)
		}
	}

	return pipeline.ID, nil
}

func (s *Service) GetPipeline(ctx context.Context, name string) (*storage.Pipeline, error) {
	return s.Store(ctx).Pipelines().Get(ctx, name)
}

func (s *Service) ListPipelines(ctx context.Context, filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	return s.Store(ctx).Pipelines().List(ctx, filter)
}

// DeletePipeline removes a custom pipeline. Built-in protection runs
// inline before the service call so the protection error shape stays
// stable for the HTTP layer's "PROTECTED" substring match. The
// pre-existence Get keeps NotFound errors surfacing the same way the
// legacy path did (storage Delete is a no-op when the row is missing).
func (s *Service) DeletePipeline(ctx context.Context, name string) error {
	if _, err := s.Pipes.Get(name); err == nil {
		return fmt.Errorf("PROTECTED: cannot delete built-in pipeline")
	}

	if _, err := s.Store(ctx).Pipelines().Get(ctx, name); err != nil {
		return err
	}

	if s.pipelineSvc != nil {
		return s.pipelineSvc.Delete(ctx, name)
	}
	return s.Store(ctx).Pipelines().Delete(ctx, name)
}

// ArchivePipeline flips the Archived flag and persists via domain.Service.Update
// so pre-events fire on the post-flip entity. The pre-flip status check
// (already-archived) stays inline since the validator runs after the
// flip and can't infer prior state.
func (s *Service) ArchivePipeline(ctx context.Context, name string) error {
	if s.pipelineSvc == nil {
		return s.Store(ctx).Pipelines().Archive(ctx, name)
	}
	p, err := s.Store(ctx).Pipelines().Get(ctx, name)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("pipeline %q not found", name)
	}
	if p.Archived {
		return nil
	}
	p.Archived = true
	p.UpdatedAt = time.Now().Truncate(time.Second)
	opCtx := withPolicyAction(domain.WithSubOp(ctx, "archive"), "archive")
	if err := s.pipelineSvc.Update(opCtx, p); err != nil {
		return fmt.Errorf("archive pipeline: %w", err)
	}
	return nil
}

// UnarchivePipeline is the inverse of ArchivePipeline: same lifecycle
// (load → flip Archived → domain.Service.Update) so pre-events fire
// symmetrically.
func (s *Service) UnarchivePipeline(ctx context.Context, name string) error {
	if s.pipelineSvc == nil {
		return s.Store(ctx).Pipelines().Unarchive(ctx, name)
	}
	p, err := s.Store(ctx).Pipelines().Get(ctx, name)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("pipeline %q not found", name)
	}
	if !p.Archived {
		return nil
	}
	p.Archived = false
	p.UpdatedAt = time.Now().Truncate(time.Second)
	opCtx := withPolicyAction(domain.WithSubOp(ctx, "unarchive"), "unarchive")
	if err := s.pipelineSvc.Update(opCtx, p); err != nil {
		return fmt.Errorf("unarchive pipeline: %w", err)
	}
	return nil
}

// dedupeOnIdempotencyKey resolves a client-generated idempotency key to the
// job that already carries it. hit reports whether a job was found; the empty
// key never matches (legacy keyless submissions keep minting fresh jobs).
func (s *Service) dedupeOnIdempotencyKey(ctx context.Context, key string) (jobID string, hit bool, err error) {
	if key == "" {
		return "", false, nil
	}
	existing, err := s.Store(ctx).Jobs().GetByIdempotencyKey(ctx, key)
	if errors.Is(err, storage.ErrRestrictedViewer) && !errors.Is(err, storage.ErrNoViewer) {
		// Jobs are owner-only (ADR-080 §6), so a restricted caller cannot
		// look its key up: its submission is enqueued as new, which can
		// duplicate a replay but never shows it another caller's job.
		// Idempotency for restricted writers needs per-principal jobs.
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("idempotency lookup: %w", err)
	}
	if existing == nil {
		return "", false, nil
	}
	return existing.ID, true, nil
}

func (s *Service) Enqueue(ctx context.Context, req AnalyzeRequest) (string, error) {
	now := time.Now().Truncate(time.Second)

	// Same replay contract as Analyze: a key already enqueued wins.
	if jobID, hit, err := s.dedupeOnIdempotencyKey(ctx, req.IdempotencyKey); err != nil {
		return "", fmt.Errorf("enqueue: %w", err)
	} else if hit {
		return jobID, nil
	}

	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.detectPipeline(req.Source, req.Type, req.Content)
	}

	// Same existence check as Analyze. See note above.
	if !s.pipelineExists(ctx, pipelineName) {
		return "", fmt.Errorf("enqueue: %w: type=%q pipeline=%q (no pipeline registered for this content type)",
			ErrPipelineNotFound, req.Type, pipelineName)
	}

	job := &storage.Job{
		ID:             uuid.New().String(),
		Type:           "ingest:" + req.Type,
		Status:         storage.JobPending,
		Payload:        req.Content,
		Pipeline:       pipelineName,
		Source:         req.Source,
		MaxRetries:     3,
		CreatedAt:      now,
		UpdatedAt:      now,
		IdempotencyKey: req.IdempotencyKey,
	}

	if err := s.Queue.Enqueue(ctx, job); err != nil {
		// Lost the insert race against a concurrent replay; the surviving
		// job carrying this key is the answer.
		if jobID, hit, lerr := s.dedupeOnIdempotencyKey(ctx, req.IdempotencyKey); lerr == nil && hit {
			return jobID, nil
		}
		return "", err
	}
	if ev, err := events.NewEvent("service.analyze", string(events.TopicJobEnqueued), job); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

func (s *Service) ListSteps(ctx context.Context, source string) ([]*storage.RegisteredStep, int, error) {
	return s.Store(ctx).Steps().List(ctx, source)
}

func (s *Service) GetStep(ctx context.Context, name string) (*storage.RegisteredStep, error) {
	return s.Store(ctx).Steps().Get(ctx, name)
}

// StepInstallRequest is one step install: from a registry configured
// under registries (FromRegistry, its URL) or from a packaged local
// directory (Archive, as steps.PackDir builds it). Exactly one is set.
type StepInstallRequest struct {
	Name         string `json:"name"`
	FromRegistry string `json:"from_registry,omitempty"`
	Archive      []byte `json:"archive,omitempty"`
}

// ErrStepInUse is an uninstall of a step pipelines.overrides still inserts.
var ErrStepInUse = errors.New("step in use")

// InstallStep installs a step into steps.path and records it. A registry
// install is gated by the registry's trust_level and the archive digest
// its manifest declares (steps.Installer.InstallFromRegistry).
func (s *Service) InstallStep(ctx context.Context, req StepInstallRequest) (*storage.RegisteredStep, error) {
	switch {
	case req.FromRegistry != "" && len(req.Archive) > 0:
		return nil, fmt.Errorf("%w: give from_registry or archive, not both", steps.ErrInvalidStep)
	case req.FromRegistry != "":
		return s.Installer.InstallFromRegistry(ctx, req.Name, s.registryByURL(req.FromRegistry))
	case len(req.Archive) > 0:
		return s.Installer.InstallArchive(ctx, req.Name, req.Archive, "local", "")
	default:
		return nil, fmt.Errorf("%w: from_registry or archive is required", steps.ErrInvalidStep)
	}
}

// UninstallStep removes an installed step's files and record. A step
// that pipelines.overrides still inserts is refused: remove it from the
// config first, or the next dpkms serve start fails on it.
func (s *Service) UninstallStep(ctx context.Context, name string) error {
	var users []string
	for pipe, o := range s.Cfg.Pipelines.Overrides {
		for i, ins := range o.InsertSteps {
			if ins.Step == name {
				users = append(users, fmt.Sprintf("pipelines.overrides.%s.insert_steps[%d]", pipe, i))
			}
		}
	}
	if len(users) > 0 {
		sort.Strings(users)
		return fmt.Errorf("%w: %q is inserted by %s; remove it from dpkms.yaml first", ErrStepInUse, name, strings.Join(users, ", "))
	}
	return s.Installer.Uninstall(ctx, name)
}

// FetchRegistry fetches the registry manifest at url and caches it.
func (s *Service) FetchRegistry(ctx context.Context, url string) error {
	_, err := s.Discovery.FetchRegistryManifest(ctx, url)
	return err
}

// UpdateRegistry is a manual sync: it re-fetches the manifest at url,
// with the registry's stored token, and replaces the cached copy, so its
// Last Fetched time and ETag advance. Update reminders come from the
// background check (CheckRegistryUpdates), not from here.
func (s *Service) UpdateRegistry(ctx context.Context, url string) error {
	return s.FetchRegistry(ctx, url)
}

// RegistrySyncResult reports what SyncRegistry pulled.
type RegistrySyncResult struct {
	// EntitySync mirrors the fetched manifest's capabilities.entity_sync:
	// whether the registry advertises an entity index.
	EntitySync bool
	// RegistryEntityCounts is what became of the index's entities; all
	// zero when EntitySync is false.
	RegistryEntityCounts
}

// RegistryEntityCounts is what an entity index sync did with the
// entities a registry served.
type RegistryEntityCounts struct {
	// TrustLevel is the registry's effective trust_level: untrusted
	// when the config leaves it unset.
	TrustLevel config.RegistryTrustLevel `json:"trust_level"`
	// Upserted entities were written to the local entity store.
	Upserted int `json:"upserted"`
	// PendingApproval entities came from an untrusted registry and were
	// not written: they wait in the approval queue (ListPendingEntities).
	PendingApproval int `json:"pending_approval"`
	// Blocked entities came from a sandboxed registry, which never
	// writes to the local graph.
	Blocked int `json:"blocked"`
}

// SyncRegistry is `ctxt registry sync`: it re-fetches and caches the
// manifest at url, then, when that manifest advertises
// capabilities.entity_sync, syncs the registry's entity index
// (SyncRegistryEntities). A registry that serves steps only has no
// /entities/index, so it is never asked for one.
func (s *Service) SyncRegistry(ctx context.Context, url string) (RegistrySyncResult, error) {
	manifest, err := s.Discovery.FetchRegistryManifest(ctx, url)
	if err != nil {
		return RegistrySyncResult{}, err
	}
	if !manifest.Capabilities.EntitySync {
		return RegistrySyncResult{}, nil
	}
	counts, err := s.SyncRegistryEntities(ctx, url)
	if err != nil {
		return RegistrySyncResult{EntitySync: true}, err
	}
	return RegistrySyncResult{EntitySync: true, RegistryEntityCounts: counts}, nil
}

func (s *Service) ListRegistries(ctx context.Context) ([]*storage.RegistryCache, int, error) {
	return s.Store(ctx).Registries().List(ctx)
}

func (s *Service) RemoveRegistry(ctx context.Context, url string) error {
	return s.Store(ctx).Registries().Delete(ctx, url)
}

// RegistryDiffEntry describes a single would-be change from a registry sync.
type RegistryDiffEntry struct {
	Action   string // "add", "update", "remove"
	Kind     string // "entity", "taxonomy", "alias", "step"
	Name     string
	OldValue string // version or empty
	NewValue string // version or empty
}

// DiffRegistrySync fetches the remote manifest for url and diffs it against
// the locally cached manifest. Writes nothing to storage. Returns the list of
// would-be changes and a human-readable summary header.
func (s *Service) DiffRegistrySync(ctx context.Context, url string) ([]RegistryDiffEntry, error) {
	// Fetch remote without caching.
	remote, err := s.Discovery.FetchRemoteManifest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch remote manifest: %w", err)
	}

	// Load local cached manifest (may not exist yet).
	var localSteps map[string]string // name → version
	cache, err := s.Store(ctx).Registries().GetCachedManifest(ctx, url)
	if err == nil && cache != nil && cache.Manifest != nil {
		localSteps = make(map[string]string, len(cache.Manifest.Steps))
		for _, step := range cache.Manifest.Steps {
			localSteps[step.Name] = step.Version
		}
	} else {
		localSteps = make(map[string]string)
	}

	// Build remote steps index.
	remoteSteps := make(map[string]string, len(remote.Steps))
	for _, step := range remote.Steps {
		remoteSteps[step.Name] = step.Version
	}

	var diffs []RegistryDiffEntry

	// Additions and updates.
	for _, step := range remote.Steps {
		if localVer, exists := localSteps[step.Name]; !exists {
			diffs = append(diffs, RegistryDiffEntry{
				Action:   "add",
				Kind:     classifyStepKind(step.Name),
				Name:     step.Name,
				NewValue: step.Version,
			})
		} else if localVer != step.Version {
			diffs = append(diffs, RegistryDiffEntry{
				Action:   "update",
				Kind:     classifyStepKind(step.Name),
				Name:     step.Name,
				OldValue: localVer,
				NewValue: step.Version,
			})
		}
	}

	// Removals.
	for name, ver := range localSteps {
		if _, exists := remoteSteps[name]; !exists {
			diffs = append(diffs, RegistryDiffEntry{
				Action:   "remove",
				Kind:     classifyStepKind(name),
				Name:     name,
				OldValue: ver,
			})
		}
	}

	// Stable sort: action → kind → name.
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Action != diffs[j].Action {
			return diffs[i].Action < diffs[j].Action
		}
		if diffs[i].Kind != diffs[j].Kind {
			return diffs[i].Kind < diffs[j].Kind
		}
		return diffs[i].Name < diffs[j].Name
	})

	return diffs, nil
}

// classifyStepKind returns a human-readable category for a step name.
func classifyStepKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "entity") || strings.Contains(lower, "entities"):
		return "entity"
	case strings.Contains(lower, "taxonomy") || strings.Contains(lower, "taxon"):
		return "taxonomy"
	case strings.Contains(lower, "alias"):
		return "alias"
	default:
		return "step"
	}
}

func (s *Service) ListReminders(ctx context.Context, activeOnly bool) ([]*storage.SystemReminder, int, error) {
	return s.Store(ctx).Reminders().List(ctx, activeOnly)
}

func (s *Service) DismissReminder(ctx context.Context, id string) error {
	return s.Store(ctx).Reminders().Dismiss(ctx, id)
}

// ─── Resurfacing queue ────────────────────────────────────────────────────────

// ListResurfacing returns top-N unseen resurfacing candidates for the given profile.
// Results are ordered by score descending.
func (s *Service) ListResurfacing(
	ctx context.Context, profileID string, limit int, minScore float64,
) ([]*storage.ResurfacingEntry, error) {
	return s.Store(ctx).Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID:  profileID,
		UnseenOnly: true,
		MinScore:   minScore,
		Limit:      limit,
	})
}

// MarkResurfaced marks an entry as shown to the user.
func (s *Service) MarkResurfaced(ctx context.Context, id string) error {
	return s.Store(ctx).Resurfacing().MarkSurfaced(ctx, id, time.Now())
}

// DismissResurfacing dismisses an entry so it no longer appears.
func (s *Service) DismissResurfacing(ctx context.Context, id string) error {
	return s.Store(ctx).Resurfacing().Dismiss(ctx, id, time.Now())
}

// FindByText searches knowledge objects using FTS5 full-text search.
func (s *Service) FindByText(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	return s.FindByTextFiltered(ctx, query, storage.ObjectFilter{Limit: limit})
}

// FindByTextFiltered is like FindByText but accepts a full ObjectFilter
// for metadata facet filtering. The raw query goes straight to the driver:
// each driver applies its own dialect's FTS quoting at the boundary
// (search.SanitizeFTSQueryFor), so no dialect's rules are baked in here.
func (s *Service) FindByTextFiltered(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	objs, err := s.Store(ctx).Objects().FTSSearch(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// CancelJob cancels a pending or running job.
func (s *Service) CancelJob(ctx context.Context, id string) error {
	return s.Store(ctx).Jobs().Cancel(ctx, id)
}

// SyncRegistryEntities syncs entity index from a configured registry.
// Uses the registry's sync_mode (thin or full). Returns what became of
// the entities: written, or held back by the registry's trust_level.
// When the registry manifest declares an entitlement_url, the entitlement is
// fetched and stored before any sync occurs; ErrEntitlementRequired is returned
// if the registry denies access.
func (s *Service) SyncRegistryEntities(ctx context.Context, registryURL string) (RegistryEntityCounts, error) {
	var cfg config.RegistryConfig
	for _, r := range s.Cfg.Registries {
		if r.URL == registryURL {
			cfg = r
			break
		}
	}
	if cfg.URL == "" {
		cfg = config.RegistryConfig{URL: registryURL, SyncMode: config.RegistrySyncModeFull}
	}

	// Populate EntitlementURL from cached manifest if not already set.
	if cfg.EntitlementURL == "" {
		if cache, err := s.Store(ctx).Registries().GetCachedManifest(ctx, registryURL); err == nil &&
			cache != nil && cache.Manifest != nil && cache.Manifest.EntitlementURL != "" {
			cfg.EntitlementURL = cache.Manifest.EntitlementURL
		}
	}

	syncer := registrysync.New(s.Store(ctx).Entities()).
		WithEntitlements(s.Store(ctx).Entitlements()).
		WithRegistryStore(s.Store(ctx).Registries()).
		WithRequireSignatures(s.Cfg.RegistriesGlobal.RequireSignatures).
		WithCredentials(s.registryAuth).
		WithPendingQueue(s.Store(ctx).PendingEntities())
	result, err := syncer.Sync(ctx, cfg)
	if err != nil {
		return RegistryEntityCounts{}, fmt.Errorf("sync registry entities: %w", err)
	}
	return RegistryEntityCounts{
		TrustLevel:      cfg.EffectiveTrustLevel(),
		Upserted:        result.Upserted,
		PendingApproval: result.PendingApproval,
		Blocked:         result.Blocked,
	}, nil
}

// SyncAllRegistriesResult is the output of SyncAllRegistriesWithReconciliation.
type SyncAllRegistriesResult struct {
	// Merged is the total number of entities written after reconciliation.
	Merged int
	// Conflicts lists per-field disagreements across registries.
	Conflicts []registrysync.ConflictReport
	// OfflineRegistries lists registry URLs that were unreachable.
	OfflineRegistries []string
	// PendingApproval and Blocked total what the registries' trust_level
	// held back; Registries breaks them down per configured registry.
	PendingApproval int
	Blocked         int
	Registries      []ReconciledRegistryCounts
}

// ReconciledRegistryCounts is what a reconciling sync's trust gate did
// with one registry's entities. Upserted is absent: written entities
// are counted after reconciliation (Merged), not per registry.
type ReconciledRegistryCounts struct {
	Name            string                    `json:"name"`
	TrustLevel      config.RegistryTrustLevel `json:"trust_level"`
	PendingApproval int                       `json:"pending_approval"`
	Blocked         int                       `json:"blocked"`
}

// SyncAllRegistriesWithReconciliation syncs all configured registries, reconciles
// conflicting entity definitions using the given merge strategy, and returns a
// conflict report. Offline-first: unreachable registries are skipped; local cache
// is preserved.
//
// strategy: "last-write-wins" (default) or "trust-score".
// trustScores: optional map of registry URL → 0.0–1.0; only used with trust-score.
func (s *Service) SyncAllRegistriesWithReconciliation(
	ctx context.Context,
	strategy registrysync.MergeStrategy,
	trustScores map[string]float64,
) (*SyncAllRegistriesResult, error) {
	if len(s.Cfg.Registries) == 0 {
		return &SyncAllRegistriesResult{}, nil
	}
	if strategy == "" {
		strategy = registrysync.MergeLastWriteWins
	}

	syncer := registrysync.New(s.Store(ctx).Entities()).
		WithRegistryStore(s.Store(ctx).Registries()).
		WithRequireSignatures(s.Cfg.RegistriesGlobal.RequireSignatures).
		WithCredentials(s.registryAuth).
		WithPendingQueue(s.Store(ctx).PendingEntities())
	multi := registrysync.MultiSyncConfig{
		Registries:  s.Cfg.Registries,
		Strategy:    strategy,
		TrustScores: trustScores,
	}
	result, err := syncer.MultiSync(ctx, multi)
	if err != nil {
		return nil, fmt.Errorf("multi-registry sync: %w", err)
	}

	out := &SyncAllRegistriesResult{
		Merged:            result.Merged,
		Conflicts:         result.Conflicts,
		OfflineRegistries: result.OfflineRegistries,
		PendingApproval:   result.PendingApproval,
		Blocked:           result.Blocked,
	}
	for _, r := range result.PerRegistry {
		reg := s.registryByURL(r.RegistryURL)
		out.Registries = append(out.Registries, ReconciledRegistryCounts{
			Name:            reg.Name,
			TrustLevel:      reg.EffectiveTrustLevel(),
			PendingApproval: r.PendingApproval,
			Blocked:         r.Blocked,
		})
	}
	return out, nil
}

// registryByURL is the configured registry at url, or one with just
// that URL (and so untrusted) when none is configured.
func (s *Service) registryByURL(url string) config.RegistryConfig {
	for _, r := range s.Cfg.Registries {
		if r.URL == url {
			return r
		}
	}
	return config.RegistryConfig{URL: url}
}

// PullEntity promotes a thin entity to full by fetching its complete definition
// from its source registry. Returns ErrEntityDefinitionUnavailable if not thin.
func (s *Service) PullEntity(ctx context.Context, slug string) (*storage.Entity, error) {
	entity, err := s.Store(ctx).Entities().Get(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("get entity: %w", err)
	}

	if entity.ContentStatus == storage.ContentStatusFull {
		// Already full — nothing to do.
		return entity, nil
	}

	if entity.RegistryURL == "" {
		return nil, fmt.Errorf("entity %q has no registry_url; cannot pull", slug)
	}

	// Mark as pending so concurrent callers can detect an in-progress pull.
	if err := s.Store(ctx).Entities().SetContentStatus(ctx, slug, storage.ContentStatusPendingPull); err != nil {
		return nil, fmt.Errorf("set pending_pull: %w", err)
	}

	// Fetch full definition from registry.
	fullEntity, err := s.fetchRemoteEntityDefinition(ctx, entity.RegistryURL, slug)
	if err != nil {
		// Restore thin status on failure so the entity remains usable.
		_ = s.Store(ctx).Entities().SetContentStatus(ctx, slug, storage.ContentStatusThin)
		return nil, fmt.Errorf("fetch entity definition: %w", err)
	}

	fullEntity.Slug = entity.Slug
	fullEntity.VersionHash = entity.VersionHash
	fullEntity.RegistryURL = entity.RegistryURL
	fullEntity.ContentStatus = storage.ContentStatusFull
	fullEntity.CreatedAt = entity.CreatedAt
	fullEntity.UpdatedAt = time.Now().Truncate(time.Second)

	if err := s.Store(ctx).Entities().Upsert(ctx, fullEntity); err != nil {
		_ = s.Store(ctx).Entities().SetContentStatus(ctx, slug, storage.ContentStatusThin)
		return nil, fmt.Errorf("store full entity: %w", err)
	}

	return fullEntity, nil
}

// fetchRemoteEntityDefinition calls GET <registryURL>/entities/<slug> and decodes
// the full entity definition.
func (s *Service) fetchRemoteEntityDefinition(ctx context.Context, registryURL, slug string) (*storage.Entity, error) {
	url := registryURL + "/entities/" + slug
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.registryAuth.Client(http.DefaultClient).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := s.registryAuth.CheckResponse(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, storage.ErrEntityDefinitionUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}

	var entity storage.Entity
	if err := json.NewDecoder(resp.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("decode entity: %w", err)
	}
	return &entity, nil
}

// Compose generates a markdown composition from knowledge objects
// (composition.Compose).
func (s *Service) Compose(ctx context.Context, objects []*storage.KnowledgeObject, compositionType string) (string, error) {
	return composition.Compose(objects, compositionType), nil
}

// ComposeWithCitations generates a markdown composition with inline
// [ref:ID] citation markers and an appended reference table
// (composition.WithCitations).
func (s *Service) ComposeWithCitations(ctx context.Context, objects []*storage.KnowledgeObject, compositionType string) (*CompositionResult, error) {
	return composition.WithCitations(objects, compositionType), nil
}

// objectSnippet returns the best short text snippet for an object
// (composition.Snippet).
func objectSnippet(obj *storage.KnowledgeObject) string {
	return composition.Snippet(obj)
}

// SearchObjectsNodeAware runs an RSQL query and post-filters results by
// NodeAwareFilter constraints (NodeTypes / EdgeTypes). Storage does not yet
// expose native node-type filtering, so the filtering happens in-process.
// When filter is nil or empty, behaviour is identical to SearchObjects.
func (s *Service) SearchObjectsNodeAware(ctx context.Context, query string, limit, offset int, filter *storage.NodeAwareFilter, profileID ...string) ([]*storage.KnowledgeObject, int, error) {
	// Fetch a wider candidate pool when node filtering is active so the final
	// page still has enough results after the in-process filter.
	fetchLimit := limit
	fetchOffset := offset
	nodeFilter := filter != nil && (len(filter.NodeTypes) > 0 || len(filter.EdgeTypes) > 0)
	if nodeFilter {
		// TODO: push NodeTypes/EdgeTypes into storage query when
		// storage exposes node-index filtering.
		fetchLimit = limit*10 + 200
		fetchOffset = 0
	}

	all, _, err := s.Search.Search(ctx, s.Store(ctx).Objects(), query, fetchLimit, fetchOffset, profileID...)
	if err != nil {
		return nil, 0, err
	}

	if !nodeFilter {
		if err := s.previewContent(ctx, all); err != nil {
			return nil, 0, err
		}
		return all, len(all), nil
	}

	nodeTypeSet := make(map[string]bool, len(filter.NodeTypes))
	for _, nt := range filter.NodeTypes {
		nodeTypeSet[nt] = true
	}
	edgeTypeSet := make(map[string]bool, len(filter.EdgeTypes))
	for _, et := range filter.EdgeTypes {
		edgeTypeSet[et] = true
	}

	var filtered []*storage.KnowledgeObject
	for _, obj := range all {
		if graphHasTypes(obj.Graph, nodeTypeSet, edgeTypeSet) {
			filtered = append(filtered, obj)
		}
	}

	total := len(filtered)
	// Apply original offset/limit to filtered results.
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]
	if err := s.previewContent(ctx, page); err != nil {
		return nil, 0, err
	}
	return page, total, nil
}

// graphHasTypes reports whether g has a node of one of nodeTypes and an
// edge of one of edgeTypes; an empty set matches any graph. A nil graph
// never matches.
func graphHasTypes(g *storage.ObjectGraph, nodeTypes, edgeTypes map[string]bool) bool {
	if g == nil {
		return false
	}
	if len(nodeTypes) > 0 && !slices.ContainsFunc(g.Nodes, func(n storage.GraphNode) bool { return nodeTypes[n.NodeType] }) {
		return false
	}
	if len(edgeTypes) > 0 && !slices.ContainsFunc(g.Edges, func(e storage.GraphEdge) bool { return edgeTypes[e.EdgeType] }) {
		return false
	}
	return true
}

// --- Feed methods ---

// CreateFeed creates a new feed subscription.
func (s *Service) CreateFeed(ctx context.Context, url, format string) (*storage.Feed, error) {
	feed := &storage.Feed{
		ID:        "feed_" + uuid.New().String()[:8],
		URL:       url,
		Format:    format,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Store(ctx).Feeds().Create(ctx, feed); err != nil {
		return nil, fmt.Errorf("create feed: %w", err)
	}
	return feed, nil
}

// ListFeeds returns all feed subscriptions matching the filter.
func (s *Service) ListFeeds(ctx context.Context, filter storage.FeedFilter) ([]*storage.Feed, error) {
	return s.Store(ctx).Feeds().List(ctx, filter)
}

// SyncFeed enqueues a feed.sync pipeline job to fetch and process feed items.
// Deduplication and item enqueueing are handled by the pipeline steps.
func (s *Service) SyncFeed(ctx context.Context, id string) (string, error) {
	feed, err := s.Store(ctx).Feeds().Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("feed not found: %w", err)
	}

	jobID, err := s.Analyze(ctx, AnalyzeRequest{
		Content:  feed.URL,
		Type:     "url",
		Pipeline: "feed.sync",
		Source:   "feed:" + id,
	})
	if err != nil {
		return "", fmt.Errorf("sync feed: %w", err)
	}
	return jobID, nil
}

// SyncAllFeeds enqueues sync jobs for all active feeds.
func (s *Service) SyncAllFeeds(ctx context.Context) ([]string, error) {
	feeds, err := s.Store(ctx).Feeds().List(ctx, storage.FeedFilter{Status: "active"})
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	// nil when no feed enqueues: POST /feeds/sync answers job_ids: null.
	var jobIDs []string //nolint:prealloc // see above; preallocating would answer []
	for _, f := range feeds {
		jobID, err := s.SyncFeed(ctx, f.ID)
		if err != nil {
			continue
		}
		jobIDs = append(jobIDs, jobID)
	}
	return jobIDs, nil
}

// DeleteFeed removes a feed subscription.
func (s *Service) DeleteFeed(ctx context.Context, id string) error {
	if _, err := s.Store(ctx).Feeds().Get(ctx, id); err != nil {
		return fmt.Errorf("feed not found: %w", err)
	}
	return s.Store(ctx).Feeds().Delete(ctx, id)
}

// --- Batch import methods ---

const maxBatchRecords = 10000

// CreateBatch creates a new batch import operation and enqueues per-record jobs.
func (s *Service) CreateBatch(ctx context.Context, content, format string) (*storage.Batch, error) {
	// Handle OPML specially: create feed subscriptions instead of knowledge objects.
	if format == "opml" {
		return s.createBatchFromOPML(ctx, content)
	}

	records, parseErrors := s.parseBatchRecords(content, format)
	if len(records)+len(parseErrors) > maxBatchRecords {
		return nil, fmt.Errorf("batch size %d exceeds limit of %d", len(records)+len(parseErrors), maxBatchRecords)
	}

	batch := &storage.Batch{
		ID:           "batch_" + uuid.New().String()[:8],
		Format:       format,
		TotalRecords: len(records) + len(parseErrors),
		Failed:       len(parseErrors),
		Errors:       parseErrors,
		Status:       "processing",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.Store(ctx).Batches().Create(ctx, batch); err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}

	// Enqueue a job for each valid record.
	for _, rec := range records {
		if rec.Content == "" {
			batch.Failed++
			batch.Errors = append(batch.Errors, storage.BatchError{Error: "missing content"})
			continue
		}
		jobID, err := s.Analyze(ctx, AnalyzeRequest{
			Content:  rec.Content,
			Type:     rec.Type,
			Pipeline: "batch.import",
			Source:   rec.Source,
		})
		if err != nil {
			batch.Failed++
			batch.Errors = append(batch.Errors, storage.BatchError{Error: err.Error()})
			continue
		}
		_ = s.Store(ctx).Edges().Create(ctx, &storage.Edge{
			ID:        uuid.New().String(),
			FromType:  "batch",
			FromID:    batch.ID,
			ToType:    "job",
			ToID:      jobID,
			EdgeType:  "batch_contains",
			CreatedAt: time.Now(),
		})
	}

	switch {
	case batch.Failed == 0:
		batch.Status = "completed"
	case batch.Failed < batch.TotalRecords:
		batch.Status = "partial"
	default:
		batch.Status = "failed"
	}
	batch.Completed = batch.TotalRecords - batch.Failed
	batch.UpdatedAt = time.Now()
	_ = s.Store(ctx).Batches().Update(ctx, batch)

	return batch, nil
}

// ImportFormatJSONL is the batch import format: one JSON ImportRecord per
// line. CreateBatch assumes it when no format is given.
const ImportFormatJSONL = "jsonl"

// parseBatchRecords parses JSONL, CSV, or TSV content into ImportRecords.
func (s *Service) parseBatchRecords(content, format string) ([]storage.ImportRecord, []storage.BatchError) {
	var records []storage.ImportRecord
	var errs []storage.BatchError

	switch format {
	case ImportFormatJSONL, "":
		for i, line := range strings.Split(strings.TrimSpace(content), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec storage.ImportRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				errs = append(errs, storage.BatchError{Line: i + 1, Error: err.Error()})
				continue
			}
			records = append(records, rec)
		}
	case "csv", "tsv":
		r := csv.NewReader(strings.NewReader(content))
		if format == "tsv" {
			r.Comma = '\t'
		}
		rows, err := r.ReadAll()
		if err != nil {
			errs = append(errs, storage.BatchError{Line: 0, Error: err.Error()})
			return records, errs
		}
		if len(rows) < 2 {
			return records, errs
		}
		header := rows[0]
		colIdx := make(map[string]int)
		for i, h := range header {
			colIdx[strings.TrimSpace(strings.ToLower(h))] = i
		}
		for i, row := range rows[1:] {
			rec := storage.ImportRecord{}
			if idx, ok := colIdx["content"]; ok && idx < len(row) {
				rec.Content = row[idx]
			}
			if idx, ok := colIdx["type"]; ok && idx < len(row) {
				rec.Type = row[idx]
			}
			if idx, ok := colIdx["source"]; ok && idx < len(row) {
				rec.Source = row[idx]
			}
			if rec.Content == "" {
				errs = append(errs, storage.BatchError{Line: i + 2, Error: "missing content"})
				continue
			}
			records = append(records, rec)
		}
	}
	return records, errs
}

// opmlOutline is used for XML parsing of OPML files.
type opmlOutline struct {
	Type     string        `xml:"type,attr"`
	Text     string        `xml:"text,attr"`
	XMLUrl   string        `xml:"xmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlDoc struct {
	Body opmlBody `xml:"body"`
}

// createBatchFromOPML parses OPML and creates feed subscriptions.
func (s *Service) createBatchFromOPML(ctx context.Context, content string) (*storage.Batch, error) {
	var doc opmlDoc
	if err := xml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("invalid OPML: %w", err)
	}

	var feedURLs []string
	var collectFeeds func(outlines []opmlOutline)
	collectFeeds = func(outlines []opmlOutline) {
		for _, o := range outlines {
			if o.XMLUrl != "" {
				feedURLs = append(feedURLs, o.XMLUrl)
			}
			collectFeeds(o.Outlines)
		}
	}
	collectFeeds(doc.Body.Outlines)

	batch := &storage.Batch{
		ID:           "batch_" + uuid.New().String()[:8],
		Format:       "opml",
		TotalRecords: len(feedURLs),
		Status:       "processing",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.Store(ctx).Batches().Create(ctx, batch); err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}

	for _, url := range feedURLs {
		if _, err := s.CreateFeed(ctx, url, "rss"); err != nil {
			batch.Failed++
			batch.Errors = append(batch.Errors, storage.BatchError{Error: err.Error()})
		} else {
			batch.Completed++
		}
	}
	batch.Status = "completed"
	if batch.Failed > 0 {
		batch.Status = "partial"
	}
	batch.UpdatedAt = time.Now()
	_ = s.Store(ctx).Batches().Update(ctx, batch)

	return batch, nil
}

// GetBatch retrieves a batch import operation by ID.
func (s *Service) GetBatch(ctx context.Context, id string) (*storage.Batch, error) {
	return s.Store(ctx).Batches().Get(ctx, id)
}

// --- Detector methods ---

// DetectorCreateRequest carries parameters for creating a detector.
type DetectorCreateRequest struct {
	Kind         storage.DetectorKind
	Name         string
	PipelineName string
	Pattern      string
	Priority     int
}

// CreateDetector creates a new pipeline detector.
func (s *Service) CreateDetector(ctx context.Context, req DetectorCreateRequest) (*storage.DetectorRecord, error) {
	d := &storage.DetectorRecord{
		ID:           "det_" + uuid.New().String()[:8],
		Kind:         req.Kind,
		Name:         req.Name,
		PipelineName: req.PipelineName,
		Pattern:      req.Pattern,
		Priority:     req.Priority,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.Store(ctx).Detectors().Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create detector: %w", err)
	}
	return d, nil
}

// ListDetectors returns all detectors matching the filter.
func (s *Service) ListDetectors(ctx context.Context, filter storage.DetectorFilter) ([]*storage.DetectorRecord, error) {
	detectors, _, err := s.Store(ctx).Detectors().List(ctx, filter)
	return detectors, err
}

// DeleteDetector removes a detector.
func (s *Service) DeleteDetector(ctx context.Context, id string) error {
	return s.Store(ctx).Detectors().Delete(ctx, id)
}

// EnableDetector enables a detector.
func (s *Service) EnableDetector(ctx context.Context, id string) error {
	return s.Store(ctx).Detectors().Enable(ctx, id)
}

// DisableDetector disables a detector.
func (s *Service) DisableDetector(ctx context.Context, id string) error {
	return s.Store(ctx).Detectors().Disable(ctx, id)
}

// --- Saved Search (US-0054) ---

// SavedSearchCreateRequest carries parameters for creating a saved search.
type SavedSearchCreateRequest struct {
	Name      string
	Query     string
	ProfileID string
	AlertOn   string
	Notify    string
}

// CreateSavedSearch persists a named saved search.
// Returns error if the name already exists (UNIQUE constraint).
func (s *Service) CreateSavedSearch(ctx context.Context, req SavedSearchCreateRequest) (*storage.SavedSearch, error) {
	now := time.Now().Truncate(time.Second)
	ss := &storage.SavedSearch{
		ID:        "ss_" + uuid.New().String()[:8],
		Name:      req.Name,
		Query:     req.Query,
		ProfileID: req.ProfileID,
		AlertOn:   req.AlertOn,
		Notify:    req.Notify,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Store(ctx).SavedSearches().Create(ctx, ss); err != nil {
		return nil, fmt.Errorf("create saved search: %w", err)
	}
	return ss, nil
}

// GetSavedSearch returns a saved search by name, or nil if not found.
func (s *Service) GetSavedSearch(ctx context.Context, name string) (*storage.SavedSearch, error) {
	return s.Store(ctx).SavedSearches().GetByName(ctx, name)
}

// ListSavedSearches returns saved searches matching the filter.
func (s *Service) ListSavedSearches(ctx context.Context, filter storage.SavedSearchFilter) ([]*storage.SavedSearch, error) {
	return s.Store(ctx).SavedSearches().List(ctx, filter)
}

// UpdateSavedSearch persists field changes to an existing saved search.
func (s *Service) UpdateSavedSearch(ctx context.Context, ss *storage.SavedSearch) error {
	ss.UpdatedAt = time.Now().Truncate(time.Second)
	return s.Store(ctx).SavedSearches().Update(ctx, ss)
}

// DeleteSavedSearch removes a saved search by name.
func (s *Service) DeleteSavedSearch(ctx context.Context, name string) error {
	return s.Store(ctx).SavedSearches().Delete(ctx, name)
}

// --- Search History (US-0055) ---

// AppendSearchHistory records one search query execution.
func (s *Service) AppendSearchHistory(ctx context.Context, query, profileID, strategies string, resultCount int) error {
	e := &storage.SearchHistoryEntry{
		ID:             "sh_" + uuid.New().String()[:8],
		Query:          query,
		ProfileID:      profileID,
		StrategiesUsed: strategies,
		ResultCount:    resultCount,
		SearchedAt:     time.Now().Truncate(time.Second),
	}
	return s.Store(ctx).SearchHistory().Append(ctx, e)
}

// ListSearchHistory returns search history entries matching the filter.
func (s *Service) ListSearchHistory(ctx context.Context, filter storage.SearchHistoryFilter) ([]*storage.SearchHistoryEntry, error) {
	return s.Store(ctx).SearchHistory().List(ctx, filter)
}

// ClearSearchHistory removes all history entries for a profile.
func (s *Service) ClearSearchHistory(ctx context.Context, profileID string) error {
	return s.Store(ctx).SearchHistory().ClearByProfile(ctx, profileID)
}

// --- Semantic search ---

// SemanticSearchFiltered runs a vector-only search over the default
// embedding model's index (read now, never cached), filtered by filter.
// When the semantic leg cannot run (no default model, provider failure,
// dimension mismatch, missing index or no coverage) it falls back to FTS if
// cfg.FallbackToFTS is set and fails otherwise; either way the reason is in
// the returned diagnostics' Semantic report, so the degradation is visible.
func (s *Service) SemanticSearchFiltered(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]*storage.KnowledgeObject, SearchDiagnostics, error) {
	objs, rep, err := sem.Search(ctx, s.Store(ctx), search.DecomposeQuery(query, 2), filter)
	if err != nil {
		return nil, SearchDiagnostics{}, fmt.Errorf("semantic search: %w", err)
	}
	diagnostics := SearchDiagnostics{Semantic: &rep}
	if rep.OK() {
		diagnostics.CandidateCount = len(objs)
		if err := s.previewContent(ctx, objs); err != nil {
			return nil, diagnostics, err
		}
		return objs, diagnostics, nil
	}
	if !cfg.FallbackToFTS {
		return nil, diagnostics, &SemanticUnavailableError{Report: rep}
	}
	objs, err = s.Store(ctx).Objects().FTSSearch(ctx, query, filter)
	if err != nil {
		return nil, diagnostics, fmt.Errorf("semantic search fts fallback: %w", err)
	}
	diagnostics.CandidateCount = len(objs)
	if err := s.previewContent(ctx, objs); err != nil {
		return nil, diagnostics, err
	}
	return objs, diagnostics, nil
}

// HybridSearch runs FTS and vector search concurrently, merges results with
// Reciprocal Rank Fusion (RRF), and returns the top-limit objects. The
// vector leg searches the default embedding model's index through sem.
// When that leg cannot run, cfg.FallbackToFTS degrades to FTS-only (the
// reason is in the diagnostics); otherwise the search fails.
func (s *Service) HybridSearch(ctx context.Context, query string, limit int, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]*storage.KnowledgeObject, error) {
	return s.HybridSearchFiltered(ctx, query, storage.ObjectFilter{Limit: limit}, sem, cfg)
}

// HybridSearchFiltered is like HybridSearch but accepts a full ObjectFilter
// for metadata facet filtering.
func (s *Service) HybridSearchFiltered(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]*storage.KnowledgeObject, error) {
	objs, _, err := s.HybridSearchFilteredWithDiagnostics(ctx, query, filter, sem, cfg)
	return objs, err
}

// HybridSearchFilteredWithDiagnostics is like HybridSearchFiltered but also
// returns SearchDiagnostics describing candidates that surfaced from the FTS
// or vector legs and were dropped by the reranker MinScore threshold.
func (s *Service) HybridSearchFilteredWithDiagnostics(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]*storage.KnowledgeObject, SearchDiagnostics, error) {
	envelope, err := s.HybridSearchExplainFilteredWithDiagnostics(ctx, query, filter, sem, cfg)
	if err != nil {
		return nil, SearchDiagnostics{}, err
	}
	out := make([]*storage.KnowledgeObject, len(envelope.Results))
	for i, r := range envelope.Results {
		obj := r.Object
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["rrf_score"] = r.Breakdown.Total
		out[i] = obj
	}
	return out, envelope.Diagnostics, nil
}

// HybridSearchExplain is like HybridSearch but returns per-result score breakdowns
// so callers can explain why each result ranked where it did.
func (s *Service) HybridSearchExplain(ctx context.Context, query string, limit int, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]HybridResult, error) {
	return s.HybridSearchExplainFiltered(ctx, query, storage.ObjectFilter{Limit: limit}, sem, cfg)
}

// HybridSearchExplainFiltered is like HybridSearchExplain but accepts a full
// ObjectFilter for metadata facet filtering. Diagnostics about dropped
// candidates are discarded; call HybridSearchExplainFilteredWithDiagnostics
// for the full envelope.
func (s *Service) HybridSearchExplainFiltered(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]HybridResult, error) {
	envelope, err := s.HybridSearchExplainFilteredWithDiagnostics(ctx, query, filter, sem, cfg)
	if err != nil {
		return nil, err
	}
	return envelope.Results, nil
}

// HybridSearchExplainFilteredWithDiagnostics runs the full hybrid search
// pipeline and returns a HybridSearchResult envelope containing both the
// post-threshold result list and a SearchDiagnostics block describing
// candidates dropped by the reranker MinScore threshold.
//
// Diagnostics let callers distinguish two visually identical "no results"
// cases at the CLI: (a) zero candidates from any retrieval leg vs. (b)
// candidates surfaced but all fell below MinScore.
func (s *Service) HybridSearchExplainFilteredWithDiagnostics(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) (*HybridSearchResult, error) {
	return s.hybridSearch(ctx, query, filter, sem, cfg, false)
}

// HybridSearchExplainFilteredWithTrace is HybridSearchExplainFilteredWithDiagnostics
// plus envelope.Trace: every candidate the search scored, its leg
// membership, raw and fused scores, final rank, and the stage it reached
// (returned, cut_limit, cut_threshold). Results and Diagnostics are
// identical to the untraced call.
func (s *Service) HybridSearchExplainFilteredWithTrace(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) (*HybridSearchResult, error) {
	return s.hybridSearch(ctx, query, filter, sem, cfg, true)
}

// hybridSearch is the shared hybrid pipeline. withTrace adds per-candidate
// bookkeeping; without it the pipeline does no trace work.
func (s *Service) hybridSearch(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig, withTrace bool) (*HybridSearchResult, error) {
	k := cfg.RRF.K
	if k <= 0 {
		k = 60
	}

	ftsPool := cfg.CandidatePool.FTS
	if ftsPool <= 0 {
		ftsPool = 50
	}

	// Expand FTS query with concept aliases; vector leg uses the original
	// query. The expanded text stays raw here: each driver applies its own
	// dialect's FTS quoting at the boundary (search.SanitizeFTSQueryFor),
	// covering user input and alias-injected punctuation alike.
	ftsQuery, err := search.ExpandQuery(ctx, query, newStorageAliasResolver(s.Store(ctx).Aliases(), ""))
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	// Build per-leg filters: inherit metadata facets but override pool size.
	ftsFilter := filter
	ftsFilter.Limit = ftsPool

	type legResult struct {
		results []*storage.KnowledgeObject
		err     error
	}

	ftsCh := make(chan legResult, 1)
	go func() {
		res, err := s.Store(ctx).Objects().FTSSearch(ctx, ftsQuery, ftsFilter)
		ftsCh <- legResult{res, err}
	}()

	type vecLegResult struct {
		results []*storage.KnowledgeObject
		report  retrieval.SemanticReport
		err     error
	}
	vecCh := make(chan vecLegResult, 1)
	go func() {
		res, rep, err := s.hybridVectorLeg(ctx, query, filter, sem, cfg)
		vecCh <- vecLegResult{res, rep, err}
	}()

	ftsRes := <-ftsCh
	vecRes := <-vecCh

	if ftsRes.err != nil {
		return nil, fmt.Errorf("hybrid search fts leg: %w", ftsRes.err)
	}
	if vecRes.err != nil {
		return nil, vecRes.err
	}

	// Build per-leg RRF scores and collect candidates for the reranker.
	ftsScores := map[string]float64{}
	vecScores := map[string]float64{}
	byID := map[string]*storage.KnowledgeObject{}

	addLeg := func(results []*storage.KnowledgeObject, weight float64, dest map[string]float64) {
		for rank, obj := range results {
			dest[obj.ID] += weight * (1.0 / float64(k+rank+1))
			byID[obj.ID] = obj
		}
	}

	addLeg(ftsRes.results, cfg.RRF.FTSWeight, ftsScores)
	addLeg(vecRes.results, cfg.RRF.VectorWeight, vecScores)

	// Build candidate map for the reranker.
	candidates := make(map[string]ranking.Candidate, len(byID))
	for id, obj := range byID {
		candidates[id] = ranking.Candidate{
			Object:   obj,
			FTSScore: ftsScores[id],
			VecScore: vecScores[id],
		}
	}

	// Resolve reranker weight config (fall back to package defaults when zero).
	rc := cfg.Reranker
	weights := ranking.WeightConfig{
		MentionBoost:    rc.MentionBoostPerMention,
		MaxMentionBoost: rc.MaxMentionBoost,
		DirectBacklink:  rc.DirectBacklinkBoost,
		HopBacklink:     rc.HopBacklinkBoost,
	}
	if weights.MentionBoost == 0 {
		weights = ranking.DefaultWeights()
	}

	reranker := ranking.New(s.Store(ctx).Edges(), weights)
	// Run the reranker with minScore=0 so we receive every scored candidate.
	// Diagnostics need to know which candidates fell below cfg.MinScore — that
	// information is lost if we let Rerank filter early.
	scored, err := reranker.Rerank(ctx, query, candidates, 0)
	if err != nil {
		return nil, fmt.Errorf("hybrid search rerank: %w", err)
	}

	threshold := cfg.MinScore
	ranked, diagnostics := partitionByThreshold(scored, threshold)
	diagnostics.CandidateCount = len(candidates)
	semantic := vecRes.report
	diagnostics.Semantic = &semantic

	// count candidates whose pipeline stamp is older than the
	// registry's installed version for the same family. Surface the count
	// as a soft warning so callers can prompt operators toward
	// `ctxt upgrade plan`. Computed once per family for cheap repeat lookups.
	if staleCount := countStaleCandidates(s.Pipes, candidates); staleCount > 0 {
		diagnostics.StalenessWarning = &StalenessWarning{
			Count: staleCount,
			Reason: fmt.Sprintf(
				"%d objects in this result set are pending pipeline upgrade — run 'ctxt upgrade plan' to see what's affected",
				staleCount,
			),
		}
	}

	limit := filter.Limit
	if limit <= 0 || limit > len(ranked) {
		limit = len(ranked)
	}
	kept := make([]*storage.KnowledgeObject, limit)
	for i := range kept {
		kept[i] = ranked[i].Object
	}
	// Before the document view, which is built from the content.
	if err := s.previewContent(ctx, kept); err != nil {
		return nil, err
	}
	out := make([]HybridResult, limit)
	for i := 0; i < limit; i++ {
		r := ranked[i]
		out[i] = HybridResult{
			Object:       r.Object,
			Breakdown:    breakdownOf(r),
			DocumentView: projection.ProjectDocument(r.Object),
		}
	}
	envelope := &HybridSearchResult{Results: out, Diagnostics: diagnostics}
	if withTrace {
		envelope.Trace = buildSearchTrace(searchTraceInput{
			query:     query,
			ftsQuery:  ftsQuery,
			semantic:  vecRes.report,
			ftsPool:   ftsPool,
			vecPool:   vectorPool(cfg),
			rrf:       cfg.RRF,
			k:         k,
			weights:   weights,
			threshold: threshold,
			limit:     filter.Limit,
			kept:      limit,
			scored:    scored,
			ftsLeg:    ftsRes.results,
			vecLeg:    vecRes.results,
		})
	}
	return envelope, nil
}

// partitionByThreshold splits reranked results into above-threshold (kept)
// and below-threshold (dropped), preserving the descending-score order from
// the reranker. The returned diagnostics carry the threshold and the
// dropped count/top score; the caller fills CandidateCount.
func partitionByThreshold(scored []ranking.Result, threshold float64) ([]ranking.Result, SearchDiagnostics) {
	ranked := make([]ranking.Result, 0, len(scored))
	diag := SearchDiagnostics{Threshold: threshold}
	haveBelow := false
	for _, r := range scored {
		if r.Total < threshold {
			diag.BelowThresholdCount++
			if !haveBelow || r.Total > diag.TopBelowThresholdScore {
				diag.TopBelowThresholdScore = r.Total
				haveBelow = true
			}
			continue
		}
		ranked = append(ranked, r)
	}
	return ranked, diag
}

// hybridVectorLeg runs the hybrid search's vector leg: the default
// embedding model is read per query, and a leg that cannot run reports why
// instead of failing the search, unless cfg.FallbackToFTS is off.
func (s *Service) hybridVectorLeg(ctx context.Context, query string, filter storage.ObjectFilter, sem retrieval.SemanticSource, cfg config.SearchConfig) ([]*storage.KnowledgeObject, retrieval.SemanticReport, error) {
	vecFilter := filter
	vecFilter.Limit = vectorPool(cfg)
	res, rep, err := sem.Search(ctx, s.Store(ctx), search.DecomposeQuery(query, 2), vecFilter)
	if err != nil {
		return nil, rep, fmt.Errorf("hybrid search vector leg: %w", err)
	}
	if !rep.OK() && !cfg.FallbackToFTS {
		return nil, rep, fmt.Errorf("hybrid search: %w", &SemanticUnavailableError{Report: rep})
	}
	return res, rep, nil
}

// vectorPool is the effective vector-leg candidate pool size.
func vectorPool(cfg config.SearchConfig) int {
	if cfg.CandidatePool.Vector > 0 {
		return cfg.CandidatePool.Vector
	}
	return 50
}

// EnsureDefaultRegistry caches the bundled default registry manifest if no cache entry
// exists for DefaultRegistryURL yet. Safe to call on every startup; idempotent.
// A read that fails for any other reason is returned: the entry may be
// there, and nothing is rewritten over it.
func (s *Service) EnsureDefaultRegistry(ctx context.Context) error {
	_, err := s.Store(ctx).Registries().GetCachedManifest(ctx, registrysync.DefaultRegistryURL)
	switch {
	case err == nil:
		return nil // already cached
	case !errors.Is(err, storage.ErrNotFound):
		return fmt.Errorf("read default registry: %w", err)
	}

	cache, err := registrysync.DefaultRegistryCache()
	if err != nil {
		return fmt.Errorf("load default registry: %w", err)
	}
	if err := s.Store(ctx).Registries().CacheManifest(ctx, cache); err != nil {
		return fmt.Errorf("cache default registry: %w", err)
	}
	return nil
}

// CheckRegistryCapabilities returns capability warnings for the named registry.
// clientVersion should be the running ctxt version (e.g. "0.5.0" or "dev").
// requiredFeatures is a subset of: "entity_sync", "taxonomy", "translations".
func (s *Service) CheckRegistryCapabilities(
	ctx context.Context,
	registryURL string,
	clientVersion string,
	requiredFeatures ...string,
) ([]registrysync.CapabilityWarning, error) {
	cache, err := s.Store(ctx).Registries().GetCachedManifest(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("get registry manifest: %w", err)
	}
	if cache.Manifest == nil {
		return nil, nil
	}
	warnings := registrysync.CheckCapabilities(cache.Manifest, clientVersion, requiredFeatures...)
	return warnings, nil
}

// SearchRemoteBookmarks queries all configured registries for bookmarks matching
// query using parallel scatter/gather. Per-registry timeout is enforced by ctx
// (callers should set a deadline). Failed registries are logged and skipped
// (graceful degradation). Returns merged, de-duplicated KnowledgeObject candidates
// from all reachable registries; the slice is nil when no registries are configured.
func (s *Service) SearchRemoteBookmarks(
	ctx context.Context,
	query string,
	timeout time.Duration,
) ([]*storage.KnowledgeObject, []registrysync.SourceResult, error) {
	urls := make([]string, 0, len(s.Cfg.Registries))
	for _, r := range s.Cfg.Registries {
		if r.URL != "" {
			urls = append(urls, r.URL)
		}
	}

	if len(urls) == 0 {
		return nil, nil, nil
	}

	client := registrysync.NewBookmarkSearchClient(timeout).WithCredentials(s.registryAuth)
	raw := registrysync.ScatterGather(ctx, client, urls, query)
	merged := registrysync.MergeResults(raw)
	return merged, raw, nil
}

// RecordMeteringEvent records a metering event for a paid registry access.
// registryName is the human-readable registry name (from config.RegistryConfig.Name).
// If the store call fails the error is returned; quota enforcement is advisory.
func (s *Service) RecordMeteringEvent(
	ctx context.Context,
	registryName string,
	eventType storage.MeteringEventType,
	namespace string,
) error {
	event := &storage.MeteringEvent{
		ID:           uuid.New().String(),
		RegistryName: registryName,
		EventType:    eventType,
		Namespace:    namespace,
		Count:        1,
		OccurredAt:   time.Now().UTC(),
	}
	return s.Store(ctx).Metering().Record(ctx, event)
}

// RegistryUsageSummary returns aggregated metering counts per event type for
// the given registry in the current billing period. periodStart == zero means
// all-time.
func (s *Service) RegistryUsageSummary(
	ctx context.Context,
	registryName string,
	periodStart time.Time,
) ([]*storage.MeteringAggregate, error) {
	f := storage.MeteringFilter{
		RegistryName: registryName,
		After:        periodStart,
	}
	return s.Store(ctx).Metering().Aggregate(ctx, f)
}

// AllRegistriesUsageSummary returns aggregated metering counts for all registries.
func (s *Service) AllRegistriesUsageSummary(
	ctx context.Context,
	periodStart time.Time,
) ([]*storage.MeteringAggregate, error) {
	f := storage.MeteringFilter{After: periodStart}
	return s.Store(ctx).Metering().Aggregate(ctx, f)
}

func (s *Service) parsePipelineSteps(stepsJSON string) ([]storage.StepRef, error) {
	var steps []map[string]any
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		return nil, err
	}

	// nil when no entry names a step: the pipeline stores steps as null.
	var result []storage.StepRef //nolint:prealloc // see above; preallocating would store []
	for _, step := range steps {
		name, ok := step["name"].(string)
		if !ok {
			name, ok = step["type"].(string)
			if !ok {
				continue
			}
		}
		config, _ := step["config"].(map[string]any)
		result = append(result, storage.StepRef{
			Name:   name,
			Config: config,
		})
	}

	return result, nil
}
