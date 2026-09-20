package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TopicIndexType is the KnowledgeObject.Type for topic index objects.
const TopicIndexType = "topic_index"

// TopicEntry is one object's summary within a topic category.
type TopicEntry struct {
	ObjectID string `json:"object_id"`
	Summary  string `json:"summary"`
}

// TopicCategory groups entries under a single topic label.
type TopicCategory struct {
	Topic   string       `json:"topic"`
	Entries []TopicEntry `json:"entries"`
}

// TopicIndex is the full materialized index stored as JSON in a
// KnowledgeObject's RawContent.
type TopicIndex struct {
	Categories  []TopicCategory `json:"categories"`
	ObjectIDs   []string        `json:"object_ids"`
	GeneratedAt time.Time       `json:"generated_at"`
}

// BuildTopicIndex scans all objects, groups by topic from
// enrichment.structured_metadata, and stores a single topic_index
// object. Returns the index object ID.
func (s *Service) BuildTopicIndex(ctx context.Context) (string, error) {
	idx, err := s.buildIndex(ctx)
	if err != nil {
		return "", err
	}
	return s.storeIndex(ctx, idx)
}

// GetTopicIndex retrieves the stored topic index. Returns nil if
// no index exists yet.
func (s *Service) GetTopicIndex(ctx context.Context) (*TopicIndex, error) {
	obj, err := s.getIndexObject(ctx)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, nil
	}
	var idx TopicIndex
	if err := json.Unmarshal([]byte(obj.RawContent), &idx); err != nil {
		return nil, fmt.Errorf("decode topic index: %w", err)
	}
	return &idx, nil
}

// GetTopicEntries returns entries for a specific topic. Case-insensitive
// prefix match on topic name.
func (s *Service) GetTopicEntries(
	ctx context.Context, topic string,
) ([]TopicEntry, error) {
	idx, err := s.GetTopicIndex(ctx)
	if err != nil {
		return nil, err
	}
	if idx == nil {
		return nil, nil
	}
	lower := strings.ToLower(topic)
	for _, cat := range idx.Categories {
		if strings.ToLower(cat.Topic) == lower ||
			strings.HasPrefix(strings.ToLower(cat.Topic), lower) {
			return cat.Entries, nil
		}
	}
	return nil, nil
}

// UpdateTopicIndexIncremental adds a single object to the existing
// index without full rebuild.
func (s *Service) UpdateTopicIndexIncremental(
	ctx context.Context, obj *storage.KnowledgeObject,
) error {
	topics, summary := extractTopicsAndSummary(obj)
	if len(topics) == 0 {
		return nil
	}

	idx, err := s.GetTopicIndex(ctx)
	if err != nil {
		return err
	}
	if idx == nil {
		// No index yet; do full build.
		_, err := s.BuildTopicIndex(ctx)
		return err
	}

	// Check if object already indexed.
	for _, id := range idx.ObjectIDs {
		if id == obj.ID {
			return nil
		}
	}

	entry := TopicEntry{ObjectID: obj.ID, Summary: summary}
	idx.ObjectIDs = append(idx.ObjectIDs, obj.ID)

	catMap := make(map[string]int, len(idx.Categories))
	for i, cat := range idx.Categories {
		catMap[cat.Topic] = i
	}

	for _, topic := range topics {
		if i, ok := catMap[topic]; ok {
			idx.Categories[i].Entries = append(
				idx.Categories[i].Entries, entry,
			)
		} else {
			idx.Categories = append(idx.Categories, TopicCategory{
				Topic:   topic,
				Entries: []TopicEntry{entry},
			})
			catMap[topic] = len(idx.Categories) - 1
		}
	}
	sort.Slice(idx.Categories, func(i, j int) bool {
		return idx.Categories[i].Topic < idx.Categories[j].Topic
	})
	idx.GeneratedAt = time.Now().Truncate(time.Second)

	_, err = s.storeIndex(ctx, idx)
	return err
}

// buildIndex scans all objects and groups by topic.
func (s *Service) buildIndex(ctx context.Context) (*TopicIndex, error) {
	objects, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{
		Limit:  10000,
		Status: "all",
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}

	catMap := make(map[string][]TopicEntry)
	var objectIDs []string

	for _, obj := range objects {
		if obj.Type == TopicIndexType {
			continue
		}
		topics, summary := extractTopicsAndSummary(obj)
		if len(topics) == 0 {
			continue
		}
		entry := TopicEntry{ObjectID: obj.ID, Summary: summary}
		objectIDs = append(objectIDs, obj.ID)
		for _, topic := range topics {
			catMap[topic] = append(catMap[topic], entry)
		}
	}

	categories := make([]TopicCategory, 0, len(catMap))
	for topic, entries := range catMap {
		categories = append(categories, TopicCategory{
			Topic:   topic,
			Entries: entries,
		})
	}
	sort.Slice(categories, func(i, j int) bool {
		return categories[i].Topic < categories[j].Topic
	})

	return &TopicIndex{
		Categories:  categories,
		ObjectIDs:   objectIDs,
		GeneratedAt: time.Now().Truncate(time.Second),
	}, nil
}

// storeIndex upserts the topic_index object.
func (s *Service) storeIndex(
	ctx context.Context, idx *TopicIndex,
) (string, error) {
	data, err := json.Marshal(idx)
	if err != nil {
		return "", fmt.Errorf("marshal topic index: %w", err)
	}

	existing, err := s.getIndexObject(ctx)
	if err != nil {
		return "", err
	}

	now := time.Now().Truncate(time.Second)

	if existing != nil {
		existing.RawContent = string(data)
		existing.UpdatedAt = now
		if err := s.Store.Objects().Update(ctx, existing); err != nil {
			return "", fmt.Errorf("update topic index: %w", err)
		}
		return existing.ID, nil
	}

	obj := &storage.KnowledgeObject{
		ID:         uuid.New().String(),
		Type:       TopicIndexType,
		RawContent: string(data),
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Store.Objects().Create(ctx, obj); err != nil {
		return "", fmt.Errorf("create topic index: %w", err)
	}
	return obj.ID, nil
}

// getIndexObject finds the existing topic_index object, if any.
func (s *Service) getIndexObject(
	ctx context.Context,
) (*storage.KnowledgeObject, error) {
	objects, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{
		Type:   TopicIndexType,
		Limit:  1,
		Status: "all",
	})
	if err != nil {
		return nil, fmt.Errorf("find topic index: %w", err)
	}
	if len(objects) == 0 {
		return nil, nil
	}
	return objects[0], nil
}

// extractTopicsAndSummary pulls topics and a one-line summary from
// a KnowledgeObject's structured metadata.
func extractTopicsAndSummary(
	obj *storage.KnowledgeObject,
) ([]string, string) {
	if obj.Metadata == nil {
		return nil, ""
	}
	raw, ok := obj.Metadata["enrichment.structured_metadata"]
	if !ok {
		return nil, ""
	}

	var topics []string
	switch m := raw.(type) {
	case map[string]any:
		if t, ok := m["topics"]; ok {
			switch tv := t.(type) {
			case []any:
				for _, v := range tv {
					if s, ok := v.(string); ok && s != "" {
						topics = append(topics, s)
					}
				}
			case []string:
				topics = tv
			}
		}
	}

	if len(topics) == 0 {
		return nil, ""
	}

	summary := oneLiner(obj)
	return topics, summary
}

// oneLiner returns a short summary for an object.
func oneLiner(obj *storage.KnowledgeObject) string {
	if len(obj.Summaries) > 0 {
		s := obj.Summaries[0]
		if len(s) > 120 {
			return s[:117] + "..."
		}
		return s
	}
	if obj.RawContent != "" {
		s := strings.ReplaceAll(obj.RawContent, "\n", " ")
		s = strings.TrimSpace(s)
		if len(s) > 120 {
			return s[:117] + "..."
		}
		return s
	}
	return obj.ID
}
