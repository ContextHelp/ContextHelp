package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
)

// handleReprocess registers the reprocess job handler
// (POST /api/v1/objects/{id}/reprocess enqueues the job). The steps run
// with this instance's configured LLM provider and write through the
// service, so each update publishes the object-updated event.
func handleReprocess(pool *jobs.WorkerPool, st *stack.Stack) {
	r := &jobs.Reprocessor{Objects: st.Service, LLM: st.Providers.LLM()}
	pool.Handle(jobs.ReprocessJobType, r.Handle)
}
