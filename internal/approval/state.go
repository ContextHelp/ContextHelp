package approval

import (
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// State is a request's state as the mechanism reports it: the stored
// status, or expired, which is computed from the clock and never
// written (ADR-083 §9).
type State string

// The states.
const (
	StateRequested State = State(storage.ApprovalRequested)
	StateApproved  State = State(storage.ApprovalApproved)
	StateRejected  State = State(storage.ApprovalRejected)
	StateWithdrawn State = State(storage.ApprovalWithdrawn)
	StateStale     State = State(storage.ApprovalStale)
	StateExpired   State = "expired"
)

// States lists every state, pending first.
var States = []State{StateRequested, StateApproved, StateRejected, StateWithdrawn, StateStale, StateExpired}

// Trigger is who or what moves a request between two states.
type Trigger string

// The triggers.
const (
	// TriggerApprover: an eligible approver's decision (the N-th
	// approval, or one rejection).
	TriggerApprover Trigger = "approver"
	// TriggerRequester: the requester, or the owner, withdrawing.
	TriggerRequester Trigger = "requester"
	// TriggerMechanism: the mechanism itself, when the preconditions no
	// longer hold at the last approval.
	TriggerMechanism Trigger = "mechanism"
	// TriggerClock: the pending TTL passing.
	TriggerClock Trigger = "clock"
)

// transitions is the state machine (ADR-083 §9): every allowed move and
// who triggers it. Every state but requested is terminal.
var transitions = map[State]map[State]Trigger{
	StateRequested: {
		StateApproved:  TriggerApprover,
		StateRejected:  TriggerApprover,
		StateWithdrawn: TriggerRequester,
		StateStale:     TriggerMechanism,
		StateExpired:   TriggerClock,
	},
}

// Transition reports whether a request may move from from to to, and
// who triggers the move.
func Transition(from, to State) (Trigger, bool) {
	t, ok := transitions[from][to]
	return t, ok
}

// Terminal reports whether no transition leaves s.
func (s State) Terminal() bool { return len(transitions[s]) == 0 }

// StateOf returns r's state at now: its stored status, or expired while
// it is pending at or past its expiry.
func StateOf(r *storage.ApprovalRequest, now time.Time) State {
	if r.ExpiredAt(now) {
		return StateExpired
	}
	return State(r.Status)
}

// requirePending refuses a decision or withdrawal on a request that is
// not pending at now.
func requirePending(r *storage.ApprovalRequest, now time.Time) error {
	switch s := StateOf(r, now); s {
	case StateRequested:
		return nil
	case StateExpired:
		return refuse(CodeExpired, "approval request %s expired at %s", r.ID, r.ExpiresAt.Format(time.RFC3339))
	default:
		return refuse(CodeClosed, "approval request %s is %s", r.ID, s)
	}
}
