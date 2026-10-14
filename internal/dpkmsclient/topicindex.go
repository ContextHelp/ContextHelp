package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// TopicIndex is the materialized topic index. It mirrors dpkms's index
// on the wire; a test holds the two in step.
type TopicIndex struct {
	Categories  []TopicCategory `json:"categories"`
	ObjectIDs   []string        `json:"object_ids"`
	GeneratedAt time.Time       `json:"generated_at"`
}

// TopicCategory groups the entries of one topic.
type TopicCategory struct {
	Topic   string       `json:"topic"`
	Entries []TopicEntry `json:"entries"`
}

// TopicEntry is one object's summary under a topic.
type TopicEntry struct {
	ObjectID string `json:"object_id"`
	Summary  string `json:"summary"`
}

// TopicIndex returns the stored topic index (GET /api/v1/topic-index).
// Before the first rebuild it is a NOT_FOUND error (exit 3).
func (c *Client) TopicIndex(ctx context.Context) (*TopicIndex, error) {
	var idx TopicIndex
	if err := c.Get(ctx, "/api/v1/topic-index", nil, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// TopicEntries returns the entries of the first topic whose name equals
// or starts with topic, ignoring case; an empty slice when none does.
// Before the first rebuild it is a NOT_FOUND error.
func (c *Client) TopicEntries(ctx context.Context, topic string) ([]TopicEntry, error) {
	var out struct {
		Data []TopicEntry `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/topic-index/"+url.PathEscape(topic), nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// RebuildTopicIndex rebuilds the topic index from every object on dpkms
// (POST /api/v1/topic-index, write:objects) and returns the index
// object's ID.
func (c *Client) RebuildTopicIndex(ctx context.Context) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(ctx, Request{Method: http.MethodPost, Path: "/api/v1/topic-index"}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}
