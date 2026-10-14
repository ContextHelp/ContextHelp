package stack

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// ServiceIngester hands dpkms's server-side watches (/api/v1/watches)
// to the in-process service.
func ServiceIngester(svc *service.Service) watcher.Ingester { return serviceIngester{svc} }

type serviceIngester struct{ svc *service.Service }

func (s serviceIngester) Analyze(ctx context.Context, req watcher.Request) (string, error) {
	return s.svc.Analyze(ctx, service.AnalyzeRequest{
		Content: req.Content, Type: req.Type, Source: req.Source,
		Pipeline: req.Pipeline, KnownHash: req.KnownHash,
	})
}

// JobResult returns the job's result object, "" while it has none.
func (s serviceIngester) JobResult(ctx context.Context, jobID string) (string, error) {
	job, err := s.svc.GetJob(ctx, jobID)
	if err != nil {
		return "", err
	}
	return job.ResultID, nil
}

func (s serviceIngester) DeleteObject(ctx context.Context, id string) error {
	return s.svc.DeleteObject(ctx, id)
}
