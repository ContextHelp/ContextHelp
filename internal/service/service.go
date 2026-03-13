package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	pipelinesteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Service coordinates all business operations.
type Service struct {
	Store     storage.StorageDriver
	Queue     *jobs.Queue
	Pipes     pipeline.Registry
	Search    *search.Engine
	Discovery *steps.StepDiscovery
	Executor  *steps.StepExecutor
}

// New creates a new service instance.
func New(store storage.StorageDriver, queue *jobs.Queue, pipes pipeline.Registry, engine *search.Engine, stepsPath string) *Service {
	discovery := steps.NewStepDiscovery(store, stepsPath)
	executor := steps.NewStepExecutor(store, stepsPath)

	return &Service{
		Store:     store,
		Queue:     queue,
		Pipes:     pipes,
		Search:    engine,
		Discovery: discovery,
		Executor:  executor,
	}
}

// Analyze enqueues a content analysis job and returns the job ID.
func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (string, error) {
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
	return s.Store.Objects().Update(ctx, obj)
}

// DeleteObject deletes a knowledge object and its edges.
func (s *Service) DeleteObject(ctx context.Context, id string) error {
	if err := s.Store.Edges().DeleteByObject(ctx, id); err != nil {
		return err
	}
	return s.Store.Objects().Delete(ctx, id)
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
func (s *Service) EntityBacklinks(ctx context.Context, slug string) ([]*storage.KnowledgeObject, error) {
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
	// Check database first (user-created pipelines).
	p, err := s.Store.Pipelines().Get(ctx, name)
	if err == nil {
		return p, nil
	}

	// Fall back to in-memory built-in registry.
	bp, bpErr := s.Pipes.Get(name)
	if bpErr != nil {
		return nil, err // return original storage error
	}
	return builtInToStorage(name, bp), nil
}

func (s *Service) ListPipelines(ctx context.Context, filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	// Fetch user-created pipelines from storage.
	dbPipelines, count, err := s.Store.Pipelines().List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Track database pipeline names to avoid duplicates.
	seen := make(map[string]bool, len(dbPipelines))
	for _, p := range dbPipelines {
		seen[p.Name] = true
	}

	// Prepend built-in pipelines that match the filter.
	for _, name := range s.Pipes.List() {
		if seen[name] {
			continue
		}
		if filter.Name != "" && !strings.Contains(name, filter.Name) {
			continue
		}
		bp, _ := s.Pipes.Get(name)
		dbPipelines = append([]*storage.Pipeline{builtInToStorage(name, bp)}, dbPipelines...)
		count++
	}

	return dbPipelines, count, nil
}

func builtInToStorage(name string, p *pipeline.Pipeline) *storage.Pipeline {
	stepRefs := make([]storage.StepRef, len(p.Steps))
	for i, s := range p.Steps {
		stepRefs[i] = storage.StepRef{Name: s.Name()}
	}
	return &storage.Pipeline{
		Name:        name,
		Description: p.Description,
		Steps:       stepRefs,
		IsBuiltIn:   true,
	}
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

func (s *Service) ListReminders(ctx context.Context, activeOnly bool) ([]*storage.SystemReminder, int, error) {
	return s.Store.Reminders().List(ctx, activeOnly)
}

func (s *Service) DismissReminder(ctx context.Context, id string) error {
	return s.Store.Reminders().Dismiss(ctx, id)
}

// FindByText searches knowledge objects by text matching on summaries and raw content.
func (s *Service) FindByText(ctx context.Context, query string, limit int) ([]*storage.KnowledgeObject, error) {
	all, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{Limit: 10000})
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []*storage.KnowledgeObject
	for _, obj := range all {
		for _, summary := range obj.Summaries {
			if strings.Contains(strings.ToLower(summary), q) {
				results = append(results, obj)
				break
			}
		}
		if len(results) > 0 && results[len(results)-1] == obj {
			continue
		}
		if strings.Contains(strings.ToLower(obj.RawContent), q) {
			results = append(results, obj)
		}
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// SemanticSearch finds objects by vector cosine similarity.
// It embeds the query using the provided EmbeddingProvider, then loads all
// stored embeddings and ranks them in memory.
func (s *Service) SemanticSearch(ctx context.Context, query string, limit int, ep providers.EmbeddingProvider) ([]*storage.KnowledgeObject, error) {
	queryVec, err := ep.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVec) == 0 {
		return nil, fmt.Errorf("embedding provider returned empty vector")
	}

	rows, err := s.Store.Objects().ListWithEmbeddings(ctx)
	if err != nil {
		return nil, err
	}

	type scored struct {
		obj   *storage.KnowledgeObject
		score float64
	}
	var results []scored
	for _, row := range rows {
		if len(row.Embeddings) == 0 {
			continue
		}
		score := pipelinesteps.CosineSimilarity(queryVec, row.Embeddings)
		results = append(results, scored{row, score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	out := make([]*storage.KnowledgeObject, 0, limit)
	for i, r := range results {
		if i >= limit {
			break
		}
		out = append(out, r.obj)
	}
	return out, nil
}

// CreateFeed creates a new feed subscription with status=active.
func (s *Service) CreateFeed(ctx context.Context, url string) (*storage.Feed, error) {
	now := time.Now().Truncate(time.Second)
	feed := &storage.Feed{
		ID:        uuid.New().String(),
		URL:       url,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
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

// DeleteFeed deletes a feed subscription by ID.
func (s *Service) DeleteFeed(ctx context.Context, id string) error {
	if _, err := s.Store.Feeds().Get(ctx, id); err != nil {
		return fmt.Errorf("not found: %w", err)
	}
	return s.Store.Feeds().Delete(ctx, id)
}

// SyncFeed enqueues a feed.sync job for the given feed ID.
func (s *Service) SyncFeed(ctx context.Context, feedID string) (string, error) {
	feed, err := s.Store.Feeds().Get(ctx, feedID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}
	now := time.Now().Truncate(time.Second)
	job := &storage.Job{
		ID:         uuid.New().String(),
		Type:       "feed:sync",
		Status:     storage.JobPending,
		Payload:    feed.URL,
		Pipeline:   "feed.sync",
		Source:     feed.URL,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Queue.Enqueue(ctx, job); err != nil {
		return "", err
	}
	return job.ID, nil
}

// CreateBatch creates a new batch import record.
func (s *Service) CreateBatch(ctx context.Context, content, format string) (*storage.Batch, error) {
	now := time.Now().Truncate(time.Second)
	batch := &storage.Batch{
		ID:        uuid.New().String(),
		Format:    format,
		Status:    "processing",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Store.Batches().Create(ctx, batch); err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}
	return batch, nil
}

// GetBatch retrieves a batch import by ID.
func (s *Service) GetBatch(ctx context.Context, id string) (*storage.Batch, error) {
	return s.Store.Batches().Get(ctx, id)
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

// CreateDetector persists a new detector configuration.
func (s *Service) CreateDetector(ctx context.Context, req DetectorCreateRequest) (*storage.DetectorRecord, error) {
	now := time.Now().Truncate(time.Second)
	d := &storage.DetectorRecord{
		ID:           uuid.New().String(),
		Kind:         req.Kind,
		Name:         req.Name,
		PipelineName: req.PipelineName,
		Pattern:      req.Pattern,
		Priority:     req.Priority,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if d.Priority == 0 {
		d.Priority = 100
	}
	if err := s.Store.Detectors().Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create detector: %w", err)
	}
	return d, nil
}

// ListDetectors returns detectors matching the filter.
func (s *Service) ListDetectors(ctx context.Context, filter storage.DetectorFilter) ([]*storage.DetectorRecord, error) {
	ds, _, err := s.Store.Detectors().List(ctx, filter)
	return ds, err
}

// DeleteDetector removes a detector by ID.
func (s *Service) DeleteDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Delete(ctx, id)
}

// EnableDetector enables a detector by ID.
func (s *Service) EnableDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Enable(ctx, id)
}

// DisableDetector disables a detector by ID.
func (s *Service) DisableDetector(ctx context.Context, id string) error {
	return s.Store.Detectors().Disable(ctx, id)
}
