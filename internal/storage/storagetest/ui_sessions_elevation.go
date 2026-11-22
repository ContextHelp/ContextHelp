package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The steward elevation of a browser session (ADR-083 §3): the grant a
// session is elevated into and the last request of the elevation, set
// and cleared together, one elevation at a time.

// uiElevationCases are the elevation cases UISessionConformance runs.
var uiElevationCases = []struct {
	name string
	fn   func(*testing.T, storage.UISessionStore, *uiRegistry)
}{
	{"a new session is not elevated", uiConfNewSessionNotElevated},
	{"elevate and touch", uiConfElevateAndTouch},
	{"end an elevation", uiConfEndElevation},
	{"one elevation at a time", uiConfOneElevationAtATime},
	{"a revoked session is not elevated", uiConfRevokedSessionNotElevated},
	{"elevation of an unknown session", uiConfElevationUnknownSession},
}

// assertElevation checks session id is elevated into grant (none when
// grant is "") since activeAt, through Get and Lookup alike.
func assertElevation(t *testing.T, st storage.UISessionStore, s *storage.UISession, grant string, activeAt time.Time) {
	t.Helper()
	ctx := context.Background()
	byID, err := st.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gots := []*storage.UISession{byID}
	if byID.RevokedAt == nil {
		bySecret, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(time.Minute))
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		gots = append(gots, bySecret)
	}
	for _, got := range gots {
		if got.StewardGrantID != grant {
			t.Errorf("steward_grant_id = %q, want %q", got.StewardGrantID, grant)
		}
		switch {
		case grant == "" && got.StewardActiveAt != nil:
			t.Errorf("steward_active_at = %s on a session that is not elevated", got.StewardActiveAt)
		case grant != "" && (got.StewardActiveAt == nil || !got.StewardActiveAt.Equal(activeAt)):
			t.Errorf("steward_active_at = %v, want %s", got.StewardActiveAt, activeAt)
		}
	}
}

func uiConfNewSessionNotElevated(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-elev-new", r.opsA, uiT0, time.Hour, 24*time.Hour)
	// Create never elevates, whatever the row it is handed says.
	at := uiT0
	s.StewardGrantID, s.StewardActiveAt = "stg_handed", &at
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertElevation(t, st, s, "", time.Time{})
}

// elevatedSession stores a session of r.ops named id, elevated into
// grant at at.
func elevatedSession(t *testing.T, st storage.UISessionStore, r *uiRegistry, id, grant string, at time.Time) *storage.UISession {
	t.Helper()
	ctx := context.Background()
	s := r.session(id, r.opsA, uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Elevate(ctx, s.ID, grant, at); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	assertElevation(t, st, s, grant, at)
	return s
}

func uiConfElevateAndTouch(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := elevatedSession(t, st, r, "ses-elev", "stg_a", uiT0.Add(10*time.Second))
	list, err := st.List(ctx, storage.UISessionFilter{PrincipalID: r.ops.ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, got := range list {
		if got.ID == s.ID && got.StewardGrantID != "stg_a" {
			t.Errorf("List: steward_grant_id = %q, want stg_a", got.StewardGrantID)
		}
	}

	t2 := uiT0.Add(20 * time.Second)
	if ok, err := st.TouchElevation(ctx, s.ID, "stg_a", t2); err != nil || !ok {
		t.Fatalf("TouchElevation(stg_a) = %v, %v; want true", ok, err)
	}
	assertElevation(t, st, s, "stg_a", t2)
	if ok, err := st.TouchElevation(ctx, s.ID, "stg_b", uiT0.Add(30*time.Second)); err != nil || ok {
		t.Errorf("TouchElevation of another grant = %v, %v; want false", ok, err)
	}
	assertElevation(t, st, s, "stg_a", t2)
	// Re-elevating into the same grant refreshes it.
	t3 := uiT0.Add(40 * time.Second)
	if err := st.Elevate(ctx, s.ID, "stg_a", t3); err != nil {
		t.Fatalf("re-Elevate: %v", err)
	}
	assertElevation(t, st, s, "stg_a", t3)
}

func uiConfEndElevation(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	t1 := uiT0.Add(10 * time.Second)
	s := elevatedSession(t, st, r, "ses-elev-end", "stg_a", t1)
	if ok, err := st.EndElevation(ctx, s.ID, "stg_b"); err != nil || ok {
		t.Errorf("EndElevation of another grant = %v, %v; want false", ok, err)
	}
	assertElevation(t, st, s, "stg_a", t1)
	if ok, err := st.EndElevation(ctx, s.ID, "stg_a"); err != nil || !ok {
		t.Fatalf("EndElevation = %v, %v; want true", ok, err)
	}
	assertElevation(t, st, s, "", time.Time{})
	if ok, err := st.EndElevation(ctx, s.ID, "stg_a"); err != nil || ok {
		t.Errorf("second EndElevation = %v, %v; want false", ok, err)
	}
	if ok, err := st.TouchElevation(ctx, s.ID, "stg_a", t1); err != nil || ok {
		t.Errorf("TouchElevation after the end = %v, %v; want false", ok, err)
	}
	assertElevation(t, st, s, "", time.Time{})
	// The session itself is untouched by its elevation.
	got, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	assertUISession(t, got, s)
}

func uiConfOneElevationAtATime(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-elev-one", r.opsA, uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t1 := uiT0.Add(time.Second)
	if err := st.Elevate(ctx, s.ID, "stg_first", t1); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	if err := st.Elevate(ctx, s.ID, "stg_second", uiT0.Add(2*time.Second)); !errors.Is(err, storage.ErrSessionElevated) {
		t.Errorf("Elevate into a second grant: err = %v, want ErrSessionElevated", err)
	}
	assertElevation(t, st, s, "stg_first", t1)
	// Another session of the same principal elevates on its own.
	other := r.session("ses-elev-other", r.opsB, uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, other); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Elevate(ctx, other.ID, "stg_second", t1); err != nil {
		t.Fatalf("Elevate another session: %v", err)
	}
	assertElevation(t, st, other, "stg_second", t1)
	assertElevation(t, st, s, "stg_first", t1)
}

func uiConfRevokedSessionNotElevated(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-elev-revoked", r.opsA, uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t1 := uiT0.Add(time.Second)
	if err := st.Elevate(ctx, s.ID, "stg_r", t1); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	if err := st.Revoke(ctx, s.ID, uiT0.Add(2*time.Second), "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := st.Elevate(ctx, s.ID, "stg_r", uiT0.Add(3*time.Second)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Elevate a revoked session: err = %v, want ErrNotFound", err)
	}
	if ok, err := st.TouchElevation(ctx, s.ID, "stg_r", uiT0.Add(3*time.Second)); err != nil || ok {
		t.Errorf("TouchElevation of a revoked session = %v, %v; want false", ok, err)
	}
	if ok, err := st.EndElevation(ctx, s.ID, "stg_r"); err != nil || !ok {
		t.Errorf("EndElevation of a revoked session = %v, %v; want true: its elevation still ends", ok, err)
	}
	assertElevation(t, st, s, "", time.Time{})
}

func uiConfElevationUnknownSession(t *testing.T, st storage.UISessionStore, _ *uiRegistry) {
	ctx := context.Background()
	if err := st.Elevate(ctx, "ses-nope", "stg_x", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Elevate: err = %v, want ErrNotFound", err)
	}
	if _, err := st.TouchElevation(ctx, "ses-nope", "stg_x", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("TouchElevation: err = %v, want ErrNotFound", err)
	}
	if _, err := st.EndElevation(ctx, "ses-nope", "stg_x"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("EndElevation: err = %v, want ErrNotFound", err)
	}
}
