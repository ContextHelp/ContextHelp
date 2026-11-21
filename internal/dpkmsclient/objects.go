package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
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

// ObjectFilter selects objects for ListObjects (GET /api/v1/objects) and
// ObjectFacets (GET /api/v1/objects/facets). Empty fields do not filter;
// dpkms validates every value and answers a malformed one with 400
// (USAGE, exit 2). The API documents each parameter.
type ObjectFilter struct {
	Type    string
	Subtype string
	// Tagged selects by tag (internal/tagfilter): one of, or every
	// required one; optional tags rank higher. The zero value selects
	// all.
	Tagged   tagfilter.Filter
	Mention  string
	Pipeline string
	// Status is active (the API's default when empty), inbox, discarded,
	// raw or all.
	Status string
	// Since and Until bound created_at, half-open (since <= created <
	// until): an RFC 3339 timestamp or a YYYY-MM-DD date, a UTC day; a date
	// Until is the start of the next day.
	Since string
	Until string
	// MetaType, Topic, Person and SourceType match the object's
	// metadata type, topics, people and source_type.
	MetaType   string
	Topic      string
	Person     string
	SourceType string
	// MentionedSince and MentionedUntil bound metadata.dates_mentioned:
	// YYYY-MM-DD dates.
	MentionedSince string
	MentionedUntil string
	// Limit caps the page; 0 returns every match. ListObjects only.
	Limit  int
	Offset int
	// Sort is created_at (the default) or updated_at; Dir is desc (the
	// default) or asc. ListObjects only.
	Sort string
	Dir  string
	// ChangedAfter, when non-nil, lists by change order: objects whose
	// change sequence is past it, oldest change first, paged by the
	// NextChangedAfter of ListObjectChanges. It excludes Sort, Dir and
	// Offset (dpkms answers the combination with 400). ListObjectChanges
	// only; 0 starts from the first change.
	ChangedAfter *int64
}

// filterQuery encodes the selecting fields, the parameters both
// GET /objects and GET /objects/facets take.
func (f ObjectFilter) filterQuery() url.Values {
	q := url.Values{}
	for k, v := range map[string]string{
		"type": f.Type, "subtype": f.Subtype, "tagged": f.Tagged.String(), "mention": f.Mention,
		"pipeline": f.Pipeline, "status": f.Status, "since": f.Since, "until": f.Until,
		"meta_type": f.MetaType, "topic": f.Topic, "person": f.Person, "source_type": f.SourceType,
		"mentioned_since": f.MentionedSince, "mentioned_until": f.MentionedUntil,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}

// query encodes the filter plus the page, for GET /objects.
func (f ObjectFilter) query() url.Values {
	q := f.filterQuery()
	q.Set("limit", strconv.Itoa(f.Limit))
	for k, v := range map[string]string{"sort": f.Sort, "dir": f.Dir} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.Offset > 0 {
		q.Set("offset", strconv.Itoa(f.Offset))
	}
	if f.ChangedAfter != nil {
		q.Set("changed_after", strconv.FormatInt(*f.ChangedAfter, 10))
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

// ObjectChanges is one page of a change-order list.
type ObjectChanges struct {
	Objects []*storage.KnowledgeObject
	// Total counts every change past the request's cursor that the
	// filter matches, not just this page.
	Total int
	// NextChangedAfter is the ChangedAfter of the next page: the last
	// object's change sequence, or the request's own when the page is
	// empty. Keep it to resume where this page ended.
	NextChangedAfter int64
}

// ListObjectChanges returns one page of the objects f.ChangedAfter
// selects, in change order (GET /api/v1/objects?changed_after=N). It is
// ListObjects for a caller that follows changes: an object updated
// since the cursor lists again, whatever its timestamps say. A nil
// f.ChangedAfter lists from the first change.
func (c *Client) ListObjectChanges(ctx context.Context, f ObjectFilter) (ObjectChanges, error) {
	if f.ChangedAfter == nil {
		var zero int64
		f.ChangedAfter = &zero
	}
	var out struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
		Next  int64                      `json:"next_changed_after"`
	}
	if err := c.Get(ctx, "/api/v1/objects", f.query(), &out); err != nil {
		return ObjectChanges{}, err
	}
	return ObjectChanges{Objects: out.Data, Total: out.Total, NextChangedAfter: out.Next}, nil
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
