package cmd

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// handleReprocess registers the reprocess job handler
// (POST /api/v1/objects/{id}/reprocess enqueues the job). The steps run
// with this instance's configured LLM provider and write through the
// service, so each update publishes the object-updated event.
func handleReprocess(pool *jobs.WorkerPool, st *stack.Stack) {
	r := &jobs.Reprocessor{Objects: systemReprocessObjects{svc: st.Service}, LLM: st.Providers.LLM()}
	pool.Handle(jobs.ReprocessJobType, r.Handle)
}

// systemReprocessObjects reads and writes objects for a reprocess job as
// the system: the job runs in a worker long after the request that
// enqueued it, with no viewer of its own, so every service call attaches
// the unrestricted viewer explicitly (ADR-080 §4).
type systemReprocessObjects struct{ svc *service.Service }

func (o systemReprocessObjects) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	return o.svc.GetObject(asSystem(ctx), id)
}

func (o systemReprocessObjects) UpdateObject(ctx context.Context, obj *storage.KnowledgeObject) error {
	return o.svc.UpdateObject(asSystem(ctx), obj)
}
