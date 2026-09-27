package events_test

import (
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// The re-projection topics follow kit's source.category.object.action
// grammar: category upgrade (beside signature.mismatched), object
// reprojection as one word (kit reads an underscore as object_modifier),
// past-tense action.
func TestReprojectionTopicsAreKitConformant(t *testing.T) {
	cases := []struct {
		name  string
		topic bus.Topic
		want  bus.Topic
	}{
		{
			"TopicDpkmsUpgradeReprojectionScheduled", events.TopicDpkmsUpgradeReprojectionScheduled,
			bus.TopicOf("dpkms", "upgrade", "reprojection").Action("scheduled"),
		},
		{
			"TopicDpkmsUpgradeReprojectionStarted", events.TopicDpkmsUpgradeReprojectionStarted,
			bus.TopicOf("dpkms", "upgrade", "reprojection").Action("started"),
		},
		{
			"TopicDpkmsUpgradeReprojectionProgressed", events.TopicDpkmsUpgradeReprojectionProgressed,
			bus.TopicOf("dpkms", "upgrade", "reprojection").Action("progressed"),
		},
		{
			"TopicDpkmsUpgradeReprojectionCompleted", events.TopicDpkmsUpgradeReprojectionCompleted,
			bus.TopicOf("dpkms", "upgrade", "reprojection").Action("completed"),
		},
		{
			"TopicDpkmsUpgradeReprojectionFailed", events.TopicDpkmsUpgradeReprojectionFailed,
			bus.TopicOf("dpkms", "upgrade", "reprojection").Action("failed"),
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
				t.Errorf("%s = %q: object carries modifier %q", tc.name, tc.topic, mod)
			}
		})
	}
}
