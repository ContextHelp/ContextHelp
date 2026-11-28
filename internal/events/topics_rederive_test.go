package events_test

import (
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// The re-derive topics follow kit's source.category.object.action grammar,
// like the re-projection ones: category upgrade, one-word object, past-tense action.
func TestRederiveTopicsAreKitConformant(t *testing.T) {
	for topic, want := range map[bus.Topic]bus.Topic{
		events.TopicDpkmsUpgradeRederiveScheduled:   bus.TopicOf("dpkms", "upgrade", "rederive").Action("scheduled"),
		events.TopicDpkmsUpgradeRederiveCompleted:   bus.TopicOf("dpkms", "upgrade", "rederive").Action("completed"),
		events.TopicDpkmsUpgradeRederiveInterrupted: bus.TopicOf("dpkms", "upgrade", "rederive").Action("interrupted"),
	} {
		if topic != want {
			t.Errorf("topic %q, want %q", topic, want)
		}
	}
}
