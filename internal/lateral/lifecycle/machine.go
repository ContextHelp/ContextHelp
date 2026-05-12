package lifecycle

import "hop.top/kit/go/runtime/domain"

// Lifecycle states for lateral_candidate records. Typed as domain.State so
// they slot directly into the kit state machine.
const (
	Probationary domain.State = "probationary"
	Expired      domain.State = "expired"
	Promoted     domain.State = "promoted"
)

// Rules declares the allowed transitions. Promoted is terminal (empty
// slice). Resurrection is expired → promoted within the soft-delete window
// — the window check lives in promote.Handler (T19), not the rules map.
//
// Treat as read-only; NewMachine deep-copies before passing to kit.
var Rules = map[domain.State][]domain.State{
	Probationary: {Expired, Promoted},
	Expired:      {Promoted},
	Promoted:     {},
}

// NewMachine returns a kit StateMachine wired with the lateral rules.
// Pass a non-nil EventPublisher to enable the
// kit.runtime.state.pre_transitioned (sync, veto-able) and
// kit.runtime.state.post_transitioned (async) bus events.
//
// Callers may forward kit options such as
// domain.WithSMTopicPrefix("ctxt.lateral.state") to override the default
// kit.runtime.state.* bus topics with a lateral-scoped prefix.
//
// Rules is deep-copied before being handed to kit so the package-level map
// stays effectively immutable across StateMachine instances.
func NewMachine(pub domain.EventPublisher, opts ...domain.SMOption) *domain.StateMachine {
	rules := make(map[domain.State][]domain.State, len(Rules))
	for from, tos := range Rules {
		cp := make([]domain.State, len(tos))
		copy(cp, tos)
		rules[from] = cp
	}
	return domain.NewStateMachine(rules, pub, opts...)
}
