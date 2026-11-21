package approval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The state machine of ADR-083 §9, pair by pair: requested moves to each
// terminal state by its one trigger, and every other move, every move
// out of a terminal state and every self-loop included, is illegal.
func TestStateMachine_EveryPair(t *testing.T) {
	allowed := map[[2]approval.State]approval.Trigger{
		{approval.StateRequested, approval.StateApproved}:  approval.TriggerApprover,
		{approval.StateRequested, approval.StateRejected}:  approval.TriggerApprover,
		{approval.StateRequested, approval.StateWithdrawn}: approval.TriggerRequester,
		{approval.StateRequested, approval.StateStale}:     approval.TriggerMechanism,
		{approval.StateRequested, approval.StateExpired}:   approval.TriggerClock,
	}
	for _, from := range approval.States {
		for _, to := range approval.States {
			trigger, ok := approval.Transition(from, to)
			want, legal := allowed[[2]approval.State{from, to}]
			assert.Equal(t, legal, ok, "%s -> %s", from, to)
			assert.Equal(t, want, trigger, "%s -> %s", from, to)
		}
		assert.Equal(t, from != approval.StateRequested, from.Terminal(), "%s terminal", from)
	}
	for _, s := range []approval.State{"", "pending", "deleted"} {
		_, ok := approval.Transition(approval.StateRequested, s)
		assert.False(t, ok, "requested -> %q", s)
		_, ok = approval.Transition(s, approval.StateApproved)
		assert.False(t, ok, "%q -> approved", s)
	}
}

// The stored states are the machine's states but expired, which is
// computed: pending at or after expires_at, never stored.
func TestStateOf_ExpiredIsComputed(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	r := &storage.ApprovalRequest{Status: storage.ApprovalRequested, ExpiresAt: t0}
	assert.Equal(t, approval.StateRequested, approval.StateOf(r, t0.Add(-time.Millisecond)))
	assert.Equal(t, approval.StateExpired, approval.StateOf(r, t0))
	assert.Equal(t, approval.StateExpired, approval.StateOf(r, t0.Add(time.Hour)))
	for _, s := range storage.ApprovalStatuses[1:] {
		r.Status = s
		assert.Equal(t, approval.State(s), approval.StateOf(r, t0.Add(time.Hour)), "a %s request never expires", s)
	}
	stored := map[approval.State]bool{}
	for _, s := range storage.ApprovalStatuses {
		stored[approval.State(s)] = true
	}
	for _, s := range approval.States {
		assert.Equal(t, s != approval.StateExpired, stored[s], "%s stored", s)
	}
}

// Canonical JSON: key order and spacing do not change the bytes or the
// digest; numbers keep their text; anything that is not one JSON value
// is refused.
func TestCanonicalize(t *testing.T) {
	a, err := approval.Canonicalize([]byte(` {"b": [1, 2.50], "a": {"y": "<x>", "x": null}} `))
	assert.NoError(t, err)
	b, err := approval.Canonicalize([]byte(`{"a":{"x":null,"y":"<x>"},"b":[1,2.50]}`))
	assert.NoError(t, err)
	assert.Equal(t, `{"a":{"x":null,"y":"<x>"},"b":[1,2.50]}`, string(a))
	assert.Equal(t, a, b)
	assert.Equal(t, approval.Digest(a), approval.Digest(b))
	assert.Regexp(t, `^v1:sha256:[0-9a-f]{64}$`, approval.Digest(a))
	c, _ := approval.Canonicalize([]byte(`{"a":{"x":null,"y":"<x>"},"b":[1,2.5]}`))
	assert.NotEqual(t, approval.Digest(a), approval.Digest(c), "2.50 and 2.5 are different payload text")
	for _, bad := range []string{``, `{`, `{} {}`, `{"a":1} x`, `nope`} {
		_, err := approval.Canonicalize([]byte(bad))
		assert.Error(t, err, "%q", bad)
	}
}
