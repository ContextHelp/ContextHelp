package adapter

import (
	"testing"

	"hop.top/kit/go/runtime/bus"
)

// TestLifecycleTopicsAreValid asserts every state action produces a
// kit-conformant 4-segment topic. kit/bus.ValidateTopic enforces
// past-tense action segments so the runner's emitted actions match
// the -ed heuristic / pastTenseWhitelist.
func TestLifecycleTopicsAreValid(t *testing.T) {
	for _, action := range []string{"started", "readied", "drained", "stopped"} {
		topic := LifecycleTopic(action)
		if err := bus.ValidateTopic(bus.Topic(topic)); err != nil {
			t.Errorf("LifecycleTopic(%q) = %q: invalid: %v", action, topic, err)
		}
	}
}

// TestEntityTopicsAreValid asserts entity-event topics across multiple
// protocol slots all pass kit/bus validation.
func TestEntityTopicsAreValid(t *testing.T) {
	for _, proto := range []string{"email", "contacts", "calendar"} {
		for _, action := range []string{"pre_validated", "pre_persisted", "persisted", "failed"} {
			topic := EntityTopic(proto, action)
			if err := bus.ValidateTopic(bus.Topic(topic)); err != nil {
				t.Errorf("EntityTopic(%q,%q) = %q: invalid: %v", proto, action, topic, err)
			}
		}
	}
}

// TestLifecycleTopicShape locks the literal topic name so subscribers
// (`dpkms.adapter.lifecycle.*`) keep matching after refactors.
func TestLifecycleTopicShape(t *testing.T) {
	got := LifecycleTopic("started")
	want := "dpkms.adapter.lifecycle.started"
	if got != want {
		t.Errorf("LifecycleTopic(started) = %q, want %q", got, want)
	}
}

// TestEntityTopicShape locks the literal entity-topic shape — kit/policy
// CEL rules subscribe by exact name (e.g. dpkms.email.entity.pre_persisted).
func TestEntityTopicShape(t *testing.T) {
	got := EntityTopic("email", "pre_persisted")
	want := "dpkms.email.entity.pre_persisted"
	if got != want {
		t.Errorf("EntityTopic(email, pre_persisted) = %q, want %q", got, want)
	}
}
