package steps

import (
	"context"
	"encoding/json"
	"fmt"

	ica "github.com/ideacrafterslabs/ctxt/plugins/ica"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ICANormalizer maps raw feed items ([]map[string]any from the
// fetcher step) into typed KnowledgeObject fields via
// ica.NormalizedItemToDraft.
type ICANormalizer struct {
	pipeline.BaseContract
	distChannelsAsMentions bool
}

// NewICANormalizer creates an ICANormalizer. When
// distChannelsAsMentions is true, distribution channel URIs are
// appended to draft.Mentions.
func NewICANormalizer(
	distChannelsAsMentions bool,
) *ICANormalizer {
	return &ICANormalizer{
		BaseContract: pipeline.NewBaseContract(
			pipeline.StepContract{
				Requires:     []string{"Metadata"},
				Produces:     []string{"Metadata", "Mentions", "Sections", "Tags"},
				Capabilities: []string{},
			},
		),
		distChannelsAsMentions: distChannelsAsMentions,
	}
}

func (s *ICANormalizer) Name() string {
	return "ica_normalizer"
}

func (s *ICANormalizer) Run(
	ctx context.Context,
	draft *storage.KnowledgeObject,
) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		return draft, nil
	}

	raw, ok := draft.Metadata["feed_items"]
	if !ok {
		return draft, nil
	}

	items, ok := raw.([]any)
	if !ok {
		return draft, nil
	}
	if len(items) == 0 {
		return draft, nil
	}

	var processed int
	for i, rawItem := range items {
		m, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		buf, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf(
				"ica_normalizer: marshal item %d: %w",
				i, err,
			)
		}

		var ni ica.NormalizedItem
		if err := json.Unmarshal(buf, &ni); err != nil {
			return nil, fmt.Errorf(
				"ica_normalizer: unmarshal item %d: %w",
				i, err,
			)
		}

		mapped := ica.NormalizedItemToDraft(&ni)
		if mapped == nil {
			continue
		}

		draft.Tags = append(draft.Tags, mapped.Tags...)
		draft.Sections = append(
			draft.Sections, mapped.Sections...,
		)

		if s.distChannelsAsMentions {
			draft.Mentions = append(
				draft.Mentions, mapped.Mentions...,
			)
		}

		for k, v := range mapped.Metadata {
			if draft.Metadata == nil {
				draft.Metadata = make(map[string]any)
			}
			draft.Metadata[k] = v
		}

		draft.ContentHash = mapped.ContentHash
		processed++
	}

	draft.Metadata["ica_items_processed"] = processed
	return draft, nil
}
