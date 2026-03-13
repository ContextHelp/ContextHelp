package autosuggest

import (
	"encoding/json"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const pendingMetaKey = "plugin.autosuggest.pending"

// pendingPayload is stored in obj.Plugins["autosuggest"] in select mode.
type pendingPayload struct {
	Tags     []string `json:"tags"`
	Mentions []string `json:"mentions"`
}

// ApplyGenerate directly appends suggested tags and mentions to obj, deduplicating.
// The caller is responsible for persisting the updated object.
func ApplyGenerate(obj *storage.KnowledgeObject, tags []string, mentions []string) error {
	// Merge tags.
	existing := make(map[string]bool, len(obj.Tags))
	for _, t := range obj.Tags {
		existing[t.Label] = true
	}
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
	}

	// Merge mentions.
	existingM := make(map[string]bool, len(obj.Mentions))
	for _, m := range obj.Mentions {
		existingM[m] = true
	}
	for _, mention := range mentions {
		if mention == "" || existingM[mention] {
			continue
		}
		obj.Mentions = append(obj.Mentions, mention)
		existingM[mention] = true
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
