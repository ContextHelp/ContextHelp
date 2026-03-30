package autosuggest

import (
	"encoding/json"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

const pendingMetaKey = "plugin.autosuggest.pending"

// pendingPayload is stored in obj.Plugins["autosuggest"] in select mode.
type pendingPayload struct {
	Tags     []string `json:"tags"`
	Mentions []string `json:"mentions"`
}

// ApplyGenerate appends suggested tags and mentions to obj, deduplicating.
// Flat fields (Tags, Mentions) are updated for backward compat; Graph nodes are
// also emitted so projection.ProjectIndex picks them up on graph-canonical KOs.
// The caller is responsible for persisting the updated object.
func ApplyGenerate(obj *storage.KnowledgeObject, tags []string, mentionSlugs []string) error {
	// ── flat tags (backward compat) ───────────────────────────────────────────
	existing := make(map[string]bool, len(obj.Tags))
	for _, t := range obj.Tags {
		existing[t.Label] = true
	}
	var newTags []string
	for _, label := range tags {
		if label == "" || existing[label] {
			continue
		}
		obj.Tags = append(obj.Tags, storage.Tag{
			Label:  label,
			Weight: 0.5,
			Source: "plugin:autosuggest",
		})
		existing[label] = true
		newTags = append(newTags, label)
	}

	// ── flat mentions (backward compat) ───────────────────────────────────────
	existingM := make(map[string]bool, len(obj.Mentions))
	for _, u := range obj.Mentions {
		existingM[u.String()] = true
	}
	var newMentions []string
	for _, u := range mentions.ParseSlice(mentionSlugs) {
		if !existingM[u.String()] {
			obj.Mentions = append(obj.Mentions, u)
			existingM[u.String()] = true
			newMentions = append(newMentions, u.String())
		}
	}

	// ── graph nodes (graph-canonical contract) ────────────────────────────────
	// Append tag and entity_mention nodes so ProjectIndex works correctly when
	// Graph is present. Only new entries are appended; existing graph nodes are
	// left untouched to avoid duplication on repeated calls.
	if obj.ID != "" && (len(newTags) > 0 || len(newMentions) > 0) {
		if obj.Graph == nil {
			obj.Graph = &pluginapi.ObjectGraph{}
		}
		tagOrd, mentionOrd := 0, 0
		for _, n := range obj.Graph.Nodes {
			switch n.NodeType {
			case pluginapi.NodeTypeTag:
				tagOrd++
			case pluginapi.NodeTypeEntityMention:
				mentionOrd++
			}
		}
		for _, label := range newTags {
			obj.Graph.Nodes = append(obj.Graph.Nodes, pluginapi.GraphNode{
				ID:       pluginapi.NewNodeID(obj.ID, pluginapi.NodeTypeTag, tagOrd),
				NodeType: pluginapi.NodeTypeTag,
				Label:    label,
				Content:  label,
				Order:    tagOrd,
			})
			tagOrd++
		}
		for _, m := range newMentions {
			obj.Graph.Nodes = append(obj.Graph.Nodes, pluginapi.GraphNode{
				ID:       pluginapi.NewNodeID(obj.ID, pluginapi.NodeTypeEntityMention, mentionOrd),
				NodeType: pluginapi.NodeTypeEntityMention,
				Content:  m,
				Order:    mentionOrd,
			})
			mentionOrd++
		}
	}

	return nil
}

// ApplySelect stores suggestions as pending in obj.Plugins["autosuggest"] for later approval.
// The caller is responsible for persisting the updated object.
func ApplySelect(obj *storage.KnowledgeObject, tags []string, mentions []string) error {
	if obj.Plugins == nil {
		obj.Plugins = make(map[string]any)
	}
	payload := pendingPayload{Tags: tags, Mentions: mentions}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	obj.Plugins["autosuggest"] = map[string]any{
		"pending": json.RawMessage(b),
	}
	return nil
}

// GetPending reads any pending suggestions stored by ApplySelect.
// Returns empty slices if none are found.
func GetPending(obj *storage.KnowledgeObject) (tags []string, mentions []string) {
	if obj.Plugins == nil {
		return nil, nil
	}
	pluginData, ok := obj.Plugins["autosuggest"].(map[string]any)
	if !ok {
		return nil, nil
	}
	raw, ok := pluginData["pending"]
	if !ok {
		return nil, nil
	}

	var b []byte
	switch v := raw.(type) {
	case json.RawMessage:
		b = v
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return nil, nil
	}

	var payload pendingPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, nil
	}
	return payload.Tags, payload.Mentions
}

// ClearPending removes pending suggestions from the object.
func ClearPending(obj *storage.KnowledgeObject) {
	if obj.Plugins == nil {
		return
	}
	if pluginData, ok := obj.Plugins["autosuggest"].(map[string]any); ok {
		delete(pluginData, "pending")
	}
}
