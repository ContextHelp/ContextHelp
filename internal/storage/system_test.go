package storage

import "testing"

// System is an identity: it names the unscoped driver, it never wraps
// or narrows it.
func TestSystemReturnsSameDriver(t *testing.T) {
	d := &mockDriver{}
	got := System(d)
	if got != StorageDriver(d) {
		t.Fatalf("System(d) = %T(%p), want the same driver %p", got, got, d)
	}
	if System(nil) != nil {
		t.Fatal("System(nil) must stay nil")
	}
}
