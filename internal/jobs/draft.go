package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// PipelineExecutor computes a Stage 1 KnowledgeObject draft from an acquired
// ingest job without writing to the job queue or object store. WorkerPool
// performs Stage 2 persistence (binary externalization, dedup, object creation,
// vector writes, mention edges, item fan-out, and queue settlement).
type PipelineExecutor interface {
	Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error)
}

// DraftJobRequest is the wire payload sent to a stateless pod worker
// (`dpkms worker run-draft`) to execute Stage 1 of an ingest job.
type DraftJobRequest struct {
	Job     storage.Job `json:"job"`
	MaxHops int         `json:"max_hops,omitempty"`
}

// DraftJobResult is the wire result returned by `dpkms worker run-draft`.
type DraftJobResult struct {
	Draft     *storage.KnowledgeObject `json:"draft,omitempty"`
	Permanent bool                     `json:"permanent,omitempty"`
	Error     string                   `json:"error,omitempty"`
}

// LocalExecutor runs Stage 1 pipeline execution in-process against a Registry.
type LocalExecutor struct {
	Pipelines pipeline.Registry
	MaxHops   int
}

// Execute runs Stage 1 draft computation locally in-process.
func (e *LocalExecutor) Execute(ctx context.Context, job *storage.Job) (*storage.KnowledgeObject, error) {
	return RunDraft(ctx, e.Pipelines, job, e.MaxHops)
}

// RunDraft executes Stage 1 of an ingest job: decodes job content, runs the
// initial pipeline and any `next_pipeline` hops up to maxHops, applies ambient
// metadata, and computes DraftContentHash. It performs no database or blob
// writes, making it safe to execute either in-process or inside a stateless pod.
func RunDraft(ctx context.Context, reg pipeline.Registry, job *storage.Job, maxHops int) (*storage.KnowledgeObject, error) {
	return runDraft(ctx, reg, nil, job, maxHops)
}

func runDraft(ctx context.Context, reg pipeline.Registry, blobs storage.BlobStore, job *storage.Job, maxHops int) (*storage.KnowledgeObject, error) {
	if maxHops <= 0 {
		maxHops = 5
	}
	pipe, err := reg.Get(job.Pipeline)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}

	content, err := jobContent(job)
	if err != nil {
		return nil, &pipeline.PermanentError{Err: err}
	}
	draft := &storage.KnowledgeObject{
		ID:         uuid.New().String(),
		Type:       seedType(job.Type, job.Pipeline),
		RawContent: content,
		Pipeline:   job.Pipeline,
		Source:     job.Source,
		CreatedAt:  time.Now(),
		Mentions:   parseUserMentions(job.UserMentions),
		Tags:       parseUserHints(job.UserHints),
		ProfileID:  job.UserProfile,
		InboxNote:  job.UserNote,
		SourceKey:  job.UserSourceKey,
	}
	if len(job.UserData) > 0 {
		draft.Metadata = map[string]any{storageutil.MetadataDataKey: job.UserData}
	}

	// Binary content is an original: kept as bytes, content-addressed,
	// before any step runs, so a step that replaces RawContent with
	// text cannot lose it. When blobs is nil (stateless pod), attach the
	// deterministic original metadata so representation nodes still wire
	// derives_from -> artifact/0; the host stores the blob.
	if blobref.NeedsBlob(content) {
		if err := keepOriginal(ctx, blobs, draft, content); err != nil {
			return nil, err
		}
	}

	draft, err = pipeline.RunSteps(ctx, pipe, draft)
	if err != nil {
		return nil, err
	}

	for hop := 1; hop < maxHops; hop++ {
		if draft.Metadata == nil {
			break
		}
		next, ok := draft.Metadata["next_pipeline"].(string)
		if !ok || next == "" {
			break
		}
		delete(draft.Metadata, "next_pipeline")

		nextPipe, err := reg.Get(next)
		if err != nil {
			slog.Debug("jobs: hop pipeline not found, stopping hops", "pipeline", next)
			break
		}
		draft, err = pipeline.RunSteps(ctx, nextPipe, draft)
		if err != nil {
			return nil, err
		}
	}

	storageutil.ApplyAmbient(draft, job.UserAmbient)
	draft.ContentHash = storageutil.DraftContentHash(pipeline.ContentBytes(draft), draft.Source, draft.Metadata)
	return draft, nil
}
