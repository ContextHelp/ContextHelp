package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Reprocess: re-run one enrichment step on a stored object
// (POST /api/v1/objects/{id}/reprocess). The step runs as a task job on
// the dpkms host, with that host's providers and configuration, and
// writes its result back to the object.

// ReprocessJobType is the task job type of a reprocess.
const ReprocessJobType = "object:reprocess"

// reprocessMaxRetries bounds automatic retries of a step that errors.
const reprocessMaxRetries = 2

// Steps a reprocess job runs.
const (
	StepStructuredMetadata = "structured_metadata"
	StepEntityExtractor    = "entity_extractor"
	StepTagger             = "tagger"
)

// ReprocessSteps lists every step a reprocess job runs.
var ReprocessSteps = []string{StepStructuredMetadata, StepEntityExtractor, StepTagger}

// ErrUnknownReprocessStep is returned for a step outside ReprocessSteps.
var ErrUnknownReprocessStep = fmt.Errorf("unknown reprocess step; choose from %s", strings.Join(ReprocessSteps, ", "))

// ReprocessPayload is a reprocess job's payload.
type ReprocessPayload struct {
	ObjectID string `json:"object_id"`
	Step     string `json:"step"`
}

// EnqueueReprocess enqueues a reprocess job running step on objectID.
// It does not check that the object exists; the job fails if it doesn't.
func (q *Queue) EnqueueReprocess(ctx context.Context, objectID, step string) (*storage.Job, error) {
	if !slices.Contains(ReprocessSteps, step) {
		return nil, fmt.Errorf("enqueue reprocess %q: %w", step, ErrUnknownReprocessStep)
	}
	if objectID == "" {
		return nil, errors.New("enqueue reprocess: object ID is required")
	}
	payload, err := json.Marshal(ReprocessPayload{ObjectID: objectID, Step: step})
	if err != nil {
		return nil, fmt.Errorf("enqueue reprocess: %w", err)
	}
	return q.EnqueueTask(ctx, ReprocessJobType, string(payload), reprocessMaxRetries)
}

// ReprocessObjects reads and writes the objects a reprocess job works on.
// *service.Service implements it, so an update publishes the usual
// object-updated event.
type ReprocessObjects interface {
	GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error)
	UpdateObject(ctx context.Context, obj *storage.KnowledgeObject) error
}

// Reprocessor runs reprocess jobs.
type Reprocessor struct {
	Objects ReprocessObjects
	// LLM is the provider the steps use: the dpkms host's configured LLM.
	LLM providers.LLMProvider
}

type enrichStep interface {
	Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}

func (r *Reprocessor) step(name string) (enrichStep, error) {
	switch name {
	case StepStructuredMetadata:
		return steps.NewStructuredMetadataExtractorWithLLM(r.LLM), nil
	case StepEntityExtractor:
		return steps.NewEntityExtractorWithLLM(r.LLM), nil
	case StepTagger:
		return steps.NewTaggerWithLLM(r.LLM), nil
	default:
		return nil, fmt.Errorf("step %q: %w", name, ErrUnknownReprocessStep)
	}
}

// Handle is the TaskHandler for ReprocessJobType. The result is the
// object ID. A payload that can never run (unreadable, an unknown step,
// a missing object) fails the job without a retry.
func (r *Reprocessor) Handle(ctx context.Context, job *storage.Job) (string, error) {
	var p ReprocessPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		return "", pipeline.Permanent(fmt.Errorf("reprocess: decode payload: %w", err))
	}
	s, err := r.step(p.Step)
	if err != nil {
		return "", pipeline.Permanent(fmt.Errorf("reprocess %s: %w", p.ObjectID, err))
	}
	obj, err := r.Objects.GetObject(ctx, p.ObjectID)
	if errors.Is(err, storage.ErrNotFound) {
		return "", pipeline.Permanent(fmt.Errorf("reprocess %s: %w", p.ObjectID, err))
	}
	if err != nil {
		return "", fmt.Errorf("reprocess %s: get object: %w", p.ObjectID, err)
	}
	out, err := s.Run(ctx, obj)
	if err != nil {
		return "", fmt.Errorf("reprocess %s: step %s: %w", p.ObjectID, p.Step, err)
	}
	out.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := r.Objects.UpdateObject(ctx, out); err != nil {
		return "", fmt.Errorf("reprocess %s: update object: %w", p.ObjectID, err)
	}
	return out.ID, nil
}
