// Package schemaevolution suggests entity types and topics for a focus
// profile's schema from the structured metadata of recent objects. It is
// basic frequency analysis over objects the caller already holds: ctxt
// runs it on objects read from dpkms.
package schemaevolution

import (
	"encoding/json"
	"sort"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ScanLimit is how many of the most recently created objects a caller
// should pass to Suggest.
const ScanLimit = 100

// MinCount is how often a type or topic has to occur to be suggested.
const MinCount = 2

// metadataKey is where the structured_metadata step stores its result.
const metadataKey = "enrichment.structured_metadata"

// Suggestion is a single suggested addition.
type Suggestion struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Result holds all suggestions for a profile schema.
type Result struct {
	Profile   string       `json:"profile"`
	NewTypes  []Suggestion `json:"new_types,omitempty"`
	NewTopics []Suggestion `json:"new_topics,omitempty"`
}

// Suggest counts the metadata types and topics of objects that schema
// does not list yet, and returns those seen at least MinCount times, most
// frequent first.
func Suggest(profileName string, schema config.ProfileSchema, objects []*storage.KnowledgeObject) *Result {
	existingTypes := toSet(schema.EntityTypes)
	existingTopics := toSet(schema.TopicVocabulary)

	typeCounts := map[string]int{}
	topicCounts := map[string]int{}
	for _, obj := range objects {
		meta, ok := structuredMetadata(obj)
		if !ok {
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

	return &Result{
		Profile:   profileName,
		NewTypes:  filterAndSort(typeCounts, MinCount),
		NewTopics: filterAndSort(topicCounts, MinCount),
	}
}

// meta is the part of the structured_metadata step's output Suggest
// reads.
type meta struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

// structuredMetadata reads the object's structured metadata. It arrives
// as a JSON object over the API and as the step's own struct in-process;
// both round-trip through JSON.
func structuredMetadata(obj *storage.KnowledgeObject) (meta, bool) {
	raw, ok := obj.Metadata[metadataKey]
	if !ok || raw == nil {
		return meta{}, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return meta{}, false
	}
	var m meta
	if err := json.Unmarshal(b, &m); err != nil {
		return meta{}, false
	}
	return m, true
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

func filterAndSort(counts map[string]int, minCount int) []Suggestion {
	var out []Suggestion
	for k, v := range counts {
		if v >= minCount {
			out = append(out, Suggestion{Value: k, Count: v})
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
