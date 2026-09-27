package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// objectPath is /api/v1/objects/{id}, with id escaped.
func objectPath(id string) string { return "/api/v1/objects/" + url.PathEscape(id) }

// GetObject returns one object (GET /api/v1/objects/{id}). A missing
// object is a NOT_FOUND error (exit 3).
func (c *Client) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	if err := c.Get(ctx, objectPath(id), nil, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// ObjectFilter selects objects for ListObjects (GET /api/v1/objects).
// Empty fields do not filter; the API documents each parameter.
type ObjectFilter struct {
	Type     string
	Subtype  string
	Tag      string
	Mention  string
	Pipeline string
	// Status is active (the API's default when empty), inbox, discarded,
	// raw or all.
	Status string
	// Limit caps the page; 0 returns every match.
	Limit  int
	Offset int
	// Sort is created_at (the default) or updated_at; Dir is desc (the
	// default) or asc.
	Sort string
	Dir  string
}

func (f ObjectFilter) query() url.Values {
	q := url.Values{"limit": {strconv.Itoa(f.Limit)}}
	for k, v := range map[string]string{
		"type": f.Type, "subtype": f.Subtype, "tag": f.Tag, "mention": f.Mention,
		"pipeline": f.Pipeline, "status": f.Status, "sort": f.Sort, "dir": f.Dir,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.Offset > 0 {
		q.Set("offset", strconv.Itoa(f.Offset))
	}
	return q
}

// ListObjects returns one page of the objects f matches and the total
// match count.
func (c *Client) ListObjects(ctx context.Context, f ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var out struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
	}
	if err := c.Get(ctx, "/api/v1/objects", f.query(), &out); err != nil {
		return nil, 0, err
	}
	return out.Data, out.Total, nil
}

// ObjectPatch is the body of PATCH /api/v1/objects/{id}. It mirrors the
// patch the dpkms handler decodes, field for field on the wire; a test
// holds the two in step. A nil field is left as it is; a non-nil empty
// list clears tags or mentions. Title and Summary both set the first
// summary, so dpkms refuses a patch with both.
type ObjectPatch struct {
	Type    *string `json:"type,omitempty"`
	Subtype *string `json:"subtype,omitempty"`
	// Title replaces the summaries with this one.
	Title *string `json:"title,omitempty"`
	// Summary replaces the first summary and keeps the others.
	Summary *string `json:"summary,omitempty"`
	// Tags replaces the tags with these labels.
	Tags *[]string `json:"tags,omitempty"`
	// Mentions replaces the mentions: "@ns.slug" or ctxt://entity/... URIs.
	Mentions *[]string `json:"mentions,omitempty"`
}

// UpdateObject applies p to the object id and returns the stored result
// (PATCH /api/v1/objects/{id}, scope write:objects).
func (c *Client) UpdateObject(ctx context.Context, id string, p ObjectPatch) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	if err := c.Do(ctx, Request{Method: http.MethodPatch, Path: objectPath(id), Body: p}, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// DeleteObject deletes the object id and its edges (DELETE
// /api/v1/objects/{id}, scope delete:objects, which only admin tokens
// hold). A missing object is a NOT_FOUND error (exit 3).
func (c *Client) DeleteObject(ctx context.Context, id string) error {
	return c.Do(ctx, Request{Method: http.MethodDelete, Path: objectPath(id)}, nil)
}

// ReprocessObject enqueues a dpkms job that re-runs one enrichment step
// on the object id with the instance's providers, and returns the job ID
// (POST /api/v1/objects/{id}/reprocess, scope write:objects). An unknown
// step is a USAGE error (exit 2).
func (c *Client) ReprocessObject(ctx context.Context, id, step string) (string, error) {
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := c.Post(ctx, objectPath(id)+"/reprocess", map[string]string{"step": step}, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}
