package dpkmsclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AnalyzeRequest is the body of POST /api/v1/analyze. It mirrors the
// request the dpkms handler decodes, field for field on the wire; a test
// holds the two in step. The client keeps its own copy so ctxt does not
// link the service layer and, through it, the storage drivers.
type AnalyzeRequest struct {
	Content     string   `json:"content"`
	Type        string   `json:"type"`
	Pipeline    string   `json:"pipeline,omitempty"`
	Source      string   `json:"source,omitempty"`
	KnownHash   string   `json:"known_hash,omitempty"`
	SourceTitle string   `json:"source_title,omitempty"`
	AuthState   string   `json:"auth_state,omitempty"`
	Raw         bool     `json:"raw,omitempty"`
	NoFanout    bool     `json:"no_fanout,omitempty"`
	SourceKey   string   `json:"source_key,omitempty"`
	Force       bool     `json:"force,omitempty"`
	Mentions    []string `json:"mentions,omitempty"`
	Hints       []string `json:"hints,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	// IdempotencyKey identifies one logical submission. dpkms answers a
	// replayed key with the job it already created. Analyze mints one
	// when it is empty.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Note           string `json:"note,omitempty"`
}

// Analyze enqueues content with POST /api/v1/analyze and returns the job
// ID. One call is one logical submission: when req carries no idempotency
// key, Analyze mints one, so a caller that retries the same request value
// is deduplicated by dpkms.
func (c *Client) Analyze(ctx context.Context, req AnalyzeRequest) (string, error) {
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.NewString()
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := c.Post(ctx, "/api/v1/analyze", req, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}

// SearchRequest is an RSQL query against GET /api/v1/search.
type SearchRequest struct {
	Query  string
	Limit  int
	Offset int
	// Profile scopes the search to a focus profile; "" searches globally.
	Profile string
}

// Search runs req and returns one page of objects and the total match
// count.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]*storage.KnowledgeObject, int, error) {
	q := url.Values{
		"q":      {req.Query},
		"limit":  {strconv.Itoa(req.Limit)},
		"offset": {strconv.Itoa(req.Offset)},
	}
	if req.Profile != "" {
		q.Set("profile", req.Profile)
	}
	var out struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
	}
	if err := c.Get(ctx, "/api/v1/search", q, &out); err != nil {
		return nil, 0, err
	}
	return out.Data, out.Total, nil
}
