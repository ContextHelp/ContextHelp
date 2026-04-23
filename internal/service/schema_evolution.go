package service

import (
	"context"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SchemaEvolutionSuggestion is a single suggested addition.
type SchemaEvolutionSuggestion struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// SchemaEvolutionResult holds all suggestions for a profile schema.
type SchemaEvolutionResult struct {
	Profile  string                      `json:"profile"`
	NewTypes []SchemaEvolutionSuggestion `json:"new_types,omitempty"`
	NewTopics []SchemaEvolutionSuggestion `json:"new_topics,omitempty"`
}

// SuggestSchemaEvolution scans recent objects and suggests entity types and
// topics not yet in the profile schema. Basic frequency analysis, no LLM.
func (s *Service) SuggestSchemaEvolution(
	ctx context.Context,
	profileName string,
	schema config.ProfileSchema,
) (*SchemaEvolutionResult, error) {
	const scanLimit = 100
	const minCount = 2

	objects, _, err := s.Store.Objects().List(ctx, storage.ObjectFilter{
		Limit: scanLimit,
		Sort:  "created_at",
		Dir:   "desc",
	})
	if err != nil {
		return nil, err
	}

	existingTypes := toSet(schema.EntityTypes)
	existingTopics := toSet(schema.TopicVocabulary)

	typeCounts := map[string]int{}
	topicCounts := map[string]int{}

	for _, obj := range objects {
		if obj.Metadata == nil {
			continue
		}
		raw, ok := obj.Metadata["enrichment.structured_metadata"]
		if !ok {
			continue
		}
		meta := extractMeta(raw)
		if meta == nil {
			continue
		}

		if meta.Type != "" && !existingTypes[meta.Type] {
			typeCounts[meta.Type]++
		}
		for _, topic := range meta.Topics {
			if topic != "" && !existingTopics[topic] {
				topicCounts[topic]++
			}
		}
	}

	result := &SchemaEvolutionResult{Profile: profileName}
	result.NewTypes = filterAndSort(typeCounts, minCount)
	result.NewTopics = filterAndSort(topicCounts, minCount)
	return result, nil
}

func extractMeta(raw any) *psteps.StructuredMetadata {
	switch v := raw.(type) {
	case *psteps.StructuredMetadata:
		return v
	case map[string]any:
		m := &psteps.StructuredMetadata{}
		if t, ok := v["type"].(string); ok {
			m.Type = t
		}
		if topics, ok := v["topics"].([]any); ok {
			for _, t := range topics {
				if s, ok := t.(string); ok {
					m.Topics = append(m.Topics, s)
				}
			}
		}
		return m
	default:
		return nil
	}
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

func filterAndSort(counts map[string]int, minCount int) []SchemaEvolutionSuggestion {
	var out []SchemaEvolutionSuggestion
	for k, v := range counts {
		if v >= minCount {
			out = append(out, SchemaEvolutionSuggestion{Value: k, Count: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}
