package stack

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// ServiceIngester hands dpkms's server-side watches (/api/v1/watches)
// to the in-process service.
func ServiceIngester(svc *service.Service) watcher.Ingester { return serviceIngester{svc} }

// serviceIngester calls the service as the system: a watch ingests on
// the owner's behalf with no request behind it, so every call attaches
// the unrestricted viewer explicitly (ADR-080 §4).
type serviceIngester struct{ svc *service.Service }

func (s serviceIngester) Analyze(ctx context.Context, req watcher.Request) (string, error) {
	return s.svc.Analyze(sharing.WithViewer(ctx, sharing.Unrestricted), service.AnalyzeRequest{
		Content: req.Content, Type: req.Type, Source: req.Source,
		Pipeline: req.Pipeline, KnownHash: req.KnownHash,
	})
}

// JobResult returns the job's result object, "" while it has none.
func (s serviceIngester) JobResult(ctx context.Context, jobID string) (string, error) {
	job, err := s.svc.GetJob(sharing.WithViewer(ctx, sharing.Unrestricted), jobID)
	if err != nil {
		return "", err
	}
	return job.ResultID, nil
}

func (s serviceIngester) DeleteObject(ctx context.Context, id string) error {
	return s.svc.DeleteObject(sharing.WithViewer(ctx, sharing.Unrestricted), id)
}
