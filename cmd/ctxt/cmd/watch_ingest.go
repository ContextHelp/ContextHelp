package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// watchIngester hands `ctxt watch` content to the resolved dpkms
// instance over its API: POST /analyze, GET /jobs/{id} for the object a
// job produced, DELETE /objects/{id}.
type watchIngester struct{ c *dpkmsclient.Client }

var _ watcher.Ingester = watchIngester{}

func (w watchIngester) Analyze(ctx context.Context, req watcher.Request) (string, error) {
	return w.c.Analyze(ctx, dpkmsclient.AnalyzeRequest{
		Content: req.Content, Type: req.Type, Source: req.Source,
		Pipeline: req.Pipeline, KnownHash: req.KnownHash,
	})
}

// JobResult returns the object the job produced, "" while it has none.
func (w watchIngester) JobResult(ctx context.Context, jobID string) (string, error) {
	var job struct {
		ResultID string `json:"result_id"`
	}
	if err := w.c.Get(ctx, "/api/v1/jobs/"+url.PathEscape(jobID), nil, &job); err != nil {
		return "", err
	}
	return job.ResultID, nil
}

// DeleteObject deletes the object; one already gone is not an error.
func (w watchIngester) DeleteObject(ctx context.Context, id string) error {
	err := w.c.DeleteObject(ctx, id)
	var ke *output.Error
	if errors.As(err, &ke) && ke.Code == output.CodeNotFound {
		return nil
	}
	return err
}

// loggedRecords prints each recorded failure to w as well, so a running
// `ctxt watch start` shows it as it happens.
type loggedRecords struct {
	watcher.FileRecords
	w io.Writer
}

func (l loggedRecords) SetLastError(ctx context.Context, watchID, msg string) error {
	fmt.Fprintf(l.w, "watch: %s: %s\n", watchID, msg)
	return l.FileRecords.SetLastError(ctx, watchID, msg)
}
