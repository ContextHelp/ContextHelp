package dpkmsclient

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const inboxPath = "/api/v1/inbox"

// InboxListRequest pages GET /api/v1/inbox. Zero fields are not sent:
// dpkms then applies its own default limit and no time bound.
type InboxListRequest struct {
	Limit  int
	Offset int
	// Before and After bound the items' creation time.
	Before time.Time
	After  time.Time
}

// ListInbox returns one page of inbox items and the total count.
func (c *Client) ListInbox(ctx context.Context, req InboxListRequest) ([]*storage.KnowledgeObject, int, error) {
	q := url.Values{}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}
	if !req.Before.IsZero() {
		q.Set("before", req.Before.UTC().Format(time.RFC3339))
	}
	if !req.After.IsZero() {
		q.Set("after", req.After.UTC().Format(time.RFC3339))
	}
	var out struct {
		Items []*storage.KnowledgeObject `json:"items"`
		Total int                        `json:"total"`
	}
	if err := c.Get(ctx, inboxPath, q, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.Total, nil
}

// InboxQueueRequest selects GET /api/v1/inbox/queue. Pending, Failed and
// Raw pick categories; none set picks all three. A zero Limit leaves the
// default to dpkms.
type InboxQueueRequest struct {
	Pending bool
	Failed  bool
	Raw     bool
	Limit   int
	Offset  int
}

// InboxQueueItem is one entry of the inbox queue: a pending, running or
// failed job, or a raw object. It mirrors the item dpkms encodes, field
// for field on the wire; a test holds the two in step.
type InboxQueueItem struct {
	// ID is the job ID or the object ID, depending on Kind.
	ID string `json:"id"`
	// Kind is "job" or "object".
	Kind string `json:"kind"`
	// Status is the job status (pending, running, failed) or "raw".
	Status string `json:"status"`
	// Type is the content type, e.g. text or url.
	Type string `json:"type"`
	// Source is where the content came from.
	Source string `json:"source"`
	// Pipeline is the pipeline name, when set.
	Pipeline  string    `json:"pipeline"`
	CreatedAt time.Time `json:"created_at"`
	// Error is a failed job's error message.
	Error string `json:"error,omitempty"`
}

// ListInboxQueue returns one page of the inbox queue and the total count.
func (c *Client) ListInboxQueue(ctx context.Context, req InboxQueueRequest) ([]InboxQueueItem, int, error) {
	q := url.Values{}
	for name, on := range map[string]bool{"pending": req.Pending, "failed": req.Failed, "raw": req.Raw} {
		if on {
			q.Set(name, "true")
		}
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}
	var out struct {
		Items []InboxQueueItem `json:"items"`
		Total int              `json:"total"`
	}
	if err := c.Get(ctx, inboxPath+"/queue", q, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.Total, nil
}

// TriageInbox promotes inbox item id to active and enqueues it, on
// pipeline when it is non-empty (else dpkms detects one). It returns the
// job ID. Needs process:inbox.
func (c *Client) TriageInbox(ctx context.Context, id, pipeline string) (string, error) {
	body := struct {
		Pipeline string `json:"pipeline,omitempty"`
	}{pipeline}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := c.Post(ctx, inboxPath+"/"+url.PathEscape(id)+"/triage", body, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}

// DiscardInbox discards inbox item id. Needs process:inbox.
func (c *Client) DiscardInbox(ctx context.Context, id string) error {
	return c.Post(ctx, inboxPath+"/"+url.PathEscape(id)+"/discard", nil, nil)
}

// ClearInbox discards every inbox item and returns how many. Needs
// process:inbox.
func (c *Client) ClearInbox(ctx context.Context) (int, error) {
	var out struct {
		Cleared int `json:"cleared"`
	}
	if err := c.Post(ctx, inboxPath+"/clear", nil, &out); err != nil {
		return 0, err
	}
	return out.Cleared, nil
}
