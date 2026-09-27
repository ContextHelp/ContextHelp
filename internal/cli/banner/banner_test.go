package banner

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// TestFormatStates verifies each state renders with the expected sigil and
// keywords. We assert on substrings so future polish (e.g. ANSI colour) does
// not destabilise the test.
func TestFormatStates(t *testing.T) {
	cases := []struct {
		name string
		st   upgrade.Status
		want []string
	}{
		{
			"in_progress",
			upgrade.Status{
				State:      upgrade.StateInProgress,
				Bucket:     upgrade.BucketReindexAuto,
				Done:       3,
				Total:      10,
				Progress:   0.3,
				EtaSeconds: 7,
			},
			[]string{"upgrading reindex_auto", "3/10 objects", "30%", "ETA 7s"},
		},
		{
			"failed",
			upgrade.Status{
				State:     upgrade.StateFailed,
				Bucket:    upgrade.BucketReingestSelective,
				LastError: "disk full",
			},
			[]string{"upgrade failed", "reingest_selective", "disk full"},
		},
		{
			"awaiting_consent",
			upgrade.Status{
				State:  upgrade.StateAwaitingConsent,
				Bucket: upgrade.BucketReingestAll,
			},
			[]string{"awaiting_consent", "reingest_all"},
		},
		{
			"idle_renders_empty",
			upgrade.Status{State: upgrade.StateIdle},
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Format(tc.st)
			if tc.want == nil {
				if out != "" {
					t.Fatalf("idle should render empty, got %q", out)
				}
				return
			}
			for _, frag := range tc.want {
				if !strings.Contains(out, frag) {
					t.Errorf("missing %q in %q", frag, out)
				}
			}
		})
	}
}
