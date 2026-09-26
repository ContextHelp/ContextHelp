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
		"TopicObjectIngested":                           events.TopicObjectIngested,
		"TopicObjectUpdated":                            events.TopicObjectUpdated,
		"TopicObjectDeleted":                            events.TopicObjectDeleted,
		"TopicJobCompleted":                             events.TopicJobCompleted,
		"TopicJobFailed":                                events.TopicJobFailed,
		"TopicJobEnqueued":                              events.TopicJobEnqueued,
		"TopicDpkmsUpgradeSignatureMismatch":            events.TopicDpkmsUpgradeSignatureMismatch,
		"TopicDpkmsUpgradeEmbeddingsMigrationStarted":   events.TopicDpkmsUpgradeEmbeddingsMigrationStarted,
		"TopicDpkmsUpgradeEmbeddingsMigrationProgress":  events.TopicDpkmsUpgradeEmbeddingsMigrationProgress,
		"TopicDpkmsUpgradeEmbeddingsMigrationCompleted": events.TopicDpkmsUpgradeEmbeddingsMigrationCompleted,
		"TopicDpkmsUpgradeEmbeddingsMigrationFailed":    events.TopicDpkmsUpgradeEmbeddingsMigrationFailed,
		"TopicCtxtUpgradeEmbeddingModelPromoted":        events.TopicCtxtUpgradeEmbeddingModelPromoted,
		"TopicCtxtUpgradeEmbeddingModelDeprecated":      events.TopicCtxtUpgradeEmbeddingModelDeprecated,
		"TopicCtxtUpgradeEmbeddingModelPurged":          events.TopicCtxtUpgradeEmbeddingModelPurged,
	}
	for name, topic := range cases {
		t.Run(name, func(t *testing.T) {
			if err := bus.ValidateTopic(topic); err != nil {
				t.Errorf("%s = %q: %v", name, topic, err)
			}
		})
	}
}
