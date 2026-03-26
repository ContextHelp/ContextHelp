package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/citation"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Service coordinates all business operations.
type Service struct {
	Store          storage.StorageDriver
	Queue          *jobs.Queue
	Pipes          pipeline.Registry
	Search         *search.Engine
	Discovery      *steps.StepDiscovery
	Executor       *steps.StepExecutor
	Bus            events.Bus
	Cfg            config.Config
	PluginRegistry *plugin.Registry
}

// New creates a new service instance.
func New(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string, bus events.Bus, cfg ...config.Config) *Service {
	discovery := steps.NewStepDiscovery(store, stepsPath)
	executor := steps.NewStepExecutor(store, stepsPath)

	if bus == nil {
		bus = events.NewLocalBus()
	}

	var c config.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	return &Service{
		Store:     store,
		Queue:     queue,
		Pipes:     pipes,
		Search:    engine,
		Discovery: discovery,
		Executor:  executor,
		Bus:       bus,
		Cfg:       c,
	}
}

// Analyze enqueues a content analysis job and returns the job ID.
// When req.Raw is true, skips AI enrichment and stores the object immediately
// with Status "raw"; returns the object ID (not a job ID).
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
		obj := &storage.KnowledgeObject{
			ID:          uuid.New().String(),
			Type:        detectedType,
			RawContent:  req.Content,
			Source:      jobSource,
			ContentHash: storageutil.ContentHash(req.Content, jobSource),
			Status:      "raw",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.Store.Objects().Create(ctx, obj); err != nil {
			return "", fmt.Errorf("analyze raw: store: %w", err)
		}
		if ev, err := events.NewEvent("service.analyze", "object.raw_stored", obj); err == nil {
			_ = s.Bus.Publish(ctx, ev)
		}
		return obj.ID, nil
	}

	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.Pipes.SelectPipeline(req.Content)
	}

	// Duplicate detection (exact match only at analyze time; embeddings not yet computed).
	dup, err := s.checkDuplicates(ctx, req.KnownHash, nil, s.Cfg.Duplicates)
	if err != nil {
		return "", fmt.Errorf("analyze: duplicate check: %w", err)
	}
	if dup != nil {
		switch s.Cfg.Duplicates.Policy {
		case "drop":
			// Return the existing object's ID — no new job enqueued.
			return dup.Existing.ID, nil
		case "warn":
			fmt.Fprintf(os.Stderr, "warning: duplicate detected (%s): existing object %s\n",
				dup.Kind, dup.Existing.ID)
			// Fall through — continue ingestion.
		case "keep":
			// Fall through silently.
		}
	}

	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       "ingest:" + detectedType,
		Status:     storage.JobPending,
		Payload:    req.Content,
		Pipeline:   pipelineName,
		Source:     jobSource,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.Queue.Enqueue(ctx, job); err != nil {
		return "", err
	}
	if ev, err := events.NewEvent("service.analyze", "job.enqueued", job); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

// GetObject retrieves a knowledge object by ID.
func (s *Service) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return s.Store.Objects().Get(ctx, id)
}

// ListObjects lists knowledge objects matching the filter.
func (s *Service) ListObjects(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	return s.Store.Objects().List(ctx, filter)
}

// UpdateObject updates a knowledge object.
func (s *Service) UpdateObject(ctx context.Context, obj *storage.KnowledgeObject) error {
	if err := s.Store.Objects().Update(ctx, obj); err != nil {
		return err
	}
	if ev, err := events.NewEvent("service.objects", "object.updated", map[string]string{"id": obj.ID}); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return nil
}

// DeleteObject deletes a knowledge object and its edges.
func (s *Service) DeleteObject(ctx context.Context, id string) error {
	if err := s.Store.Edges().DeleteByObject(ctx, id); err != nil {
		return err
	}
	if err := s.Store.Objects().Delete(ctx, id); err != nil {
		return err
	}
	if ev, err := events.NewEvent("service.objects", "object.deleted", map[string]string{"id": id}); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return nil
}

// SearchObjects executes an RSQL query and returns matching objects.
func (s *Service) SearchObjects(ctx context.Context, query string, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
	return s.Search.Search(ctx, query, limit, offset)
}

// GetJob retrieves a job by ID.
func (s *Service) GetJob(ctx context.Context, id string) (*storage.Job, error) {
	return s.Queue.Get(ctx, id)
}

// ListJobs lists jobs matching the filter.
func (s *Service) ListJobs(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	return s.Queue.List(ctx, filter)
}

// RetryJob retries a failed job.
func (s *Service) RetryJob(ctx context.Context, id string) error {
	return s.Queue.Retry(ctx, id)
}

// GetEntity retrieves an entity by slug.
func (s *Service) GetEntity(ctx context.Context, slug string) (*storage.Entity, error) {
	return s.Store.Entities().Get(ctx, slug)
}

// ListEntities lists entities matching the filter.
func (s *Service) ListEntities(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	return s.Store.Entities().List(ctx, filter)
}

// EntityBacklinks returns objects that mention the given entity.
// slug may be in @namespace.id mention format or ctxt:// URI format.
func (s *Service) EntityBacklinks(ctx context.Context, slug string) ([]*storage.KnowledgeObject, error) {
	// Normalize slug to the edge storage format (ctxt:// URI string).
	if u, ok := mentions.Parse(slug); ok {
		slug = u.String()
	}
	edges, err := s.Store.Edges().ListTo(ctx, "entity", slug)
	if err != nil {
		return nil, err
	}

	var objs []*storage.KnowledgeObject
	for _, edge := range edges {
		if edge.FromType == "object" {
			obj, err := s.Store.Objects().Get(ctx, edge.FromID)
			if err != nil {
				continue
			}
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

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

	if err := s.Store.Pipelines().Create(ctx, pipeline); err != nil {
		return "", fmt.Errorf("create pipeline: %w", err)
	}

	return pipeline.ID, nil
}

func (s *Service) GetPipeline(ctx context.Context, name string) (*storage.Pipeline, error) {
	return s.Store.Pipelines().Get(ctx, name)
}

func (s *Service) ListPipelines(ctx context.Context, filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	return s.Store.Pipelines().List(ctx, filter)
}

func (s *Service) DeletePipeline(ctx context.Context, name string) error {
	if _, err := s.Pipes.Get(name); err == nil {
		return fmt.Errorf("PROTECTED: cannot delete built-in pipeline")
	}

	if _, err := s.Store.Pipelines().Get(ctx, name); err != nil {
		return err
	}

	return s.Store.Pipelines().Delete(ctx, name)
}

func (s *Service) ArchivePipeline(ctx context.Context, name string) error {
	return s.Store.Pipelines().Archive(ctx, name)
}

func (s *Service) UnarchivePipeline(ctx context.Context, name string) error {
	return s.Store.Pipelines().Unarchive(ctx, name)
}

func (s *Service) Enqueue(ctx context.Context, req AnalyzeRequest) (string, error) {
	now := time.Now().Truncate(time.Second)
	pipelineName := req.Pipeline
	if pipelineName == "" {
		pipelineName = s.Pipes.SelectPipeline(req.Content)
	}

	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       "ingest:" + req.Type,
		Status:     storage.JobPending,
		Payload:    req.Content,
		Pipeline:   pipelineName,
		Source:     req.Source,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.Queue.Enqueue(ctx, job); err != nil {
		return "", err
	}
	if ev, err := events.NewEvent("service.analyze", "job.enqueued", job); err == nil {
		_ = s.Bus.Publish(ctx, ev)
	}
	return job.ID, nil
}

func (s *Service) ListSteps(ctx context.Context, source string) ([]*storage.RegisteredStep, int, error) {
	return s.Store.Steps().List(ctx, source)
}

func (s *Service) GetStep(ctx context.Context, name string) (*storage.RegisteredStep, error) {
	return s.Store.Steps().Get(ctx, name)
}

func (s *Service) InstallStep(ctx context.Context, name, fromRegistry string) error {
	return s.Discovery.InstallStep(ctx, name, fromRegistry)
}

func (s *Service) UninstallStep(ctx context.Context, name string) error {
	return s.Store.Steps().Unregister(ctx, name)
}

func (s *Service) FetchRegistry(ctx context.Context, url string) error {
	_, err := s.Discovery.FetchRegistryManifest(ctx, url)
	return err
}

func (s *Service) UpdateRegistry(ctx context.Context, url string) error {
	return s.Discovery.NotifyUpdateAvailable(ctx, url, "")
}

func (s *Service) ListRegistries(ctx context.Context) ([]*storage.RegistryCache, int, error) {
	return s.Store.Registries().List(ctx)
}

func (s *Service) RemoveRegistry(ctx context.Context, url string) error {
	return s.Store.Registries().Delete(ctx, url)
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
	cache, err := s.Store.Registries().GetCachedManifest(ctx, url)
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
	return s.Store.Reminders().List(ctx, activeOnly)
}

func (s *Service) DismissReminder(ctx context.Context, id string) error {
	return s.Store.Reminders().Dismiss(ctx, id)
}

// FindByText searches knowledge objects by text matching on summaries and raw content.
// FindByText searches knowledge objects using FTS5 full-text search.
func (s *Service) FindByText(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	return s.Store.Objects().FTSSearch(ctx, query, storage.ObjectFilter{Limit: limit})
}

// CancelJob cancels a pending or running job.
func (s *Service) CancelJob(ctx context.Context, id string) error {
	return s.Store.Jobs().Cancel(ctx, id)
}

// SearchEntities searches entities by slug/title prefix matching.
func (s *Service) SearchEntities(ctx context.Context, query string, limit int) ([]*storage.Entity, error) {
	all, err := s.Store.Entities().List(ctx, storage.EntityFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []*storage.Entity
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Slug), q) ||
			strings.Contains(strings.ToLower(e.Title), q) {
			results = append(results, e)
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

// SyncRegistryEntities syncs entity index from a configured registry.
// Uses the registry's sync_mode (thin or full). Returns count of upserted entities.
func (s *Service) SyncRegistryEntities(ctx context.Context, registryURL string) (int, error) {
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

	syncer := registrysync.New(s.Store.Entities())
	result, err := syncer.Sync(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("sync registry entities: %w", err)
	}
	return result.Upserted, nil
}

// PullEntity promotes a thin entity to full by fetching its complete definition
// from its source registry. Returns ErrEntityDefinitionUnavailable if not thin.
func (s *Service) PullEntity(ctx context.Context, slug string) (*storage.Entity, error) {
	entity, err := s.Store.Entities().Get(ctx, slug)
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
	if err := s.Store.Entities().SetContentStatus(ctx, slug, storage.ContentStatusPendingPull); err != nil {
		return nil, fmt.Errorf("set pending_pull: %w", err)
	}

	// Fetch full definition from registry.
	fullEntity, err := s.fetchRemoteEntityDefinition(ctx, entity.RegistryURL, slug)
	if err != nil {
		// Restore thin status on failure so the entity remains usable.
		_ = s.Store.Entities().SetContentStatus(ctx, slug, storage.ContentStatusThin)
		return nil, fmt.Errorf("fetch entity definition: %w", err)
	}

	fullEntity.Slug = entity.Slug
	fullEntity.VersionHash = entity.VersionHash
	fullEntity.RegistryURL = entity.RegistryURL
	fullEntity.ContentStatus = storage.ContentStatusFull
	fullEntity.CreatedAt = entity.CreatedAt
	fullEntity.UpdatedAt = time.Now().Truncate(time.Second)

	if err := s.Store.Entities().Upsert(ctx, fullEntity); err != nil {
		_ = s.Store.Entities().SetContentStatus(ctx, slug, storage.ContentStatusThin)
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

// Compose generates a markdown composition from knowledge objects.
// This is a fallback implementation that concatenates objects.
func (s *Service) Compose(ctx context.Context, objects []*storage.KnowledgeObject, compositionType string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", compositionType)
	fmt.Fprintf(&b, "Generated from %d knowledge objects.\n\n", len(objects))

	for _, obj := range objects {
		fmt.Fprintf(&b, "## %s\n", obj.ID)
		if len(obj.Summaries) > 0 {
			b.WriteString(obj.Summaries[0])
			b.WriteString("\n\n")
		} else if obj.RawContent != "" {
			content := obj.RawContent
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			b.WriteString(content)
			b.WriteString("\n\n")
		}
	}
	return b.String(), nil
}

// ComposeWithCitations generates a markdown composition from knowledge objects
// with inline [ref:ID] citation markers and an appended reference table.
//
// The fallback implementation injects citation markers after each object's
// content snippet. When an AI provider is wired up it should emit markers
// itself; this layer then parses and validates them regardless of origin.
func (s *Service) ComposeWithCitations(ctx context.Context, objects []*storage.KnowledgeObject, compositionType string) (*CompositionResult, error) {
	cctx := citation.NewCompositionContext(objects)

	// Collect source IDs.
	sourceIDs := make([]string, 0, len(objects))
	for _, obj := range objects {
		sourceIDs = append(sourceIDs, obj.ID)
	}

	// Build citation-annotated body.
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", compositionType)
	fmt.Fprintf(&b, "Generated from %d knowledge objects.\n\n", len(objects))

	for _, obj := range objects {
		fmt.Fprintf(&b, "## %s\n", obj.ID)
		if len(obj.Summaries) > 0 {
			fmt.Fprintf(&b, "%s [ref:%s]\n\n", obj.Summaries[0], obj.ID)
		} else if obj.RawContent != "" {
			content := obj.RawContent
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&b, "%s [ref:%s]\n\n", content, obj.ID)
		} else {
			fmt.Fprintf(&b, "[ref:%s]\n\n", obj.ID)
		}
	}

	body := b.String()

	// Parse citations from generated content.
	citations := citation.ParseCitations(body)

	// Enrich citations with entity mentions.
	citation.EnrichCitationsWithEntities(citations, cctx.ObjectMap)

	// Append reference table.
	refTable := citation.BuildReferenceTable(citations, cctx.ObjectMap)
	content := body + refTable

	return &CompositionResult{
		Type:        compositionType,
		Content:     content,
		Citations:   citations,
		SourceIDs:   sourceIDs,
		GeneratedAt: time.Now(),
	}, nil
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
	if err := s.Store.Feeds().Create(ctx, feed); err != nil {
		return nil, fmt.Errorf("create feed: %w", err)
	}
	return feed, nil
}

// ListFeeds returns all feed subscriptions matching the filter.
func (s *Service) ListFeeds(ctx context.Context, filter storage.FeedFilter) ([]*storage.Feed, error) {
	return s.Store.Feeds().List(ctx, filter)
}

// SyncFeed enqueues a feed.sync pipeline job to fetch and process feed items.
// Deduplication and item enqueueing are handled by the pipeline steps.
func (s *Service) SyncFeed(ctx context.Context, id string) (string, error) {
	feed, err := s.Store.Feeds().Get(ctx, id)
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

// parseFeedItems parses RSS, Atom, or JSON Feed content and returns items.
func parseFeedItems(content string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(content)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		var doc struct {
			Items []struct {
				ID          string `json:"id"`
				URL         string `json:"url"`
				Title       string `json:"title"`
				ContentHTML string `json:"content_html"`
				ContentText string `json:"content_text"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(content), &doc); err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(doc.Items))
		for _, it := range doc.Items {
			body := it.ContentHTML
			if body == "" {
				body = it.ContentText
			}
			items = append(items, map[string]any{"guid": it.ID, "title": it.Title, "link": it.URL, "content": body})
		}
		return items, nil
	case strings.Contains(trimmed, "<rss"):
		var root struct {
			Channel struct {
				Items []struct {
					GUID        string `xml:"guid"`
					Title       string `xml:"title"`
					Link        string `xml:"link"`
					Description string `xml:"description"`
				} `xml:"item"`
			} `xml:"channel"`
		}
		if err := xml.Unmarshal([]byte(content), &root); err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(root.Channel.Items))
		for _, it := range root.Channel.Items {
			items = append(items, map[string]any{"guid": it.GUID, "title": it.Title, "link": it.Link, "content": it.Description})
		}
		return items, nil
	case strings.Contains(trimmed, "<feed"):
		var feed struct {
			Entries []struct {
				ID      string `xml:"id"`
				Title   string `xml:"title"`
				Summary string `xml:"summary"`
				Content string `xml:"content"`
				Links   []struct {
					Href string `xml:"href,attr"`
				} `xml:"link"`
			} `xml:"entry"`
		}
		if err := xml.Unmarshal([]byte(content), &feed); err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(feed.Entries))
		for _, e := range feed.Entries {
			body := e.Content
			if body == "" {
				body = e.Summary
			}
			link := ""
			if len(e.Links) > 0 {
				link = e.Links[0].Href
			}
			items = append(items, map[string]any{"guid": e.ID, "title": e.Title, "link": link, "content": body})
		}
		return items, nil
	}
	return nil, fmt.Errorf("unrecognized feed format")
}

// SyncAllFeeds enqueues sync jobs for all active feeds.
func (s *Service) SyncAllFeeds(ctx context.Context) ([]string, error) {
	feeds, err := s.Store.Feeds().List(ctx, storage.FeedFilter{Status: "active"})
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	var jobIDs []string
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
	if _, err := s.Store.Feeds().Get(ctx, id); err != nil {
		return fmt.Errorf("feed not found: %w", err)
	}
	return s.Store.Feeds().Delete(ctx, id)
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
	if err := s.Store.Batches().Create(ctx, batch); err != nil {
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
		_ = s.Store.Edges().Create(ctx, &storage.Edge{
			ID:        uuid.New().String(),
			FromType:  "batch",
			FromID:    batch.ID,
			ToType:    "job",
			ToID:      jobID,
			EdgeType:  "batch_contains",
			CreatedAt: time.Now(),
		})
	}

	if batch.Failed == 0 {
		batch.Status = "completed"
	} else if batch.Failed < batch.TotalRecords {
		batch.Status = "partial"
	} else {
		batch.Status = "failed"
	}
	batch.Completed = batch.TotalRecords - batch.Failed
	batch.UpdatedAt = time.Now()
	_ = s.Store.Batches().Update(ctx, batch)

	return batch, nil
}

// parseBatchRecords parses JSONL, CSV, or TSV content into ImportRecords.
func (s *Service) parseBatchRecords(content, format string) ([]storage.ImportRecord, []storage.BatchError) {
	var records []storage.ImportRecord
	var errs []storage.BatchError

	switch format {
	case "jsonl", "":
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
	Type    string        `xml:"type,attr"`
	Text    string        `xml:"text,attr"`
	XMLUrl  string        `xml:"xmlUrl,attr"`
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
	if err := s.Store.Batches().Create(ctx, batch); err != nil {
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
	_ = s.Store.Batches().Update(ctx, batch)

	return batch, nil
}

// GetBatch retrieves a batch import operation by ID.
func (s *Service) GetBatch(ctx context.Context, id string) (*storage.Batch, error) {
	return s.Store.Batches().Get(ctx, id)
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
	if err := s.Store.Detectors().Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create detector: %w", err)
	}
	return d, nil
}

// ListDetectors returns all detectors matching the filter.
func (s *Service) ListDetectors(ctx context.Context, filter storage.DetectorFilter) ([]*storage.DetectorRecord, error) {
	detectors, _, err := s.Store.Detectors().List(ctx, filter)
	return detectors, err
}

// DeleteDetector removes a detector.
func (s *Service) DeleteDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Delete(ctx, id)
}

// EnableDetector enables a detector.
func (s *Service) EnableDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Enable(ctx, id)
}

// DisableDetector disables a detector.
func (s *Service) DisableDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Disable(ctx, id)
}

// --- Semantic search ---

// SemanticSearch performs vector similarity search using the provided embedding provider.
func (s *Service) SemanticSearch(ctx context.Context, query string, limit int, ep providers.EmbeddingProvider) ([]*storage.KnowledgeObject, error) {
	vec, err := ep.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return s.Store.Objects().VectorSearch(ctx, vec, storage.ObjectFilter{Limit: limit})
}

// HybridSearch runs FTS and vector search concurrently, merges results with
// Reciprocal Rank Fusion (RRF), and returns the top-limit objects.
// If ep is nil and cfg.FallbackToFTS is true, degrades to FTS-only.
// If ep is nil and cfg.FallbackToFTS is false, returns an error.
func (s *Service) HybridSearch(ctx context.Context, query string, limit int, ep providers.EmbeddingProvider, cfg config.SearchConfig) ([]*storage.KnowledgeObject, error) {
	k := cfg.RRF.K
	if k <= 0 {
		k = 60
	}

	ftsPool := cfg.CandidatePool.FTS
	if ftsPool <= 0 {
		ftsPool = 50
	}

	type legResult struct {
		results []*storage.KnowledgeObject
		err     error
	}

	ftsCh := make(chan legResult, 1)
	go func() {
		res, err := s.Store.Objects().FTSSearch(ctx, query, storage.ObjectFilter{Limit: ftsPool})
		ftsCh <- legResult{res, err}
	}()

	vecCh := make(chan legResult, 1)
	if ep != nil {
		vecPool := cfg.CandidatePool.Vector
		if vecPool <= 0 {
			vecPool = 50
		}
		go func() {
			vec, err := ep.Embed(ctx, query)
			if err != nil {
				vecCh <- legResult{nil, err}
				return
			}
			res, err := s.Store.Objects().VectorSearch(ctx, vec, storage.ObjectFilter{Limit: vecPool})
			vecCh <- legResult{res, err}
		}()
	} else {
		if !cfg.FallbackToFTS {
			<-ftsCh // drain
			return nil, fmt.Errorf("hybrid search: no embedding provider and fallback_to_fts is false")
		}
		vecCh <- legResult{nil, nil}
	}

	ftsRes := <-ftsCh
	vecRes := <-vecCh

	if ftsRes.err != nil {
		return nil, fmt.Errorf("hybrid search fts leg: %w", ftsRes.err)
	}
	if vecRes.err != nil {
		if !cfg.FallbackToFTS {
			return nil, fmt.Errorf("hybrid search vector leg: %w", vecRes.err)
		}
		// Vector leg failed but FallbackToFTS is true — degrade to FTS-only.
		vecRes.results = nil
	}

	scores := map[string]float64{}
	byID := map[string]*storage.KnowledgeObject{}

	addLeg := func(results []*storage.KnowledgeObject, weight float64) {
		for rank, obj := range results {
			scores[obj.ID] += weight * (1.0 / float64(k+rank+1))
			byID[obj.ID] = obj
		}
	}

	addLeg(ftsRes.results, cfg.RRF.FTSWeight)
	addLeg(vecRes.results, cfg.RRF.VectorWeight)

	type scored struct {
		id    string
		score float64
	}
	var merged []scored
	for id, score := range scores {
		if score >= cfg.MinScore {
			merged = append(merged, scored{id, score})
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].score > merged[j].score })

	if limit > len(merged) {
		limit = len(merged)
	}
	out := make([]*storage.KnowledgeObject, limit)
	for i := 0; i < limit; i++ {
		obj := byID[merged[i].id]
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["rrf_score"] = merged[i].score
		out[i] = obj
	}
	return out, nil
}

func (s *Service) parsePipelineSteps(stepsJSON string) ([]storage.StepRef, error) {
	var steps []map[string]any
	if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
		return nil, err
	}

	var result []storage.StepRef
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
