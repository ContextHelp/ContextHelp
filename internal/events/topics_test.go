package events_test

// T-0478: every topic constant in this package must conform to kit's
// 4-segment past-tense contract (source.category.object.action) so that
// bus.ValidateTopic does not reject our publishers when kit flips the
// validator from warn to strict.

import (
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

func TestOutboundTopicsAreKitConformant(t *testing.T) {
	cases := map[string]bus.Topic{
		"TopicObjectIngested":                     events.TopicObjectIngested,
		"TopicObjectUpdated":                      events.TopicObjectUpdated,
		"TopicObjectDeleted":                      events.TopicObjectDeleted,
		"TopicJobCompleted":                       events.TopicJobCompleted,
		"TopicJobFailed":                          events.TopicJobFailed,
		"TopicJobEnqueued":                        events.TopicJobEnqueued,
		"TopicDpkmsUpgradeSignatureMismatch":      events.TopicDpkmsUpgradeSignatureMismatch,
		"TopicDpkmsEmbeddingsMigrationStarted":    events.TopicDpkmsEmbeddingsMigrationStarted,
		"TopicDpkmsEmbeddingsMigrationProgressed": events.TopicDpkmsEmbeddingsMigrationProgressed,
		"TopicDpkmsEmbeddingsMigrationCompleted":  events.TopicDpkmsEmbeddingsMigrationCompleted,
		"TopicDpkmsEmbeddingsMigrationFailed":     events.TopicDpkmsEmbeddingsMigrationFailed,
		"TopicCtxtEmbeddingsModelPromoted":        events.TopicCtxtEmbeddingsModelPromoted,
		"TopicCtxtEmbeddingsModelDeprecated":      events.TopicCtxtEmbeddingsModelDeprecated,
		"TopicCtxtEmbeddingsModelPurged":          events.TopicCtxtEmbeddingsModelPurged,
	}
	for name, topic := range cases {
		t.Run(name, func(t *testing.T) {
			if err := bus.ValidateTopic(topic); err != nil {
				t.Errorf("%s = %q: %v", name, topic, err)
			}
		})
	}
}

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
		{"TopicDpkmsEmbeddingsMigrationStarted", events.TopicDpkmsEmbeddingsMigrationStarted,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("started")},
		{"TopicDpkmsEmbeddingsMigrationProgressed", events.TopicDpkmsEmbeddingsMigrationProgressed,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("progressed")},
		{"TopicDpkmsEmbeddingsMigrationCompleted", events.TopicDpkmsEmbeddingsMigrationCompleted,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("completed")},
		{"TopicDpkmsEmbeddingsMigrationFailed", events.TopicDpkmsEmbeddingsMigrationFailed,
			bus.TopicOf("dpkms", "embeddings", "migration").Action("failed")},
		{"TopicCtxtEmbeddingsModelPromoted", events.TopicCtxtEmbeddingsModelPromoted,
			bus.TopicOf("ctxt", "embeddings", "model").Action("promoted")},
		{"TopicCtxtEmbeddingsModelDeprecated", events.TopicCtxtEmbeddingsModelDeprecated,
			bus.TopicOf("ctxt", "embeddings", "model").Action("deprecated")},
		{"TopicCtxtEmbeddingsModelPurged", events.TopicCtxtEmbeddingsModelPurged,
			bus.TopicOf("ctxt", "embeddings", "model").Action("purged")},
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
