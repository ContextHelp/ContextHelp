package storageutil

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubmissionApplyReplacesNoteAndData(t *testing.T) {
	obj := &storage.KnowledgeObject{
		InboxNote: "captions transcript",
		Metadata:  map[string]any{"final_url": "https://x", "data": map[string]any{"title": "old"}},
	}
	sub := Submission{
		Note: "whisper transcript",
		Data: map[string]any{"title": "new", "channel": "c"},
	}

	fields := sub.Apply(obj)

	assert.Equal(t, "whisper transcript", obj.InboxNote)
	assert.Equal(t, map[string]any{"title": "new", "channel": "c"}, obj.Metadata["data"])
	assert.Equal(t, "https://x", obj.Metadata["final_url"], "other metadata keys stay")
	assert.ElementsMatch(t, []string{"inbox_note", "metadata"}, fields)
}

func TestSubmissionApplyEmptyKeepsStored(t *testing.T) {
	obj := &storage.KnowledgeObject{
		InboxNote: "keep me",
		Metadata:  map[string]any{"data": map[string]any{"a": 1}},
	}

	fields := Submission{}.Apply(obj)

	assert.Equal(t, "keep me", obj.InboxNote)
	assert.Equal(t, map[string]any{"a": 1}, obj.Metadata["data"])
	assert.Empty(t, fields)
}

func TestSubmissionApplyUnionsTagsAndMentions(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Tags:     []storage.Tag{{Label: "go", Source: "auto", Weight: 0.4}},
		Mentions: mentions.ParseSlice([]string{"@creator.fireship"}),
	}
	sub := Submission{
		Hints:    []storage.Tag{{Label: "go", Source: "user", Weight: 1}, {Label: "tools", Source: "user", Weight: 1}},
		Mentions: mentions.ParseSlice([]string{"@creator.fireship", "@guest.ada"}),
	}

	fields := sub.Apply(obj)

	require.Len(t, obj.Tags, 2, "tag labels are unique")
	assert.Equal(t, "auto", obj.Tags[0].Source, "stored tag wins on a shared label")
	assert.Equal(t, "tools", obj.Tags[1].Label)
	require.Len(t, obj.Mentions, 2)
	assert.ElementsMatch(t, []string{"tags", "mentions"}, fields)
}

func TestSubmissionApplyNoChangeReportsNothing(t *testing.T) {
	obj := &storage.KnowledgeObject{
		InboxNote: "n",
		Metadata:  map[string]any{"data": map[string]any{"a": "b"}},
		Tags:      []storage.Tag{{Label: "x"}},
	}
	sub := Submission{Note: "n", Data: map[string]any{"a": "b"}, Hints: []storage.Tag{{Label: "x", Source: "user"}}}

	assert.Empty(t, sub.Apply(obj))
}
