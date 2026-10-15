package dpkmsclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const jobsPath = "/api/v1/jobs"

func jobPath(id string) string { return jobsPath + "/" + url.PathEscape(id) }

// JobListRequest pages GET /api/v1/jobs. Zero fields are not sent: dpkms
// then lists every status with its own default limit.
type JobListRequest struct {
	// Status keeps jobs in one state: pending, running, completed,
	// failed or cancelled.
	Status string
	Limit  int
	Offset int
}

// ListJobs returns one page of the instance's job queue, newest first,
// and the total count. Needs read:jobs.
func (c *Client) ListJobs(ctx context.Context, req JobListRequest) ([]*storage.Job, int, error) {
	q := url.Values{}
	if req.Status != "" {
		q.Set("status", req.Status)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}
	var out struct {
		Data  []*storage.Job `json:"data"`
		Total int            `json:"total"`
	}
	if err := c.Get(ctx, jobsPath, q, &out); err != nil {
		return nil, 0, err
	}
	return out.Data, out.Total, nil
}

// GetJob returns job id. A missing job is NOT_FOUND (exit 3). Needs
// read:jobs.
func (c *Client) GetJob(ctx context.Context, id string) (*storage.Job, error) {
	var job storage.Job
	if err := c.Get(ctx, jobPath(id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// RetryJob puts job id back in the queue and returns it. A job past its
// retry budget, or an unknown one, is USAGE (exit 2). Needs write:jobs.
func (c *Client) RetryJob(ctx context.Context, id string) (*storage.Job, error) {
	var job storage.Job
	if err := c.Post(ctx, jobPath(id)+"/retry", nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CancelJob cancels pending or running job id and returns it. A finished
// job is CONFLICT (exit 4), an unknown one NOT_FOUND (exit 3). Needs
// cancel:jobs (admin).
func (c *Client) CancelJob(ctx context.Context, id string) (*storage.Job, error) {
	var job storage.Job
	if err := c.Post(ctx, jobPath(id)+"/cancel", nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
