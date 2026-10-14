package dpkmsclient

import (
	"context"
	"net/url"
)

// Stats is dpkms's at-a-glance summary (GET /api/v1/stats). It mirrors
// the server's stats on the wire; a test holds the two in step.
type Stats struct {
	// KnowledgeObjects counts every object, whatever its status.
	KnowledgeObjects int            `json:"knowledge_objects"`
	ObjectsByType    map[string]int `json:"objects_by_type"`
	Jobs             JobCounts      `json:"jobs"`
	// Entities counts the entities the caller may list.
	Entities    int `json:"entities"`
	Feeds       int `json:"feeds"`
	ActiveFeeds int `json:"feeds_active"`
	Reminders   int `json:"reminders_pending"`
	// Resurfacing counts unseen resurfacing candidates; nil when the
	// instance's storage backend has no resurfacing queue.
	Resurfacing *int `json:"resurfacing_candidates"`
}

// JobCounts counts jobs by status; Total is their sum.
type JobCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// Stats returns the instance's counts. profile scopes the resurfacing
// count to one focus profile; "" counts every profile.
func (c *Client) Stats(ctx context.Context, profile string) (*Stats, error) {
	q := url.Values{}
	if profile != "" {
		q.Set("profile", profile)
	}
	var st Stats
	if err := c.Get(ctx, "/api/v1/stats", q, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
