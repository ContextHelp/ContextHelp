package lifecycle

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/runtime/domain"
)

func TestMachine_AllowedTransitions(t *testing.T) {
	sm := NewMachine(nil)
	ctx := context.Background()
	cases := []struct {
		from, to domain.State
		ok       bool
	}{
		{Probationary, Expired, true},
		{Probationary, Promoted, true},
		{Expired, Promoted, true}, // resurrection within soft-delete window
		{Promoted, Expired, false},
		{Expired, Probationary, false},
		{Promoted, Probationary, false},
	}
	for _, c := range cases {
		err := sm.Transition(ctx, c.from, c.to, false)
		gotOK := err == nil
		if gotOK != c.ok {
			t.Errorf("Transition(%v, %v) ok=%v, want %v (err=%v)", c.from, c.to, gotOK, c.ok, err)
		}
		if !c.ok && err != nil && !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("expected ErrInvalidTransition for %v→%v, got %v", c.from, c.to, err)
		}
	}
}

func TestMachine_AllowedFrom(t *testing.T) {
	sm := NewMachine(nil)
	got := sm.AllowedFrom(Probationary)
	if len(got) != 2 {
		t.Fatalf("expected 2 targets from Probationary, got %v", got)
	}
}
