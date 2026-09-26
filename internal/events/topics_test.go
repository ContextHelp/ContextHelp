package events_test

import (
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// Kit's ParseTopic splits the Object segment on its first underscore
// into object + modifier. The embeddings topics name a compound subject
// (embedding model, embeddings migration), so the subject moves to the
// Category and the Object stays a single word with no modifier.
func TestEmbeddingsTopicsFollowKitObjectModifierGrammar(t *testing.T) {
	cases := []struct {
		name  string
		topic bus.Topic
		want  bus.Topic
	}{
		{
			"TopicDpkmsEmbeddingsMigrationStarted", events.TopicDpkmsEmbeddingsMigrationStarted,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("started"),
		},
		{
			"TopicDpkmsEmbeddingsMigrationProgressed", events.TopicDpkmsEmbeddingsMigrationProgressed,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("progressed"),
		},
		{
			"TopicDpkmsEmbeddingsMigrationCompleted", events.TopicDpkmsEmbeddingsMigrationCompleted,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("completed"),
		},
		{
			"TopicDpkmsEmbeddingsMigrationFailed", events.TopicDpkmsEmbeddingsMigrationFailed,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("failed"),
		},
		{
			"TopicCtxtEmbeddingsModelPromoted", events.TopicCtxtEmbeddingsModelPromoted,
			bus.TopicOf("ctxt", "embeddings", "model").Action("promoted"),
		},
		{
			"TopicCtxtEmbeddingsModelDeprecated", events.TopicCtxtEmbeddingsModelDeprecated,
			bus.TopicOf("ctxt", "embeddings", "model").Action("deprecated"),
		},
		{
			"TopicCtxtEmbeddingsModelPurged", events.TopicCtxtEmbeddingsModelPurged,
			bus.TopicOf("ctxt", "embeddings", "model").Action("purged"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := bus.ValidateTopic(tc.topic); err != nil {
				t.Fatalf("%s = %q: %v", tc.name, tc.topic, err)
			}
			if tc.topic != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.topic, tc.want)
			}
			parsed, _, err := bus.ParseTopic(string(tc.topic))
			if err != nil {
				t.Fatalf("ParseTopic(%q): %v", tc.topic, err)
			}
			if mod := parsed.ModifierSeg(); mod != "" {
				t.Errorf("%s = %q: object %q carries modifier %q; kit reads the first underscore as object_modifier",
					tc.name, tc.topic, parsed.ObjectSeg(), mod)
			}
		})
	}
}
