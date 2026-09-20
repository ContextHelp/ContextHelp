package breaker

import (
	"errors"
	"testing"

	kitbreaker "hop.top/kit/go/core/breaker"
)

// stubKitBreaker captures Record calls and returns canned values for
// State / Allow.
type stubKitBreaker struct {
	state         kitbreaker.State
	allowErr      error
	recordSuccess []bool
	recordN       []int64
}

func (s *stubKitBreaker) Allow() error            { return s.allowErr }
func (s *stubKitBreaker) State() kitbreaker.State { return s.state }
func (s *stubKitBreaker) Record(success bool, n int64) {
	s.recordSuccess = append(s.recordSuccess, success)
	s.recordN = append(s.recordN, n)
}

func TestIsOutage_StateMapping(t *testing.T) {
	cases := []struct {
		state kitbreaker.State
		want  bool
	}{
		{kitbreaker.Closed, false},
		{kitbreaker.Open, true},
		{kitbreaker.HalfOpen, true},
	}
	for _, tc := range cases {
		t.Run(tc.state.String(), func(t *testing.T) {
			a := New(&stubKitBreaker{state: tc.state})
			if got := a.IsOutage(); got != tc.want {
				t.Errorf("IsOutage() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestAllow_PassesThrough(t *testing.T) {
	want := errors.New("circuit open")
	a := New(&stubKitBreaker{allowErr: want})
	if err := a.Allow(); !errors.Is(err, want) {
		t.Errorf("Allow err = %v; want %v", err, want)
	}
}

func TestRecord_PassesArgs(t *testing.T) {
	stub := &stubKitBreaker{}
	a := New(stub)
	a.Record(true, 42)
	a.Record(false, 0)
	if len(stub.recordSuccess) != 2 {
		t.Fatalf("Record called %d times; want 2", len(stub.recordSuccess))
	}
	if !stub.recordSuccess[0] || stub.recordSuccess[1] {
		t.Errorf("recordSuccess = %v", stub.recordSuccess)
	}
	if stub.recordN[0] != 42 || stub.recordN[1] != 0 {
		t.Errorf("recordN = %v", stub.recordN)
	}
}

func TestNew_PanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil breaker")
		}
	}()
	_ = New(nil)
}
