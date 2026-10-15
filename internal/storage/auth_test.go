package storage

import (
	"encoding/base64"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEncodeULID pins the encoding against the ULID specification: all
// zero and all one bits, and the timestamp prefix of the spec's
// example (01ARYZ6S41 for 1469918176385 ms).
func TestEncodeULID(t *testing.T) {
	if got := encodeULID([16]byte{}); got != strings.Repeat("0", 26) {
		t.Errorf("zero = %q", got)
	}
	var ones [16]byte
	for i := range ones {
		ones[i] = 0xff
	}
	if got := encodeULID(ones); got != "7"+strings.Repeat("Z", 25) {
		t.Errorf("ones = %q", got)
	}
	var b [16]byte
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], 1469918176385)
	copy(b[:6], ts[2:])
	if got := encodeULID(b); !strings.HasPrefix(got, "01ARYZ6S41") {
		t.Errorf("spec example time prefix = %q, want 01ARYZ6S41...", got[:10])
	}
}

// TestNewULID pins length, alphabet, uniqueness and time ordering.
func TestNewULID(t *testing.T) {
	seen := map[string]bool{}
	var prev string
	for i := 0; i < 200; i++ {
		id := NewULID()
		if len(id) != 26 || strings.Trim(id, crockford) != "" {
			t.Fatalf("ulid %q: want 26 Crockford base32 characters", id)
		}
		if seen[id] {
			t.Fatalf("ulid %q repeated", id)
		}
		seen[id] = true
		if prev != "" && id[:10] < prev[:10] {
			t.Errorf("time prefix went backwards: %q after %q", id, prev)
		}
		prev = id
	}
	if p, i := NewPrincipalID(), NewIdentityID(); !strings.HasPrefix(p, "prn_") || !strings.HasPrefix(i, "idn_") {
		t.Errorf("ids = %q, %q; want prn_/idn_ prefixes", p, i)
	}
}

func TestTicketSecretAndHash(t *testing.T) {
	s, err := NewTicketSecret()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != 32 {
		t.Errorf("secret %q decodes to %d bytes, %v; want 32", s, len(raw), err)
	}
	if h := TicketHash(s); len(h) != 64 || h == TicketHash(s+"x") || h != TicketHash(s) {
		t.Errorf("hash %q: want a stable 64-hex SHA-256", h)
	}
	if TicketHash("") != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Error("TicketHash is not plain SHA-256")
	}
}

func TestRolesJSON(t *testing.T) {
	s, err := RolesJSON(nil)
	if err != nil || s != "[]" {
		t.Errorf("RolesJSON(nil) = %q, %v", s, err)
	}
	s, _ = RolesJSON([]string{"admin", "reader"})
	roles, err := ParseRolesJSON(s)
	if err != nil || !slices.Equal(roles, []string{"admin", "reader"}) {
		t.Errorf("round trip = %v, %v", roles, err)
	}
	if _, err := ParseRolesJSON("not json"); err == nil {
		t.Error("ParseRolesJSON accepted garbage")
	}
	if roles, _ := ParseRolesJSON("null"); roles == nil {
		t.Error("ParseRolesJSON(null) returned nil, want an empty slice")
	}
}

func TestPrepareNewPrincipal(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("x", 3600))
	p := &Principal{Kind: PrincipalKindPerson, DisplayName: "A"}
	if err := PrepareNewPrincipal(p, now); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.ID, "prn_") || !slices.Equal(p.Roles, DefaultPrincipalRoles) || p.Status != PrincipalActive ||
		!p.CreatedAt.Equal(now) || p.CreatedAt.Location() != time.UTC {
		t.Errorf("defaults = %+v", p)
	}
	for name, bad := range map[string]*Principal{
		"nil":        nil,
		"owner kind": {Kind: PrincipalKindOwner, DisplayName: "o"},
		"owner id":   {ID: OwnerPrincipal, Kind: PrincipalKindPerson, DisplayName: "o"},
		"kind":       {Kind: "agent", DisplayName: "a"},
		"name":       {Kind: PrincipalKindPerson},
		"status":     {Kind: PrincipalKindPerson, DisplayName: "a", Status: "gone"},
		"role":       {Kind: PrincipalKindPerson, DisplayName: "a", Roles: []string{""}},
	} {
		if err := PrepareNewPrincipal(bad, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPrepareTicketIssue(t *testing.T) {
	ti := TicketIssue{Kind: TicketKindInvite, TTL: time.Hour, CreatedBy: OwnerPrincipal}
	if err := PrepareTicketIssue(&ti); err != nil {
		t.Fatal(err)
	}
	if string(ti.Payload) != "{}" || ti.Now.IsZero() || ti.Now.Location() != time.UTC {
		t.Errorf("defaults = %+v", ti)
	}
	for name, bad := range map[string]TicketIssue{
		"kind":   {Kind: "reset", TTL: time.Hour, CreatedBy: OwnerPrincipal},
		"ttl":    {Kind: TicketKindInvite, CreatedBy: OwnerPrincipal},
		"issuer": {Kind: TicketKindInvite, TTL: time.Hour},
	} {
		if err := PrepareTicketIssue(&bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
