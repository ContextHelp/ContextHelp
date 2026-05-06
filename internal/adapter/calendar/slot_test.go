package calendar

import "testing"

// TestProtocolIsCalendar locks the slot identity. Every backend under
// this slot returns this string from Adapter.Protocol(); the substrate
// Registry routes registration on it.
func TestProtocolIsCalendar(t *testing.T) {
	if Protocol != "calendar" {
		t.Errorf("Protocol = %q, want %q", Protocol, "calendar")
	}
}
