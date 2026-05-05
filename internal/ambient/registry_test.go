package ambient

import (
	"errors"
	"testing"
)

func TestRegistry_RegistersAndGetsBackSource(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	src := newFakeSource("clipboard")
	if err := r.Register(src); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	got := r.Get("clipboard")
	if got == nil {
		t.Fatal("Get(clipboard) returned nil after Register")
	}
	if got.Name() != "clipboard" {
		t.Errorf("Get returned source with name %q, want clipboard", got.Name())
	}
}

func TestRegistry_RejectsDuplicateName(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.Register(newFakeSource("clipboard")); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(newFakeSource("clipboard"))
	if !errors.Is(err, ErrSourceAlreadyRegistered) {
		t.Fatalf("second Register: want ErrSourceAlreadyRegistered, got %v", err)
	}
}

func TestRegistry_RejectsNilSource(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatal("Register(nil): expected error, got nil")
	}
}

func TestRegistry_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.Register(newFakeSource("")); err == nil {
		t.Fatal("Register(emptyName): expected error, got nil")
	}
}

func TestRegistry_NamesReturnsSorted(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	for _, name := range []string{"meeting", "clipboard", "filewatch"} {
		_ = r.Register(newFakeSource(name))
	}
	got := r.Names()
	want := []string{"clipboard", "filewatch", "meeting"}
	if len(got) != len(want) {
		t.Fatalf("got %d names, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistry_GetNonExistentReturnsNil(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if got := r.Get("nope"); got != nil {
		t.Errorf("Get(nope) on empty registry returned %v, want nil", got)
	}
}

func TestRegistry_LenTracksCount(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if r.Len() != 0 {
		t.Errorf("empty registry Len = %d, want 0", r.Len())
	}
	_ = r.Register(newFakeSource("a"))
	_ = r.Register(newFakeSource("b"))
	if r.Len() != 2 {
		t.Errorf("after 2 registers, Len = %d, want 2", r.Len())
	}
}
