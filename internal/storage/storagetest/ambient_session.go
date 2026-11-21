package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// AmbientSessionConformance pins the ambient-capture storage contract
// (ADR-067): objects.session_id round-trips through Create, Get, List and
// Update (an update that does not touch it keeps it), an object without a
// session reads back empty, Reinforce leaves the first capture's session
// and metadata alone (first wins), and a job's ambient context round-trips
// through Create, Get and AcquireNext.
func AmbientSessionConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()

	grouped := searchFixtureObject("amb-grouped", "note", "grouped body")
	grouped.ContentHash, grouped.ReinforcementCount = "sha256:amb-grouped", 1
	grouped.SessionID = "sess_first"
	grouped.Metadata = map[string]any{"ambient_source": "clipboard"}
	plain := searchFixtureObject("amb-plain", "note", "plain body")
	for _, o := range []*storage.KnowledgeObject{grouped, plain} {
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatalf("create %s: %v", o.ID, err)
		}
	}

	t.Run("object session id round trip", func(t *testing.T) {
		got, err := drv.Objects().Get(ctx, grouped.ID)
		if err != nil || got == nil {
			t.Fatalf("get: %v", err)
		}
		if got.SessionID != "sess_first" {
			t.Errorf("session id %q, want sess_first", got.SessionID)
		}
		got, err = drv.Objects().Get(ctx, plain.ID)
		if err != nil || got == nil {
			t.Fatalf("get plain: %v", err)
		}
		if got.SessionID != "" {
			t.Errorf("plain object session id %q, want empty", got.SessionID)
		}
		listed, _, err := drv.Objects().List(ctx, storage.ObjectFilter{})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, o := range listed {
			seen[o.ID] = o.SessionID
		}
		if seen[grouped.ID] != "sess_first" || seen[plain.ID] != "" {
			t.Errorf("listed session ids %v", seen)
		}
	})

	t.Run("update keeps the session id", func(t *testing.T) {
		got, err := drv.Objects().Get(ctx, grouped.ID)
		if err != nil || got == nil {
			t.Fatalf("get: %v", err)
		}
		got.InboxNote = "edited"
		if err := drv.Objects().Update(ctx, got); err != nil {
			t.Fatalf("update: %v", err)
		}
		again, err := drv.Objects().Get(ctx, grouped.ID)
		if err != nil || again == nil {
			t.Fatalf("get again: %v", err)
		}
		if again.SessionID != "sess_first" || again.InboxNote != "edited" {
			t.Errorf("after update: session %q note %q", again.SessionID, again.InboxNote)
		}
	})

	t.Run("reinforce is first wins", func(t *testing.T) {
		merge := &storage.KnowledgeObject{
			SessionID: "sess_second",
			Metadata:  map[string]any{"ambient_source": "browser"},
		}
		id, err := drv.Objects().Reinforce(ctx, "sha256:amb-grouped", merge)
		if err != nil {
			t.Fatalf("reinforce: %v", err)
		}
		if id != grouped.ID {
			t.Fatalf("reinforced %q, want %q", id, grouped.ID)
		}
		got, err := drv.Objects().Get(ctx, grouped.ID)
		if err != nil || got == nil {
			t.Fatalf("get: %v", err)
		}
		if got.SessionID != "sess_first" || got.Metadata["ambient_source"] != "clipboard" {
			t.Errorf("first capture overwritten: session %q metadata %v", got.SessionID, got.Metadata)
		}
	})

	t.Run("job ambient context round trip", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		amb := &storage.AmbientContext{
			Source: "clipboard", Fingerprint: "sha256:f", SessionID: "sess_job",
			Metadata: map[string]any{"app_name": "Terminal"},
		}
		withAmb := &storage.Job{ID: "amb-job-1", Type: "ingest:text", Status: storage.JobPending,
			Payload: "p", Pipeline: "text.short", MaxRetries: 3, CreatedAt: now, UpdatedAt: now, UserAmbient: amb}
		without := &storage.Job{ID: "amb-job-2", Type: "ingest:text", Status: storage.JobPending,
			Payload: "p2", Pipeline: "text.short", MaxRetries: 3, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
		for _, j := range []*storage.Job{withAmb, without} {
			if err := drv.Jobs().Create(ctx, j); err != nil {
				t.Fatalf("create job %s: %v", j.ID, err)
			}
		}
		got, err := drv.Jobs().Get(ctx, withAmb.ID)
		if err != nil || got == nil {
			t.Fatalf("get job: %v", err)
		}
		if got.UserAmbient == nil || got.UserAmbient.Source != "clipboard" || got.UserAmbient.Fingerprint != "sha256:f" ||
			got.UserAmbient.SessionID != "sess_job" || got.UserAmbient.Metadata["app_name"] != "Terminal" {
			t.Errorf("job ambient %+v", got.UserAmbient)
		}
		got, err = drv.Jobs().Get(ctx, without.ID)
		if err != nil || got == nil {
			t.Fatalf("get job: %v", err)
		}
		if got.UserAmbient != nil {
			t.Errorf("job without ambient read back %+v", got.UserAmbient)
		}
		claimed, err := drv.Jobs().AcquireNext(ctx)
		if err != nil || claimed == nil {
			t.Fatalf("acquire: %v", err)
		}
		if claimed.ID != withAmb.ID || claimed.UserAmbient == nil || claimed.UserAmbient.SessionID != "sess_job" {
			t.Errorf("acquired %s ambient %+v", claimed.ID, claimed.UserAmbient)
		}
	})
}
